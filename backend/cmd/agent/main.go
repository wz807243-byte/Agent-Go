package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"example.com/Agent-Go/internal/agent"
	"example.com/Agent-Go/internal/code"
	"example.com/Agent-Go/internal/review"
)

func run() error {
	if len(os.Args) == 1 {
		fmt.Print("GO")
		return nil
	}
	if os.Args[1] == "agent" {
		if len(os.Args) >= 3 && (os.Args[2] == "--file" || os.Args[2] == "--path") {
			if len(os.Args) < 4 {
				return fmt.Errorf("用法: go run ./backend/cmd/agent agent --file backend/internal/review/service.go [审查要求]")
			}
			task := strings.Join(os.Args[4:], " ")
			if task == "" {
				task = "检查指定文件中的正确性问题"
			}
			return agent.RunPath(task, os.Args[3])
		}
		if len(os.Args) < 3 {
			return fmt.Errorf("用法: go run ./backend/cmd/agent agent 检查当前项目")
		}
		return agent.Run(strings.Join(os.Args[2:], " "))
	}
	if len(os.Args) != 2 {
		return fmt.Errorf("用法: go run ./backend/cmd/agent diff|inspect|review")
	}
	switch os.Args[1] {
	case "diff":
		out, err := code.Git("diff", "--cached", "--no-color", "--")
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
		root, err := code.Git("rev-parse", "--show-toplevel")
		if err != nil {
			return err
		}
		if err = os.Chdir(strings.TrimSpace(root)); err != nil {
			return err
		}
		changes, err := code.Inspect()
		if err != nil {
			return err
		}
		if os.Args[1] == "review" {
			return review.Run(changes)
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(changes)
	default:
		return fmt.Errorf("用法: go run ./backend/cmd/agent diff|inspect|review")
	}
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
