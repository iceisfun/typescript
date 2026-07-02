// Package transpiler provides a checker-free TypeScript-to-JavaScript
// transpilation entry point (the equivalent of the classic
// ts.transpileModule): it parses a single module, erases types and lowers
// TypeScript-only syntax, and emits JavaScript — without constructing a Program
// or a type checker. It is the public surface intended for embedders that only
// need to *run* TypeScript, not type-check it.
package transpiler

import (
	"github.com/iceisfun/typescript/ast"
	"github.com/iceisfun/typescript/binder"
	"github.com/iceisfun/typescript/core"
	"github.com/iceisfun/typescript/parser"
	"github.com/iceisfun/typescript/printer"
	"github.com/iceisfun/typescript/transformers"
	"github.com/iceisfun/typescript/transformers/estransforms"
	"github.com/iceisfun/typescript/transformers/moduletransforms"
	"github.com/iceisfun/typescript/transformers/tstransforms"
	"github.com/iceisfun/typescript/tsoptions"
	"github.com/iceisfun/typescript/tspath"
)

// Options controls a single transpilation.
type Options struct {
	FileName string           // source name for diagnostics/sourcemaps; defaults to "module.ts"
	Target   core.ScriptTarget // ECMAScript output level; defaults to ESNext
	Module   core.ModuleKind   // module system for import/export; defaults to ESNext (preserve-ish)
	JSX      bool              // parse as .tsx
}

// Module transpiles a single TypeScript module's source to JavaScript. It runs
// the isolatedModules pipeline (type erasure, enum/namespace/parameter-property
// lowering, ES downleveling to Target, and module-syntax transform) and returns
// the emitted JavaScript text.
func Module(src string, o Options) (string, error) {
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
	// Bind the file so the reference resolver has a symbol table: the module and
	// runtime-syntax transforms rely on binder symbols to rewrite import
	// references (import { add } -> ns_1.add) and enum/namespace members.
	binder.BindSourceFile(sourceFile)

	host := &emitHost{options: options, files: []*ast.SourceFile{sourceFile}}
	emitContext, put := printer.GetEmitContext()
	defer put()

	for _, tf := range scriptTransformers(emitContext, host, sourceFile) {
		sourceFile = tf.TransformSourceFile(sourceFile)
	}

	p := printer.NewPrinter(printer.PrinterOptions{
		NewLine: options.NewLine,
		Target:  options.Target,
	}, printer.PrintHandlers{}, emitContext)
	return p.EmitSourceFile(sourceFile), nil
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
	tx = append(tx, tstransforms.NewTypeEraserTransformer(&opts))                 // erase types
	tx = append(tx, tstransforms.NewRuntimeSyntaxTransformer(&opts))              // enum/namespace/param-props
	if downleveler := estransforms.GetESTransformer(&opts); downleveler != nil {  // downlevel to Target
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
	options *core.CompilerOptions
	files   []*ast.SourceFile
}

func (h *emitHost) Options() *core.CompilerOptions                            { return h.options }
func (h *emitHost) SourceFiles() []*ast.SourceFile                           { return h.files }
func (h *emitHost) UseCaseSensitiveFileNames() bool                          { return true }
func (h *emitHost) GetCurrentDirectory() string                             { return "/" }
func (h *emitHost) CommonSourceDirectory() string                          { return "/" }
func (h *emitHost) IsEmitBlocked(string) bool                              { return false }
func (h *emitHost) WriteFile(string, string) error                         { return nil }
func (h *emitHost) GetEmitModuleFormatOfFile(ast.HasFileName) core.ModuleKind { return h.options.GetEmitModuleKind() }
func (h *emitHost) GetEmitResolver() printer.EmitResolver                    { return nil }
func (h *emitHost) IsSourceFileFromExternalLibrary(*ast.SourceFile) bool     { return false }
func (h *emitHost) GetProjectReferenceFromSource(tspath.Path) *tsoptions.SourceOutputAndProjectReference {
	return nil
}
