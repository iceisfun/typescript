# github.com/iceisfun/typescript

A pure-Go, dependency-light **TypeScript → JavaScript transpiler** — the
checker-free `transpileModule` subset of [typescript-go][] extracted from
`internal/` into an importable library.

```go
js, _ := transpiler.Module("const x: number = 1", transpiler.Options{})
// -> "use strict";\nconst x = 1;
```

Works today: type-annotation erasure, interfaces, generics, class member
visibility, arrow/function/class type stripping. Enum/const-enum/namespace
lowering needs an EmitResolver shim (see transpiler/, tracked).

Derived from [typescript-go][] @ `99fbd74e429336c6ef05c063ef2edc65d4d1972d` (Apache-2.0). Re-extract with `./extract.sh`.

[typescript-go]: https://github.com/microsoft/typescript-go
