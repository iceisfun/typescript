package printer

import (
	"github.com/iceisfun/typescript/ast"
	"github.com/iceisfun/typescript/tspath"
)

type SourceFileMetaDataProvider interface {
	GetSourceFileMetaData(path tspath.Path) *ast.SourceFileMetaData
}
