package transpiler

import (
	"github.com/iceisfun/typescript/ast"
	"github.com/iceisfun/typescript/evaluator"
	"github.com/iceisfun/typescript/jsnum"
	"github.com/iceisfun/typescript/printer"
)

// emitResolver is a checker-free printer.EmitResolver sufficient for the
// isolatedModules transform path. The full EmitResolver is normally the type
// checker; here only GetEnumMemberValue is actually exercised (enum/namespace
// lowering), so it is the only method implemented. The embedded nil interface
// supplies the rest of the (unused) method set; a call to any of them would
// panic, which the transpiler recovers into an error.
type emitResolver struct {
	printer.EmitResolver // nil; provides the method set, never called on this path

	// values caches the computed member values of each enum declaration.
	values map[*ast.Node]map[*ast.Node]evaluator.Result
}

func newEmitResolver() *emitResolver {
	return &emitResolver{values: map[*ast.Node]map[*ast.Node]evaluator.Result{}}
}

// GetEnumMemberValue returns the constant value of an enum member so the runtime
// syntax transform can emit reverse mappings (E[E.A = 0] = "A"). It mirrors the
// checker's computeEnumMemberValues but resolves member references syntactically
// (by name, within the same enum) rather than through symbols — covering
// auto-increment, literal/arithmetic/template initializers, and references to
// earlier members of the same enum. Anything it cannot fold syntactically
// (cross-enum or const-variable references) evaluates to nil, which the
// transform handles by emitting a computed (non-reverse-mapped) member.
func (r *emitResolver) GetEnumMemberValue(node *ast.Node) evaluator.Result {
	enumDecl := node.Parent
	members := r.computeEnum(enumDecl)
	if res, ok := members[node]; ok {
		return res
	}
	return evaluator.NewResult(nil, false, false, false)
}

func (r *emitResolver) computeEnum(enumDecl *ast.Node) map[*ast.Node]evaluator.Result {
	if cached, ok := r.values[enumDecl]; ok {
		return cached
	}
	values := map[*ast.Node]evaluator.Result{}
	byName := map[string]evaluator.Result{}
	r.values[enumDecl] = values // cache early so a self-reference terminates

	// evaluateEntity resolves an identifier / member access to an earlier member
	// of the same enum, by name.
	eval := evaluator.NewEvaluator(func(expr *ast.Node, _ *ast.Node) evaluator.Result {
		if res, ok := byName[entityName(expr)]; ok {
			return res
		}
		return evaluator.NewResult(nil, false, false, false)
	}, ast.OEKParentheses)

	autoValue := jsnum.Number(0)
	autoOK := true
	for _, member := range enumDecl.Members() {
		var res evaluator.Result
		switch {
		case member.Initializer() != nil:
			res = eval(member.Initializer(), member)
		case autoOK:
			res = evaluator.NewResult(autoValue, false, false, false)
		default:
			res = evaluator.NewResult(nil, false, false, false)
		}
		values[member] = res
		byName[ast.GetTextOfPropertyName(member.Name())] = res
		if n, ok := res.Value.(jsnum.Number); ok {
			autoValue = n + 1
			autoOK = true
		} else {
			autoOK = false
		}
	}
	return values
}

// entityName returns the referenced name for an identifier, a property access
// (E.A -> "A"), or a string-indexed element access (E["A"] -> "A").
func entityName(expr *ast.Node) string {
	switch expr.Kind {
	case ast.KindIdentifier:
		return expr.Text()
	case ast.KindPropertyAccessExpression:
		return expr.Name().Text()
	case ast.KindElementAccessExpression:
		arg := expr.AsElementAccessExpression().ArgumentExpression
		if ast.IsStringLiteralLike(arg) {
			return arg.Text()
		}
	}
	return ""
}
