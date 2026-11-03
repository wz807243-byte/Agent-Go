package code

import (
	"fmt"
)

func CodeVersion(v string) (string, error) {
	if v == "" {
		v = "working"
	}
	if v != "working" && v != "staged" && v != "head" {
		return "", fmt.Errorf("version 必须为 working、staged 或 head")
	}
	return v, nil
}
