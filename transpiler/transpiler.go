// Package transpiler provides a checker-free TypeScript-to-JavaScript
// transpilation entry point (the equivalent of the classic
// ts.transpileModule): it parses a single module, erases types and lowers
// TypeScript-only syntax, and emits JavaScript — without constructing a Program
// or a type checker. It is the public surface intended for embedders that only
// need to *run* TypeScript, not type-check it.
package transpiler

import (
	"errors"
	"fmt"
	"github.com/iceisfun/typescript/ast"
	"github.com/iceisfun/typescript/binder"
	"github.com/iceisfun/typescript/core"
	"github.com/iceisfun/typescript/parser"
	"github.com/iceisfun/typescript/printer"
	"github.com/iceisfun/typescript/scanner"
	"github.com/iceisfun/typescript/sourcemap"
	"github.com/iceisfun/typescript/transformers"
	"github.com/iceisfun/typescript/transformers/estransforms"
	"github.com/iceisfun/typescript/transformers/moduletransforms"
	"github.com/iceisfun/typescript/transformers/tstransforms"
	"github.com/iceisfun/typescript/tsoptions"
	"github.com/iceisfun/typescript/tspath"
)

// Options controls a single transpilation.
type Options struct {
	FileName string            // source name for diagnostics/sourcemaps; defaults to "module.ts"
	Target   core.ScriptTarget // ECMAScript output level; defaults to ESNext
	Module   core.ModuleKind   // module system for import/export; defaults to ESNext (preserve-ish)
	JSX      bool              // parse as .tsx

	// IgnoreSyntaxErrors makes transpilation permissive: parse diagnostics are
	// not reported as an error and the (error-recovered) input is transpiled
	// anyway, matching TypeScript's ts.transpileModule. The default is strict —
	// malformed input is rejected — which is safer when the output will be run.
	IgnoreSyntaxErrors bool
}

// Module transpiles a single TypeScript module's source to JavaScript. It runs
// the isolatedModules pipeline (type erasure, enum/namespace/parameter-property
// lowering, ES downleveling to Target, and module-syntax transform) and returns
// the emitted JavaScript text.
func Module(src string, o Options) (string, error) {
	js, _, err := transpile(src, o, false)
	return js, err
}

// ModuleWithSourceMap is like Module but also returns a v3 source map relating
// generated JavaScript positions back to the original TypeScript, so a host can
// report .ts line/column for runtime errors that occur in the emitted code.
func ModuleWithSourceMap(src string, o Options) (string, *sourcemap.RawSourceMap, error) {
	return transpile(src, o, true)
}

// ModuleAST transpiles a single TypeScript module and returns the lowered
// program as a JavaScript AST (github.com/iceisfun/typescript/ast) SourceFile
// instead of emitted text, so an embedder can hand it directly to its own
// compiler or VM without re-parsing generated JavaScript. It runs the same
// isolatedModules pipeline as [Module] (type erasure, enum/namespace/
// parameter-property lowering, ES downleveling to Target, and module-syntax
// transform).
//
// The printer resolves some information lazily at emit time that never lives on
// the nodes — auto-generated identifier names (temp variables, hoisted module/
// enum/namespace aliases) and string-literal text a transform sourced from
// another node. ModuleAST bakes those values into the tree, and parses and
// prepends any required emit-helper definitions (e.g. __importStar) as leading
// statements, so the returned SourceFile is a self-contained JavaScript program
// that can be read structurally without the printer or an EmitContext.
//
// The returned SourceFile contains only JavaScript syntax: every TypeScript-only
// node (type annotations, interfaces, type aliases, and the like) has been
// removed by the transforms.
func ModuleAST(src string, o Options) (*ast.SourceFile, error) {
	if o.FileName == "" {
		o.FileName = "/module.ts"
	}
	// ast.NewSourceFile requires a normalized, absolute file name.
	o.FileName = tspath.NormalizePath(o.FileName)
	if !tspath.IsRootedDiskPath(o.FileName) {
		o.FileName = tspath.CombinePaths("/", o.FileName)
	}
	if o.Target == 0 {
		o.Target = core.ScriptTargetESNext
	}
	if o.Module == 0 {
		o.Module = core.ModuleKindESNext
	}

	options := &core.CompilerOptions{
		Target:          o.Target,
		Module:          o.Module,
		IsolatedModules: core.TSTrue,
	}

	scriptKind := core.ScriptKindTS
	if o.JSX {
		scriptKind = core.ScriptKindTSX
	}
	parseOpts := ast.SourceFileParseOptions{
		FileName: o.FileName,
		Path:     tspath.Path(o.FileName),
	}
	sourceFile := parser.ParseSourceFile(parseOpts, src, scriptKind)
	if !o.IgnoreSyntaxErrors {
		if diags := sourceFile.Diagnostics(); len(diags) > 0 {
			return nil, syntaxError(sourceFile, o.FileName, diags)
		}
	}
	binder.BindSourceFile(sourceFile)

	// Unlike Module, use a non-pooled emit context: the returned AST — its
	// arena-allocated nodes — outlives this call, so it must not share the
	// pooled context that Module borrows and returns for reuse.
	emitContext := printer.NewEmitContext()
	host := &emitHost{options: options, files: []*ast.SourceFile{sourceFile}, resolver: newEmitResolver()}
	for _, tf := range scriptTransformers(emitContext, host, sourceFile) {
		sourceFile = tf.TransformSourceFile(sourceFile)
	}

	p := printer.NewPrinter(printer.PrinterOptions{
		NewLine: options.NewLine,
		Target:  options.Target,
	}, printer.PrintHandlers{}, emitContext)

	prologue := p.BakeForAST(sourceFile)
	if prologue != "" {
		var err error
		sourceFile, err = prependEmitHelpers(sourceFile, emitContext, prologue, o.FileName)
		if err != nil {
			return nil, err
		}
	}
	return sourceFile, nil
}

