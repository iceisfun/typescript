package sourcemap

import "github.com/iceisfun/typescript/core"

type Source interface {
	Text() string
	FileName() string
	ECMALineMap() []core.TextPos
}
