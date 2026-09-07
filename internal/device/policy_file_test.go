package device

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCreateEmptyPolicyFileRejectsSymlinkWithoutChangingTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "existing.json")
	if err := os.WriteFile(target, []byte("existing policy"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "policy.json")
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if created, err := CreateEmptyPolicyFile(path); err == nil || created {
		t.Fatalf("created=%v error=%v", created, err)
	}
	actual, err := os.ReadFile(target)
	if err != nil || string(actual) != "existing policy" {
		t.Fatalf("symlink target changed: %q, %v", actual, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 2 {
		t.Fatalf("temporary file left behind: %v, %v", entries, err)
	}
}
