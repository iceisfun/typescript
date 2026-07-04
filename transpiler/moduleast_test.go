package transpiler

import (
	"strings"
	"testing"

	"github.com/iceisfun/typescript/ast"
)

// walk visits node and all descendants, invoking v on each.
func walk(node *ast.Node, v func(*ast.Node)) {
	if node == nil {
		return
	}
	v(node)
	node.ForEachChild(func(child *ast.Node) bool {
		walk(child, v)
		return false
	})
}

// isTypeScriptOnly reports whether a node kind is TypeScript-only and therefore
// must not survive transpilation (type nodes plus the declaration forms the
// transforms lower or erase).
func isTypeScriptOnly(k ast.Kind) bool {
	if k >= ast.KindFirstTypeNode && k <= ast.KindLastTypeNode {
		return true
	}
	switch k {
	case ast.KindInterfaceDeclaration,
		ast.KindTypeAliasDeclaration,
		ast.KindEnumDeclaration,
		ast.KindModuleDeclaration:
		return true
	}
	return false
}

func TestModuleAST_ErasesTypesAndLowers(t *testing.T) {
	const src = `
interface Point { x: number; y: number }
type Id = string;
enum Color { Red, Green, Blue }
namespace Geo { export const origin: Point = { x: 0, y: 0 }; }
class Box<T> {
	private items: T[] = [];
	constructor(readonly label: string) {}
	add(item: T): void { this.items.push(item); }
}
const c: Color = Color.Green;
const b = new Box<number>("nums");
b.add(1);
const greet = (name: string): string => ` + "`hi ${name}`" + `;
export const answer: number = 42;
`
	sf, err := ModuleAST(src, Options{FileName: "sample.ts"})
	if err != nil {
		t.Fatalf("ModuleAST: %v", err)
	}
	if sf == nil || sf.Statements == nil || len(sf.Statements.Nodes) == 0 {
		t.Fatal("ModuleAST returned an empty source file")
	}

	var (
		sawEnumLowering bool
		emptyIdents     int
	)
	walk(sf.AsNode(), func(n *ast.Node) {
		if isTypeScriptOnly(n.Kind) {
			t.Errorf("TypeScript-only node survived transpilation: %v", n.Kind)
		}
		// A generated name left unresolved would carry empty text; baking must
		// have populated every identifier.
		if n.Kind == ast.KindIdentifier && n.Text() == "" {
			emptyIdents++
		}
		// The enum lowers to `var Color; (function (Color) { ... })(Color || ...)`,
		// i.e. a variable statement — a rough signal the runtime-syntax transform
		// ran and its synthesized names were baked.
		if n.Kind == ast.KindVariableStatement {
			sawEnumLowering = true
		}
	})
	if emptyIdents != 0 {
		t.Errorf("%d identifier(s) have empty text — a generated name was not baked", emptyIdents)
	}
	if !sawEnumLowering {
		t.Error("expected the enum to lower to a variable statement")
	}
}

// TestModuleAST_MatchesModuleShape sanity-checks that ModuleAST agrees with the
// text path on the identifiers present: every non-generated identifier the text
// output contains should also appear (by text) somewhere in the AST.
func TestModuleAST_MatchesModuleShape(t *testing.T) {
	const src = `
export function fib(n: number): number { return n < 2 ? n : fib(n - 1) + fib(n - 2); }
const xs: number[] = [1, 2, 3].map((v) => v * 2);
`
	js, err := Module(src, Options{FileName: "shape.ts"})
	if err != nil {
		t.Fatalf("Module: %v", err)
	}
	sf, err := ModuleAST(src, Options{FileName: "shape.ts"})
	if err != nil {
		t.Fatalf("ModuleAST: %v", err)
	}
	names := map[string]bool{}
	walk(sf.AsNode(), func(n *ast.Node) {
		if n.Kind == ast.KindIdentifier {
			names[n.Text()] = true
		}
	})
	for _, want := range []string{"fib", "xs", "map"} {
		if !names[want] {
			t.Errorf("identifier %q present in text output but missing from AST", want)
		}
		if !strings.Contains(js, want) {
			t.Errorf("sanity: %q missing from text output %q", want, js)
		}
	}
}
