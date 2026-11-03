package observability

import (
	"encoding/json"
	"os"
	"strings"
)

func SaveRun(dir string, r RunRecord) (string, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(dir, "run-*.tmp")
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())
	encoder := json.NewEncoder(f)
	encoder.SetIndent("", "  ")
	err = encoder.Encode(r)
	closeErr := f.Close()
	if err != nil {
		return "", err
	}
	if closeErr != nil {
		return "", closeErr
	}
	path := strings.TrimSuffix(f.Name(), ".tmp") + ".json"
	if err = os.Rename(f.Name(), path); err != nil {
		return "", err
	}
	return path, nil
}
