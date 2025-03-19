package parser

import (
	"github.com/Shnitzelil/goja/ast"
	"github.com/Shnitzelil/goja/unistring"
)

type _scope struct {
	outer             *_scope
	allowIn           bool
	allowLet          bool
	allowImportExport bool
	inIteration       bool
	inSwitch          bool
	inFuncParams      bool
	inFunction        bool
	inAsync           bool
	allowAwait        bool
	allowYield        bool
	declarationList   []*ast.VariableDeclaration
	importEntries     []*ast.ImportDeclaration
	exportEntries     []*ast.ExportDeclaration
	hasTLA            bool

	labels []unistring.String
}

func (self *_parser) openScope() {
	self.scope = &_scope{
		outer:   self.scope,
		allowIn: true,
	}
}

func (self *_parser) closeScope() {
	self.scope = self.scope.outer
}

func (self *_scope) declare(declaration *ast.VariableDeclaration) {
	self.declarationList = append(self.declarationList, declaration)
}

func (self *_scope) hasLabel(name unistring.String) bool {
	for _, label := range self.labels {
		if label == name {
			return true
		}
	}
	if self.outer != nil && !self.inFunction {
		// Crossing a function boundary to look for a label is verboten
		return self.outer.hasLabel(name)
	}
	return false
}
