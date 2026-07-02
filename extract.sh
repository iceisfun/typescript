#!/usr/bin/env bash
# Regenerate this library from an upstream typescript-go checkout.
#
#   ./extract.sh /path/to/typescript-go
#
# It copies the checker-free JS-transpile package closure out of internal/,
# rewrites import paths into this module, and pulls in the embedded assets.
# Hand-written files (transpiler/, go.mod, README, NOTICE, LICENSE) are left
# untouched. After running: `go build ./... && go test ./...`.
set -euo pipefail

SRC="${1:?usage: extract.sh <typescript-go checkout>}"
DST="$(cd "$(dirname "$0")" && pwd)"
OLD="github.com/microsoft/typescript-go/internal/"
NEW="github.com/iceisfun/typescript/"

# The 31-package checker-free closure of the JS emit pipeline
# (parser + transformers{es,ts,module,inliners,jsx} + printer + support).
# Recompute with:
#   go list -deps ./internal/parser ./internal/printer ./internal/binder \
#     ./internal/transformers/... (excluding declarations) | grep internal
PKGS="
ast scanner parser binder printer core collections diagnostics evaluator
jsnum tsoptions tspath vfs vfs/vfsmatch module packagejson sourcemap
stringutil semver glob json locale debug nodebuilder outputpaths
transformers transformers/estransforms transformers/tstransforms
transformers/moduletransforms transformers/inliners transformers/jsxtransforms
"

for p in $PKGS; do
	rm -rf "${DST:?}/$p"
	mkdir -p "$DST/$p"
	find "$SRC/internal/$p" -maxdepth 1 -name '*.go' -not -name '*_test.go' \
		-exec cp {} "$DST/$p/" \;
done

# Rewrite import paths internal/* -> module root.
grep -rl "$OLD" "$DST" --include='*.go' | while read -r f; do
	sed -i "s#$OLD#$NEW#g" "$f"
done

# Embedded assets that are not .go files (localized diagnostics).
[ -d "$SRC/internal/diagnostics/loc" ] && cp -r "$SRC/internal/diagnostics/loc" "$DST/diagnostics/"

echo "extracted $(find "$DST" -name '*.go' -not -name '*_test.go' | wc -l) files; upstream $(git -C "$SRC" rev-parse --short HEAD)"
