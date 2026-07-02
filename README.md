# github.com/iceisfun/typescript

> ⚠️ **This is not original work.** It is a mechanical *hoisting* of the
> JavaScript-emit core of **Microsoft's [typescript-go]** out of that project's
> `internal/` directory into an importable Go library. **All of the compiler
> code here — scanner, parser, binder, transformers, printer, and every
> supporting package — is Microsoft's**, copied verbatim and redistributed under
> its original **Apache-2.0** license (see [`LICENSE`](./LICENSE) and
> [`NOTICE`](./NOTICE)). This repository is **not** affiliated with, endorsed by,
> or maintained by Microsoft or the TypeScript team.

## Why this exists

`typescript-go` keeps its entire compiler under `internal/`, which Go forbids
any other module from importing. This repo works around that — and *only* that —
by copying the packages needed to transpile TypeScript to JavaScript, moving them
out of `internal/`, and rewriting their import paths from
`github.com/microsoft/typescript-go/internal/*` to
`github.com/iceisfun/typescript/*`. Nothing about the compiler's behavior is
changed.

## What is actually original here

Only two small things, both trivial glue:

- [`transpiler/`](./transpiler) — a thin wrapper exposing
  `transpiler.Module(src, opts) (js, err)`, replicating the checker-free
  (`isolatedModules`) subset of `internal/compiler`'s emit path.
- [`extract.sh`](./extract.sh) — the reproducible extractor that regenerates the
  hoisted packages from an upstream `typescript-go` checkout.

Everything else is upstream Microsoft code.

## Usage

```go
import "github.com/iceisfun/typescript/transpiler"

js, _ := transpiler.Module("const x: number = 1", transpiler.Options{})
// js == "\"use strict\";\nconst x = 1;\n"
```

Works today: type-annotation erasure, interfaces, generics, class member
visibility, and arrow/function/class type stripping. Enum / const-enum /
namespace lowering additionally needs an `EmitResolver` shim and is not wired yet.

## Staying current with upstream

```sh
./extract.sh /path/to/typescript-go   # re-hoist from a fresh checkout
go build ./... && go test ./...
```

## Provenance

Hoisted from [microsoft/typescript-go][typescript-go] at revision
`99fbd74e429336c6ef05c063ef2edc65d4d1972d`. Licensed under Apache-2.0, identical
to upstream. Bug fixes to the compiler itself belong upstream, not here.

[typescript-go]: https://github.com/microsoft/typescript-go
