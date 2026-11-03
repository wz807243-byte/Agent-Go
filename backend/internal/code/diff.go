package code

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type Change struct {
	File, Diff    string
	Before, After []Symbol
	Notes         []string
}

var Hunk = regexp.MustCompile("(?m)^@@ -([0-9]+)(?:,([0-9]+))? [+]([0-9]+)(?:,([0-9]+))? @@")

func Touched(first, last int, matches [][]string, side int) bool {
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

func Inspect() ([]Change, error) {
	return InspectSelected("")
}

func InspectSelected(target string) ([]Change, error) {
	names, err := Git("diff", "--cached", "--no-renames", "--name-only", "-z", "--", "*.go")
	if err != nil {
		return nil, err
	}
	out := []Change{}
	for _, path := range strings.Split(names, "\x00") {
		if path == "" {
			continue
		}
		if target != "" && path != target {
			continue
		}
		patch, err := Git("--literal-pathspecs", "diff", "--cached", "--no-renames", "--no-ext-diff", "--no-textconv", "--no-color", "--unified=0", "--", path)
		if err != nil {
			return nil, err
		}
		c := Change{File: path, Diff: patch}
		matches := Hunk.FindAllStringSubmatch(patch, -1)
		for _, s := range []struct {
			side int
			ref  string
			dest *[]Symbol
		}{{1, "HEAD:", &c.Before}, {3, ":", &c.After}} {
			if !Touched(1, 1<<30, matches, s.side) {
				continue
			}
			src, err := Git("show", s.ref+path)
			if err != nil {
				return nil, fmt.Errorf("读取 %s%s: %w", s.ref, path, err)
			}
			*s.dest, err = Symbols(src, path, matches, s.side)
			if err != nil {
				c.Notes = append(c.Notes, err.Error())
			}
		}
		out = append(out, c)
	}
	return out, nil
}
