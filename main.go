package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func git(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	return string(out), err
}

func run() error {
	if len(os.Args) == 1 {
		fmt.Print("GO")
		return nil
	}
	if len(os.Args) != 2 {
		return fmt.Errorf("用法: go run . diff|inspect|review")
	}
	switch os.Args[1] {
	case "diff":
		out, err := git("diff", "--cached", "--no-color", "--")
		if err != nil {
			return err
		}
		if out == "" {
			fmt.Println("暂存区没有变更")
		} else {
			fmt.Print(out)
		}
		return nil
	case "inspect", "review":
		root, err := git("rev-parse", "--show-toplevel")
		if err != nil {
			return err
		}
		if err = os.Chdir(strings.TrimSpace(root)); err != nil {
			return err
		}
		changes, err := inspect()
		if err != nil {
			return err
		}
		if os.Args[1] == "review" {
			return review(changes)
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(changes)
	default:
		return fmt.Errorf("用法: go run . diff|inspect|review")
	}
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
