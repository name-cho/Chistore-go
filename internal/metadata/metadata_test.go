package metadata

import (
	"strings"
	"testing"
)

func TestParseFullInfo(t *testing.T) {
	info := `## Name: EnvSnap
## Description: Snapshot system & environment state: with diff support
## Version: 1.0.1
## Size: 3.8K
## Type: single
## MainFile: envsnap
## Author: name-cho
## Homepage: https://github.com/name-cho
## Source: https://example.com/envsnap
## Dependencies: sys:curl,bash pip:psutil,rich npm:prettier
## Notes: Run envsnap --init once
## Notes: Config lives in ~/.config/envsnap
`
	pkg, err := Parse("envsnap", strings.NewReader(info))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if pkg.ID != "envsnap" {
		t.Errorf("ID = %q, want envsnap", pkg.ID)
	}
	if pkg.Name != "EnvSnap" {
		t.Errorf("Name = %q, want EnvSnap", pkg.Name)
	}
	if pkg.Description != "Snapshot system & environment state: with diff support" {
		t.Errorf("Description = %q", pkg.Description)
	}
	if pkg.Version != "1.0.1" {
		t.Errorf("Version = %q, want 1.0.1", pkg.Version)
	}
	if pkg.Size != "3.8K" {
		t.Errorf("Size = %q, want 3.8K", pkg.Size)
	}
	if pkg.Kind != KindSingle {
		t.Errorf("Kind = %q, want single", pkg.Kind)
	}
	if pkg.MainFile != "envsnap" {
		t.Errorf("MainFile = %q, want envsnap", pkg.MainFile)
	}
	if pkg.Author != "name-cho" {
		t.Errorf("Author = %q, want name-cho", pkg.Author)
	}
	if pkg.Homepage != "https://github.com/name-cho" {
		t.Errorf("Homepage = %q", pkg.Homepage)
	}

	wantDeps := "sys:curl,bash pip:psutil,rich npm:prettier"
	if got := FormatDependencies(pkg.Dependencies); got != wantDeps {
		t.Errorf("Dependencies = %q, want %q", got, wantDeps)
	}
	wantNotes := []string{"Run envsnap --init once", "Config lives in ~/.config/envsnap"}
	if len(pkg.Notes) != len(wantNotes) || pkg.Notes[0] != wantNotes[0] || pkg.Notes[1] != wantNotes[1] {
		t.Errorf("Notes = %q, want %q", pkg.Notes, wantNotes)
	}
}

func TestParseDefaults(t *testing.T) {
	pkg, err := Parse("demo", strings.NewReader("## Description: A demo\n"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if pkg.Name != "demo" {
		t.Errorf("Name = %q, want demo", pkg.Name)
	}
	if pkg.Version != "?" {
		t.Errorf("Version = %q, want ?", pkg.Version)
	}
	if pkg.Kind != KindSingle {
		t.Errorf("Kind = %q, want single", pkg.Kind)
	}
	if pkg.MainFile != "demo" {
		t.Errorf("MainFile = %q, want demo", pkg.MainFile)
	}
	if len(pkg.Dependencies) != 0 {
		t.Errorf("Dependencies = %v, want none", pkg.Dependencies)
	}
}

func TestParseSkipsEmptyNotes(t *testing.T) {
	info := "## Notes: \n## Notes: keep this\n## Notes:\n"
	pkg, err := Parse("demo", strings.NewReader(info))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(pkg.Notes) != 1 || pkg.Notes[0] != "keep this" {
		t.Errorf("Notes = %q, want one note", pkg.Notes)
	}
}

func TestParseAcceptsPlainAndCRLFLines(t *testing.T) {
	info := "\ufeffName: Demo\r\nType: archive\r\nMainFile: bin/demo\r\n"
	pkg, err := Parse("demo", strings.NewReader(info))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if pkg.Name != "Demo" || pkg.Kind != KindArchive || pkg.MainFile != "bin/demo" {
		t.Errorf("unexpected package: %+v", pkg)
	}
}

func TestParseRejectsInvalidKind(t *testing.T) {
	_, err := Parse("demo", strings.NewReader("## Type: binary\n"))
	if err == nil {
		t.Fatal("Parse() error = nil, want invalid kind")
	}
}

func TestParseRejectsInvalidID(t *testing.T) {
	for _, id := range []string{"", "../x", "a/b", ".hidden", "a b"} {
		if _, err := Parse(id, strings.NewReader("")); err == nil {
			t.Errorf("Parse(%q) error = nil, want error", id)
		}
	}
}

func TestParseRejectsTraversalMainFile(t *testing.T) {
	for _, main := range []string{"../demo", "/bin/demo", `bin\demo`} {
		_, err := Parse("demo", strings.NewReader("## MainFile: "+main+"\n"))
		if err == nil {
			t.Errorf("Parse(MainFile=%q) error = nil, want error", main)
		}
	}
}

func TestParseDependenciesAliasesAndMerging(t *testing.T) {
	deps, err := ParseDependencies("python:rich apt:curl dnf:git,bash pip:psutil python:pyfiglet")
	if err != nil {
		t.Fatalf("ParseDependencies() error = %v", err)
	}
	want := "sys:curl,git,bash pip:rich,psutil,pyfiglet"
	if got := FormatDependencies(deps); got != want {
		t.Errorf("dependencies = %q, want %q", got, want)
	}
}

func TestParseDependenciesRejectsUnknownManager(t *testing.T) {
	_, err := ParseDependencies("cargo:ripgrep")
	if err == nil {
		t.Fatal("ParseDependencies() error = nil, want error")
	}
}

func TestValidateID(t *testing.T) {
	for _, id := range []string{"envsnap", "desk-gen", "a_b.c", "A1"} {
		if err := ValidateID(id); err != nil {
			t.Errorf("ValidateID(%q) = %v, want nil", id, err)
		}
	}
	for _, id := range []string{"", ".", "..", "-x", "a/b", "a b", "a\\b", "a:b"} {
		if err := ValidateID(id); err == nil {
			t.Errorf("ValidateID(%q) = nil, want error", id)
		}
	}
}
