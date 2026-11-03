package code

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

type SymbolRef struct {
	Path    string `json:"path"`
	Version string `json:"version"`
	Name    string `json:"name"`
	First   int    `json:"first"`
	Last    int    `json:"last"`
	Hash    string `json:"hash"`
}

type SymbolEntry struct {
	SymbolRef
	ID string `json:"symbol_id"`
}

// Keep IDs small enough to return symbol lists without duplicating all metadata.

func SymbolID(ref SymbolRef) string {
	data, _ := json.Marshal(ref)
	digest := sha256.Sum256(data)
	return ref.Version + ":" + base64.RawURLEncoding.EncodeToString([]byte(ref.Path)) + ":" + fmt.Sprintf("%x", digest[:16])
}

func ParseSymbolID(id string) (SymbolRef, error) {
	parts := strings.Split(id, ":")
	if len(parts) != 3 || len(parts[2]) != 32 {
		return SymbolRef{}, fmt.Errorf("无效 symbol_id，请重新 list_symbols")
	}
	version, err := CodeVersion(parts[0])
	if err != nil || parts[0] == "" {
		return SymbolRef{}, fmt.Errorf("无效 symbol_id 版本")
	}
	p, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return SymbolRef{}, fmt.Errorf("无效 symbol_id 路径")
	}
	return SymbolRef{Path: string(p), Version: version}, nil
}

func FileSymbols(p, v, src string) ([]SymbolEntry, error) {
	fs := token.NewFileSet()
	f, err := parser.ParseFile(fs, p, src, 0)
	if err != nil {
		return nil, err
	}
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(src)))
	out := []SymbolEntry{}
	appendDecl := func(name string, node ast.Node) {
		r := SymbolRef{p, v, name, fs.PositionFor(node.Pos(), false).Line, fs.PositionFor(node.End(), false).Line, hash}
		out = append(out, SymbolEntry{r, SymbolID(r)})
	}
	for _, d := range f.Decls {
		switch n := d.(type) {
		case *ast.FuncDecl:
			name := n.Name.Name
			if n.Recv != nil {
				start := fs.PositionFor(n.Recv.List[0].Type.Pos(), false).Offset
				end := fs.PositionFor(n.Recv.List[0].Type.End(), false).Offset
				name = src[start:end] + "." + name
			}
			appendDecl(name, n)
		case *ast.GenDecl:
			for _, s := range n.Specs {
				switch x := s.(type) {
				case *ast.TypeSpec:
					appendDecl(x.Name.Name, n)
				case *ast.ValueSpec:
					for _, name := range x.Names {
						appendDecl(name.Name, n)
					}
				}
			}
		}
	}
	return out, nil
}
