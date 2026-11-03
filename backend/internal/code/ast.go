package code

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

type Symbol struct {
	Name        string
	First, Last int
	Code        string
}

func Symbols(src, path string, matches [][]string, side int) ([]Symbol, error) {
	fs := token.NewFileSet()
	f, err := parser.ParseFile(fs, path, src, 0)
	if err != nil {
		return nil, err
	}
	var out []Symbol
	for _, d := range f.Decls {
		first := fs.PositionFor(d.Pos(), false).Line
		last := fs.PositionFor(d.End(), false).Line
		if !Touched(first, last, matches, side) {
			continue
		}
		name := ""
		switch v := d.(type) {
		case *ast.FuncDecl:
			name = v.Name.Name
			if v.Recv != nil {
				name = src[fs.PositionFor(v.Recv.Pos(), false).Offset:fs.PositionFor(v.Recv.End(), false).Offset] + "." + name
			}
		case *ast.GenDecl:
			start := fs.PositionFor(v.Pos(), false).Offset
			end := fs.PositionFor(v.End(), false).Offset
			name = strings.SplitN(src[start:end], "\n", 2)[0]
			if len([]rune(name)) > 100 {
				name = string([]rune(name)[:100])
			}
		}
		start := fs.PositionFor(d.Pos(), false).Offset
		end := fs.PositionFor(d.End(), false).Offset
		out = append(out, Symbol{Name: name, First: first, Last: last, Code: src[start:end]})
	}
	return out, nil
}
