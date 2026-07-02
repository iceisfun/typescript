package transpiler

import (
	"strings"
	"testing"

	"github.com/iceisfun/typescript/core"
)

// TestSmoke covers the checker-free type-stripping path that works today.
// Enum/const-enum/namespace lowering additionally needs an EmitResolver shim
// (GetEnumMemberValue via the evaluator package) and is tracked separately.
func TestSmoke(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"type-annotation", "const x: number = 1;\n", "const x = 1;"},
		{"interface+fn", "interface P { a: number }\nfunction f(p: P): number { return p.a; }\n", "function f(p) { return p.a; }"},
		{"class", "class C {\n  private n: number = 3;\n  greet(name: string): string { return `hi ${name}`; }\n}\n", "class C"},
		{"arrow", "const y = (a: number, b: number): number => a + b;\n", "a + b"},
		{"generics", "function id<T>(v: T): T { return v; }\n", "function id(v) { return v; }"},
	}
	for _, c := range cases {
		js, err := Module(c.src, Options{})
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if !strings.Contains(js, c.want) {
			t.Errorf("%s: want substring %q in:\n%s", c.name, c.want, js)
		}
		t.Logf("[%s]\n%s=>\n%s", c.name, c.src, js)
	}
}

// TestCommonJSImports checks that binding is wired so import references are
// rewritten to require()-qualified accesses under Module: CommonJS.
func TestCommonJSImports(t *testing.T) {
	js, err := Module("import { add } from './m';\nexport const r = add(1, 2);\n",
		Options{Module: core.ModuleKindCommonJS})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(js, `require("./m")`) || !strings.Contains(js, "m_1.add") {
		t.Fatalf("import not lowered to require+qualified ref:\n%s", js)
	}
}

// TestEnums checks that enum/namespace lowering works (via the checker-free
// EmitResolver): auto-increment, explicit values, string members, cross-member
// arithmetic/bitwise references, and namespaces.
func TestEnums(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"auto-increment", "enum C { Red, Green, Blue }", `C["Blue"] = 2`},
		{"continued-auto", "enum E { A = 1, B, C }", `E["C"] = 3`},
		{"string", `enum S { A = "a" }`, `S["A"] = "a"`},
		{"bitwise-ref", "enum F { A = 1 << 0, B = 1 << 1, C = A | B }", `F["C"] = 3`},
		{"namespace", "namespace N { export const x = 1; }", "N.x = 1"},
	}
	for _, c := range cases {
		js, err := Module(c.src+"\n", Options{})
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if !strings.Contains(js, c.want) {
			t.Errorf("%s: want %q in:\n%s", c.name, c.want, js)
		}
	}
}
