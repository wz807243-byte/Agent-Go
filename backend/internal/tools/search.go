package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"example.com/Agent-Go/internal/code"
)

func Files() ([]string, error) {
	out, err := code.Git("ls-files", "--cached", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	if headBytes, err := exec.Command("git", "ls-tree", "-r", "--name-only", "-z", "HEAD").Output(); err == nil {
		head := string(headBytes)
		out += "\x00" + head
	}
	var files []string
	seen := map[string]bool{}
	for _, p := range strings.Split(out, "\x00") {
		if ReviewFile(p) && !seen[p] {
			files = append(files, p)
			seen[p] = true
		}
	}
	return files, nil
}

type codePage struct {
	Version      string   `json:"version"`
	Items        []string `json:"items"`
	Truncated    bool     `json:"truncated"`
	NextCursor   int      `json:"next_cursor"`
	Skipped      []string `json:"skipped,omitempty"`
	SkippedCount int      `json:"skipped_count"`
}

// Intersect the permission list with paths present in the requested version.

func filesAtVersion(files []string, version string) ([]string, error) {
	present := map[string]bool{}
	if version == "working" {
		for _, p := range files {
			info, err := os.Lstat(filepath.FromSlash(p))
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return nil, err
			}
			if info.Mode().IsRegular() {
				present[p] = true
			}
		}
	} else {
		args := []string{"ls-files", "--cached", "-z"}
		if version == "head" {
			args = []string{"ls-tree", "-r", "--name-only", "-z", "HEAD"}
		}
		out, err := code.Git(args...)
		if err != nil {
			return nil, err
		}
		for _, p := range strings.Split(out, "\x00") {
			present[p] = true
		}
	}
	out := []string{}
	for _, p := range files {
		if present[p] {
			out = append(out, p)
		}
	}
	return out, nil
}

func scopedFiles(files []string, prefix string) ([]string, error) {
	if strings.Contains(prefix, "\\") || strings.HasPrefix(prefix, "/") || strings.Contains(prefix, ":") {
		return nil, fmt.Errorf("path_prefix 必须为项目内的正斜杠路径")
	}
	prefix = strings.TrimSuffix(prefix, "/")
	if prefix != "" && (path.Clean(prefix) != prefix || prefix == ".." || strings.HasPrefix(prefix, "../")) {
		return nil, fmt.Errorf("无效 path_prefix")
	}
	var out []string
	for _, f := range files {
		if prefix == "" || f == prefix || strings.HasPrefix(f, prefix+"/") {
			out = append(out, f)
		}
	}
	sort.Strings(out)
	return out, nil
}

func pageTool(name string, a Args, files []string) (string, error) {
	v, err := code.CodeVersion(a.Version)
	if err != nil {
		return "", err
	}
	files, err = scopedFiles(files, a.PathPrefix)
	if err != nil {
		return "", err
	}
	files, err = filesAtVersion(files, v)
	if err != nil {
		return "", err
	}
	limit := a.Limit
	if limit == 0 {
		limit = 30
	}
	if limit < 1 || limit > 200 || a.Cursor < 0 {
		return "", fmt.Errorf("limit 应为 1-200，cursor 不能为负")
	}
	if name == "search_code" && a.Query == "" {
		return "", fmt.Errorf("query 不能为空")
	}
	page := codePage{Version: v, Items: []string{}}
	index, size := 0, 0
	add := func(item string) bool {
		if index < a.Cursor {
			index++
			return true
		}
		if len(page.Items) >= limit || size+len(item) > 8000 {
			if len(page.Items) == 0 {
				return false
			}
			page.Truncated = true
			page.NextCursor = index
			return false
		}
		page.Items = append(page.Items, item)
		size += len(item)
		index++
		return true
	}
	stop := false
	for _, p := range files {
		if name == "list_files" {
			if !add(p) {
				stop = true
				break
			}
			continue
		}
		src, err := versionRead(p, v, files)
		if err != nil {
			page.SkippedCount++
			if len(page.Skipped) < 30 {
				page.Skipped = append(page.Skipped, p+": "+err.Error())
			}
			continue
		}
		for n, line := range strings.Split(src, "\n") {
			if strings.Contains(line, a.Query) {
				if len(line) > 4000 {
					line = line[:4000] + " [行内容截断，请 read_file]"
				}
				if !add(fmt.Sprintf("%s:%d %s", p, n+1, line)) {
					stop = true
					break
				}
			}
		}
		if stop {
			break
		}
	}
	if stop && !page.Truncated {
		return "", fmt.Errorf("单项过长，请缩小范围")
	}
	data, err := json.Marshal(page)
	return string(data), err
}
