package index

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	ids, err := Parse([]byte("# packages\n\nenvsnap\n  netdiag  \nenvsnap\n"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if strings.Join(ids, ",") != "envsnap,netdiag" {
		t.Errorf("Parse() = %v", ids)
	}
}

func TestParseRejectsUnsafeID(t *testing.T) {
	if _, err := Parse([]byte("../evil\n")); err == nil {
		t.Fatal("Parse() error = nil")
	}
}

func TestWriteAndRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "INDEX")
	if err := Write(path, []string{"beta", "alpha"}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	ids, err := Read(path)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if strings.Join(ids, ",") != "beta,alpha" {
		t.Errorf("Read() = %v", ids)
	}
}

func TestReadMissing(t *testing.T) {
	if _, err := Read(filepath.Join(t.TempDir(), "INDEX")); !os.IsNotExist(err) {
		t.Errorf("Read() error = %v, want not exist", err)
	}
}
