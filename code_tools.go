package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

func codeVersion(v string) (string, error) {
	if v == "" {
		v = "working"
	}
	if v != "working" && v != "staged" && v != "head" {
		return "", fmt.Errorf("version 必须为 working、staged 或 head")
	}
	return v, nil
}

func versionRead(p, v string, files []string) (string, error) {
	v, err := codeVersion(v)
	if err != nil {
		return "", err
	}
	if v == "working" {
		return agentRead(p, files)
	}
	allowed := false
	for _, f := range files {
		if f == p {
			allowed = true
			break
		}
	}
	if !allowed || path.IsAbs(p) || path.Clean(p) != p || strings.Contains(p, "\\") || p == ".." || strings.HasPrefix(p, "../") {
		return "", fmt.Errorf("文件不在可读取的项目范围")
	}
	ref := ":" + p
	metaArgs := []string{"--literal-pathspecs", "ls-files", "--stage", "--", p}
	if v == "head" {
		ref = "HEAD:" + p
		metaArgs = []string{"--literal-pathspecs", "ls-tree", "HEAD", "--", p}
	}
	meta, err := git(metaArgs...)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(meta, "100644 ") && !strings.HasPrefix(meta, "100755 ") {
		return "", fmt.Errorf("该版本不存在普通文件或含未合并条目: %s", p)
	}
	cmd := exec.Command("git", "show", ref)
	var out cappedBuffer
	out.limit = 256000
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("读取 %s %s: %w", v, p, err)
	}
	if out.overflow {
		return "", fmt.Errorf("文件超过 256 KB")
	}
	return out.String(), nil
}

// Keep subprocess output bounded while still draining its pipes.
type cappedBuffer struct {
	buffer   bytes.Buffer
	limit    int
	overflow bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	remaining := b.limit - b.buffer.Len()
	if n > remaining {
		b.overflow = true
		p = p[:remaining]
	}
	b.buffer.Write(p)
	return n, nil
}

func (b *cappedBuffer) String() string { return b.buffer.String() }
func (b *cappedBuffer) Bytes() []byte  { return b.buffer.Bytes() }

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
		out, err := git(args...)
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

func pageTool(name string, a agentArgs, files []string) (string, error) {
	v, err := codeVersion(a.Version)
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

var _ io.Writer = (*cappedBuffer)(nil)
