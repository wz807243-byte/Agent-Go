package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"strconv"
	"strings"
)

type symbol struct {
	Name        string
	First, Last int
	Code        string
}
type change struct {
	File, Diff    string
	Before, After []symbol
	Notes         []string
}

var hunk = regexp.MustCompile("(?m)^@@ -([0-9]+)(?:,([0-9]+))? [+]([0-9]+)(?:,([0-9]+))? @@")

func touched(first, last int, matches [][]string, side int) bool {
	for _, m := range matches {
		start, _ := strconv.Atoi(m[side])
		count := 1
		if m[side+1] != "" {
			count, _ = strconv.Atoi(m[side+1])
		}
		if count > 0 && first < start+count && last >= start {
			return true
		}
	}
	return false
}
func symbols(src, path string, matches [][]string, side int) ([]symbol, error) {
	fs := token.NewFileSet()
	f, err := parser.ParseFile(fs, path, src, 0)
	if err != nil {
		return nil, err
	}
	var out []symbol
	for _, d := range f.Decls {
		first := fs.PositionFor(d.Pos(), false).Line
		last := fs.PositionFor(d.End(), false).Line
		if !touched(first, last, matches, side) {
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
		out = append(out, symbol{Name: name, First: first, Last: last, Code: src[start:end]})
	}
	return out, nil
}
func inspect() ([]change, error) {
	return inspectSelected("")
}
func inspectSelected(target string) ([]change, error) {
	names, err := git("diff", "--cached", "--no-renames", "--name-only", "-z", "--", "*.go")
	if err != nil {
		return nil, err
	}
	out := []change{}
	for _, path := range strings.Split(names, "\x00") {
		if path == "" {
			continue
		}
		if target != "" && path != target {
			continue
		}
		patch, err := git("--literal-pathspecs", "diff", "--cached", "--no-renames", "--no-ext-diff", "--no-textconv", "--no-color", "--unified=0", "--", path)
		if err != nil {
			return nil, err
		}
		c := change{File: path, Diff: patch}
		matches := hunk.FindAllStringSubmatch(patch, -1)
		for _, s := range []struct {
			side int
			ref  string
			dest *[]symbol
		}{{1, "HEAD:", &c.Before}, {3, ":", &c.After}} {
			if !touched(1, 1<<30, matches, s.side) {
				continue
			}
			src, err := git("show", s.ref+path)
			if err != nil {
				return nil, fmt.Errorf("读取 %s%s: %w", s.ref, path, err)
			}
			*s.dest, err = symbols(src, path, matches, s.side)
			if err != nil {
				c.Notes = append(c.Notes, err.Error())
			}
		}
		out = append(out, c)
	}
	return out, nil
}
