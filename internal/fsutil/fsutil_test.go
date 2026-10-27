package fsutil

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWriteFileAtomic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "file.txt")
	if err := WriteFileAtomic(path, []byte("first"), 0o600); err != nil {
		t.Fatalf("WriteFileAtomic() error = %v", err)
	}
	if err := WriteFileAtomic(path, []byte("second"), 0o755); err != nil {
		t.Fatalf("WriteFileAtomic(overwrite) error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "second" {
		t.Fatalf("content = %q, %v", data, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o755 {
		t.Errorf("mode = %v, want 0755", info.Mode().Perm())
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "file.txt" {
		t.Errorf("temporary files left behind: %v", entries)
	}
}

func TestReplaceFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := os.WriteFile(src, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ReplaceFile(src, dst); err != nil {
		t.Fatalf("ReplaceFile() error = %v", err)
	}
	data, _ := os.ReadFile(dst)
	if string(data) != "new" {
		t.Errorf("dst = %q, want new", data)
	}
}

func TestIsWithin(t *testing.T) {
	base := t.TempDir()
	if !IsWithin(base, filepath.Join(base, "a", "b")) {
		t.Error("child is not within base")
	}
	for _, path := range []string{filepath.Join(base, "..", "evil"), filepath.Dir(base)} {
		if IsWithin(base, path) {
			t.Errorf("IsWithin(%q) = true", path)
		}
	}
}

func TestSanitizeArchiveName(t *testing.T) {
	base := t.TempDir()
	got, err := ArchiveTarget(base, "bin/demo")
	if err != nil || got != filepath.Join(base, "bin", "demo") {
		t.Fatalf("ArchiveTarget() = %q, %v", got, err)
	}
	for _, name := range []string{"../evil", `..\evil`, "/etc/passwd", "bin/../../evil", "C:/evil"} {
		if _, err := ArchiveTarget(base, name); err == nil {
			t.Errorf("ArchiveTarget(%q) error = nil", name)
		}
	}
	if !strings.HasPrefix(got, base) {
		t.Errorf("target %q escapes %q", got, base)
	}
}
