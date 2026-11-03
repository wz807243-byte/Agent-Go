package tools

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"example.com/Agent-Go/internal/code"
)

func Read(path string, files []string) (string, error) {
	allowed := false
	for _, p := range files {
		if p == path {
			allowed = true
			break
		}
	}
	if !allowed {
		return "", fmt.Errorf("文件不在可读取的 Go 项目文件列表中")
	}
	root, err := os.Getwd()
	if err != nil {
		return "", err
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(root, resolved)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("文件超出项目目录")
	}
	f, err := os.Open(resolved)
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("需要普通文件")
	}
	data, err := io.ReadAll(io.LimitReader(f, 256001))
	if err != nil {
		return "", err
	}
	if len(data) > 256000 {
		return "", fmt.Errorf("文件超过 256 KB")
	}
	return string(data), nil
}

func ReviewFile(path string) bool {
	name := filepath.Base(path)
	return strings.HasSuffix(path, ".go") || name == "go.mod" || name == "任务说明.md" || strings.EqualFold(name, "README.md")
}

func versionRead(p, v string, files []string) (string, error) {
	v, err := code.CodeVersion(v)
	if err != nil {
		return "", err
	}
	if v == "working" {
		return Read(p, files)
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
	meta, err := code.Git(metaArgs...)
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

func (b *cappedBuffer) Bytes() []byte { return b.buffer.Bytes() }
