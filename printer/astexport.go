package printer

import "github.com/iceisfun/typescript/ast"

// BakeForAST prepares a fully-transformed source file to be consumed as an AST
// (github.com/iceisfun/typescript/ast) — rather than as emitted text — by a
// downstream compiler or VM.
//
// The printer normally resolves two kinds of information lazily, at emit time,
// that never live on the nodes themselves:
//
//   - auto-generated identifier names (temp variables such as _a, and hoisted
//     module / enum / namespace aliases such as exports_1), whose final text is
//     produced by the scope-aware NameGenerator, and
//   - string-literal text that a transform sourced from another node (see
//     EmitContext.textSource), e.g. a specifier synthesized from an identifier.
//
// A consumer that only reads node.Text would therefore see empty or placeholder
// text for those nodes. BakeForAST drives a full, scope-correct emit pass (into
// a throwaway buffer) with name/text write-back enabled, so every such resolved
// value is written back into its node's Text field. After it returns, the tree
// is self-describing and needs neither the printer nor the EmitContext to be
// read structurally.
//
// It returns the emit-helper prologue (e.g. the __importStar / __importDefault
// definitions) the printer would otherwise splice ahead of the module body, as
// raw JavaScript text, or "" when the module requires no helpers. The caller is
// responsible for making those helpers available to the program (typically by
// parsing them and prepending them as leading statements).
//
// BakeForAST mutates identifier and string-literal nodes in place and should be
// called at most once per source file. The printer must have been constructed
// with the same EmitContext the transforms ran against.
func (p *Printer) BakeForAST(sourceFile *ast.SourceFile) string {
	// Capture the helper prologue first, while the name generator is in its
	// pristine state, so its file-level unique names are unaffected by the body
	// emit below.
	prologue := p.HelperPrologue(sourceFile)

	saved := p.bakeNames
	p.bakeNames = true
	// EmitSourceFile visits every node in correct scope order, resolving each
	// generated name in its own name-generation scope; with bakeNames set, each
	// resolved value is written back into its node. The emitted text is
	// discarded — only the mutated AST and the prologue above are kept.
	_ = p.EmitSourceFile(sourceFile)
	p.bakeNames = saved

	return prologue
}

// HelperPrologue returns the emit-helper definitions (e.g. __importStar,
// __importDefault) the printer would splice ahead of sourceFile's body, as raw
// JavaScript text, or "" when the module needs none. It must be called with the
// same EmitContext the module was transformed against.
func (p *Printer) HelperPrologue(sourceFile *ast.SourceFile) string {
	savedWriter := p.writer
	savedSourceFile := p.currentSourceFile
	defer func() {
		p.writer = savedWriter
		p.currentSourceFile = savedSourceFile
	}()

	p.currentSourceFile = sourceFile
	w := NewTextWriter("\n", 0)
	p.writer = w
	p.emitHelpers(sourceFile.AsNode())
	return w.String()
}

// bakeText writes resolved text into a name or string-literal node's Text field
// so it can be read without the printer. Only the node kinds getTextOfNode
// resolves lazily are handled; anything else is left untouched.
func (p *Printer) bakeText(node *ast.Node, text string) {
	switch node.Kind {
	case ast.KindIdentifier:
		node.AsIdentifier().Text = text
	case ast.KindPrivateIdentifier:
		node.AsPrivateIdentifier().Text = text
	case ast.KindStringLiteral:
		node.AsStringLiteral().Text = text
	}
}
