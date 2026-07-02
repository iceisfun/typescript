package transpiler

import (
	"strings"
	"testing"
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
		{"import-rewrite", "import { add } from './m';\nexport const r = add(1, 2);\n", "m_1.add"},
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
