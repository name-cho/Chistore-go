package pkgtool

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/name-cho/chistore-go/internal/index"
)

const goodInfo = `## Name: Alpha
## Description: test package
## Version: 1.0.0
## Type: single
## MainFile: alpha
`

func writeFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func TestInit(t *testing.T) {
	root := t.TempDir()
	tool := New(root)

	if err := tool.Init("demo"); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	infoPath := filepath.Join(root, "demo", "info.md")
	info, err := os.ReadFile(infoPath)
	if err != nil || !strings.Contains(string(info), "## MainFile: demo") {
		t.Fatalf("info.md = %q, %v", info, err)
	}
	scriptPath := filepath.Join(root, "demo", "demo")
	script, err := os.ReadFile(scriptPath)
	if err != nil || !strings.HasPrefix(string(script), "#!/usr/bin/env bash") {
		t.Fatalf("script = %q, %v", script, err)
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(scriptPath)
		if info.Mode().Perm()&0o111 == 0 {
			t.Errorf("script is not executable: %v", info.Mode())
		}
	}
	if _, err := os.Stat(filepath.Join(root, "INDEX")); !os.IsNotExist(err) {
		t.Errorf("INDEX should not be created in a new repository")
	}
	if _, err := os.Stat(filepath.Join(root, "index.json")); err != nil {
		t.Errorf("index.json missing: %v", err)
	}
}

func TestInitRejectsExistingFiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "demo", "info.md"), "exists", 0o644)
	if err := New(root).Init("demo"); err == nil {
		t.Fatal("Init() error = nil")
	}
}

func TestIndex(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "beta", "info.md"), "## Name: Beta\n## Version: 2.0.0\n## MainFile: beta\n", 0o644)
	writeFile(t, filepath.Join(root, "beta", "beta"), "#!/bin/sh\necho beta\n", 0o755)
	writeFile(t, filepath.Join(root, "alpha", "info.md"), "## Name: Alpha\n## Version: 1.0.0\n## MainFile: alpha\n", 0o644)
	writeFile(t, filepath.Join(root, "alpha", "alpha"), "#!/bin/sh\necho alpha\n", 0o755)
	writeFile(t, filepath.Join(root, "noinfo", "file"), "x", 0o644)
	writeFile(t, filepath.Join(root, ".hidden", "info.md"), "## Name: Hidden\n", 0o644)
	writeFile(t, filepath.Join(root, "INDEX"), "stale\n", 0o644)

	ids, err := New(root).Index()
	if err != nil {
		t.Fatalf("Index() error = %v", err)
	}
	if strings.Join(ids, ",") != "alpha,beta" {
		t.Errorf("Index() = %v", ids)
	}
	data, _ := os.ReadFile(filepath.Join(root, "INDEX"))
	if string(data) != "alpha\nbeta\n" {
		t.Errorf("INDEX = %q", data)
	}

	manifestData, err := os.ReadFile(filepath.Join(root, "index.json"))
	if err != nil {
		t.Fatalf("index.json error = %v", err)
	}
	manifest, err := index.ParseManifest(manifestData)
	if err != nil {
		t.Fatalf("ParseManifest() error = %v", err)
	}
	if len(manifest.Packages) != 2 || manifest.Packages[0].ID != "alpha" || manifest.Packages[0].Version != "1.0.0" {
		t.Fatalf("manifest = %+v", manifest)
	}
	sum := sha256.Sum256([]byte("#!/bin/sh\necho alpha\n"))
	if manifest.Packages[0].SHA256 != hex.EncodeToString(sum[:]) {
		t.Errorf("sha256 = %q", manifest.Packages[0].SHA256)
	}
}

func TestValidateCleanRepository(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "INDEX"), "alpha\nbeta\n", 0o644)
	writeFile(t, filepath.Join(root, "alpha", "info.md"), goodInfo, 0o644)
	writeFile(t, filepath.Join(root, "alpha", "alpha"), "#!/bin/sh\n", 0o755)
	writeFile(t, filepath.Join(root, "beta", "info.md"), "## Type: archive\n## MainFile: bin/beta\n## Version: 2.0.0\n", 0o644)
	writeFile(t, filepath.Join(root, "beta", "beta.tar.gz"), "archive", 0o644)

	report, err := New(root).Validate()
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if !report.OK() {
		t.Errorf("report = %+v, want clean", report)
	}
}

func TestValidateReportsProblems(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "INDEX"), "missing\nbad\nbroken\narchive\n", 0o644)
	writeFile(t, filepath.Join(root, "bad", "info.md"), "## Type: binary\n", 0o644)
	writeFile(t, filepath.Join(root, "broken", "info.md"), goodInfo, 0o644)
	writeFile(t, filepath.Join(root, "archive", "info.md"), "## Type: archive\n## MainFile: bin/archive\n", 0o644)
	writeFile(t, filepath.Join(root, "unlisted", "info.md"), goodInfo, 0o644)
	writeFile(t, filepath.Join(root, "unlisted", "alpha"), "#!/bin/sh\n", 0o755)
	writeFile(t, filepath.Join(root, "noinfo", "file"), "x", 0o644)

	report, err := New(root).Validate()
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	joined := strings.Join(report.Errors, "\n")
	for _, want := range []string{"missing", "unknown type", "main file", "archive.tar.gz", "not listed"} {
		if !strings.Contains(joined, want) {
			t.Errorf("errors %q do not contain %q", joined, want)
		}
	}
	if len(report.Warnings) == 0 || !strings.Contains(strings.Join(report.Warnings, "\n"), "no info.md") {
		t.Errorf("warnings = %v", report.Warnings)
	}
}

func TestValidateDetectsManifestDrift(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "alpha", "info.md"), goodInfo, 0o644)
	writeFile(t, filepath.Join(root, "alpha", "alpha"), "#!/bin/sh\n", 0o755)
	if _, err := New(root).Index(); err != nil {
		t.Fatalf("Index() error = %v", err)
	}

	report, err := New(root).Validate()
	if err != nil || !report.OK() {
		t.Fatalf("Validate() = %+v, %v", report, err)
	}

	writeFile(t, filepath.Join(root, "alpha", "alpha"), "#!/bin/sh\necho changed\n", 0o755)
	report, err = New(root).Validate()
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if report.OK() {
		t.Fatal("Validate() passed after payload changed")
	}
	if !strings.Contains(strings.Join(report.Errors, "\n"), "sha256") {
		t.Errorf("errors = %v", report.Errors)
	}

	writeFile(t, filepath.Join(root, "alpha", "info.md"), strings.ReplaceAll(goodInfo, "1.0.0", "1.1.0"), 0o644)
	report, err = New(root).Validate()
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if !strings.Contains(strings.Join(report.Errors, "\n"), "version") {
		t.Errorf("errors = %v", report.Errors)
	}
}

func TestValidateMissingIndex(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "alpha", "info.md"), goodInfo, 0o644)
	writeFile(t, filepath.Join(root, "alpha", "alpha"), "#!/bin/sh\n", 0o755)

	report, err := New(root).Validate()
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if len(report.Errors) == 0 {
		t.Fatalf("report = %+v, want missing INDEX error", report)
	}
}