// prependEmitHelpers parses the emit-helper prologue (raw JavaScript such as the
// __importStar / __importDefault definitions the printer would otherwise splice
// in) and inserts its statements ahead of the module body, so the returned
// SourceFile carries its own helper definitions.
func prependEmitHelpers(sourceFile *ast.SourceFile, emitContext *printer.EmitContext, prologue, fileName string) (*ast.SourceFile, error) {
	const helperFile = "/__emit_helpers.js"
	hf := parser.ParseSourceFile(ast.SourceFileParseOptions{
		FileName: helperFile,
		Path:     tspath.Path(helperFile),
	}, prologue, core.ScriptKindJS)
	if diags := hf.Diagnostics(); len(diags) > 0 {
		return nil, fmt.Errorf("%s: parsing emit helpers: %s", fileName, diags[0].String())
	}
	combined := make([]*ast.Node, 0, len(hf.Statements.Nodes)+len(sourceFile.Statements.Nodes))
	combined = append(combined, hf.Statements.Nodes...)
	combined = append(combined, sourceFile.Statements.Nodes...)
	sourceFile.Statements = emitContext.Factory.AsNodeFactory().NewNodeList(combined)
	return sourceFile, nil
}

func transpile(src string, o Options, withMap bool) (string, *sourcemap.RawSourceMap, error) {
	if o.FileName == "" {
		o.FileName = "/module.ts"
	}
	// ast.NewSourceFile requires a normalized, absolute file name.
	o.FileName = tspath.NormalizePath(o.FileName)
	if !tspath.IsRootedDiskPath(o.FileName) {
		o.FileName = tspath.CombinePaths("/", o.FileName)
	}
	if o.Target == 0 {
		o.Target = core.ScriptTargetESNext
	}
	if o.Module == 0 {
		o.Module = core.ModuleKindESNext
	}

	options := &core.CompilerOptions{
		Target:          o.Target,
		Module:          o.Module,
		IsolatedModules: core.TSTrue,
	}

	scriptKind := core.ScriptKindTS
	if o.JSX {
		scriptKind = core.ScriptKindTSX
	}
	parseOpts := ast.SourceFileParseOptions{
		FileName: o.FileName,
		Path:     tspath.Path(o.FileName),
	}
	sourceFile := parser.ParseSourceFile(parseOpts, src, scriptKind)
	// The parser is error-tolerant: it recovers from syntax errors and still
	// produces an AST. Surface those diagnostics as an error instead of silently
	// transpiling malformed input.
	if !o.IgnoreSyntaxErrors {
		if diags := sourceFile.Diagnostics(); len(diags) > 0 {
			return "", nil, syntaxError(sourceFile, o.FileName, diags)
		}
	}
	// Bind the file so the reference resolver has a symbol table: the module and
	// runtime-syntax transforms rely on binder symbols to rewrite import
	// references (import { add } -> ns_1.add) and enum/namespace members.
	binder.BindSourceFile(sourceFile)

	host := &emitHost{options: options, files: []*ast.SourceFile{sourceFile}, resolver: newEmitResolver()}
	emitContext, put := printer.GetEmitContext()
	defer put()

	for _, tf := range scriptTransformers(emitContext, host, sourceFile) {
		sourceFile = tf.TransformSourceFile(sourceFile)
	}

	p := printer.NewPrinter(printer.PrinterOptions{
		NewLine:   options.NewLine,
		Target:    options.Target,
		SourceMap: withMap,
	}, printer.PrintHandlers{}, emitContext)

	if !withMap {
		return p.EmitSourceFile(sourceFile), nil, nil
	}

	// Drive the lower-level Write with a source-map generator: it writes the JS
	// to the text writer and records generated->original position mappings.
	gen := sourcemap.NewGenerator(
		tspath.GetBaseFileName(o.FileName)+".js", // generated file name
		"",  // sourceRoot
		"/", // sources directory
		tspath.ComparePathsOptions{UseCaseSensitiveFileNames: true, CurrentDirectory: "/"},
	)
	writer := printer.NewTextWriter("\n", 0)
	p.Write(sourceFile.AsNode(), sourceFile, writer, gen)
	return writer.String(), gen.RawSourceMap(), nil
}

