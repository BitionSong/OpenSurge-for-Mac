package device

import (
	"fmt"
	"os"
	"path/filepath"
)

// CreateEmptyPolicyFile atomically creates a starter without replacing an
// existing policy. The caller may remove the file on rollback only when created
// is true; a previously disabled policy must retain its registrations.
func CreateEmptyPolicyFile(path string) (created bool, err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return false, err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".device-policy-*")
	if err != nil {
		return false, err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if err := file.Chmod(0o640); err != nil {
		return false, err
	}
	if _, err := file.WriteString("{\n  \"devices\": [],\n  \"profiles\": [],\n  \"templates\": [],\n  \"rule_sets\": []\n}\n"); err != nil {
		return false, err
	}
	if err := file.Close(); err != nil {
		return false, err
	}
	if err := os.Link(file.Name(), path); err != nil {
		if !os.IsExist(err) {
			return false, err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return false, err
		}
		if !info.Mode().IsRegular() {
			return false, fmt.Errorf("device policy must be a regular file: %s", path)
		}
		return false, nil
	}
	return true, nil
}