// syntaxError formats the first parse diagnostic as an error with a
// file:line:column prefix. Positions are 1-based.
func syntaxError(sf *ast.SourceFile, fileName string, diags []*ast.Diagnostic) error {
	d := diags[0]
	line, col := scanner.GetECMALineAndUTF16CharacterOfPosition(sf, d.Pos())
	msg := fmt.Sprintf("%s:%d:%d: %s", fileName, line+1, int(col)+1, d.String())
	if n := len(diags); n > 1 {
		msg += fmt.Sprintf(" (and %d more)", n-1)
	}
	return errors.New(msg)
}

// scriptTransformers is the checker-free subset of the compiler's
// getScriptTransformers: in isolatedModules mode the reference resolver is the
// syntactic binder resolver (no type information), so no EmitResolver/checker is
// required for type erasure and syntax lowering.
func scriptTransformers(emitContext *printer.EmitContext, host printer.EmitHost, sourceFile *ast.SourceFile) []*transformers.Transformer {
	options := host.Options()
	opts := transformers.TransformOptions{
		Context:                   emitContext,
		CompilerOptions:           options,
		Resolver:                  binder.NewReferenceResolver(options, binder.ReferenceResolverHooks{}),
		EmitResolver:              host.GetEmitResolver(),
		GetEmitModuleFormatOfFile: host.GetEmitModuleFormatOfFile,
	}

	var tx []*transformers.Transformer
	tx = append(tx, tstransforms.NewTypeEraserTransformer(&opts))                // erase types
	tx = append(tx, tstransforms.NewRuntimeSyntaxTransformer(&opts))             // enum/namespace/param-props
	if downleveler := estransforms.GetESTransformer(&opts); downleveler != nil { // downlevel to Target
		tx = append(tx, downleveler)
	}
	tx = append(tx, estransforms.NewUseStrictTransformer(&opts))
	tx = append(tx, moduleTransformer(&opts)) // module syntax
	return tx
}

func moduleTransformer(opts *transformers.TransformOptions) *transformers.Transformer {
	switch opts.CompilerOptions.GetEmitModuleKind() {
	case core.ModuleKindPreserve:
		return moduletransforms.NewESModuleTransformer(opts)
	case core.ModuleKindCommonJS:
		return moduletransforms.NewCommonJSModuleTransformer(opts)
	default:
		return moduletransforms.NewImpliedModuleTransformer(opts)
	}
}

// emitHost is a minimal printer.EmitHost sufficient for single-file,
// checker-free transpilation. Filesystem- and program-level methods return
// inert values; GetEmitResolver returns nil because the isolatedModules pipeline
// never dereferences it.
type emitHost struct {
	options  *core.CompilerOptions
	files    []*ast.SourceFile
	resolver *emitResolver
}

func (h *emitHost) Options() *core.CompilerOptions  { return h.options }
func (h *emitHost) SourceFiles() []*ast.SourceFile  { return h.files }
func (h *emitHost) UseCaseSensitiveFileNames() bool { return true }
func (h *emitHost) GetCurrentDirectory() string     { return "/" }
func (h *emitHost) CommonSourceDirectory() string   { return "/" }
func (h *emitHost) IsEmitBlocked(string) bool       { return false }
func (h *emitHost) WriteFile(string, string) error  { return nil }
func (h *emitHost) GetEmitModuleFormatOfFile(ast.HasFileName) core.ModuleKind {
	return h.options.GetEmitModuleKind()
}
func (h *emitHost) GetEmitResolver() printer.EmitResolver                { return h.resolver }
func (h *emitHost) IsSourceFileFromExternalLibrary(*ast.SourceFile) bool { return false }
func (h *emitHost) GetProjectReferenceFromSource(tspath.Path) *tsoptions.SourceOutputAndProjectReference {
	return nil
}
