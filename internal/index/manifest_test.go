package index

import (
	"strings"
	"testing"
	"time"

	"github.com/name-cho/chistore-go/internal/metadata"
)

func samplePackage(id, version string) metadata.Package {
	return metadata.Package{
		ID:          id,
		Name:        strings.ToUpper(id),
		Description: "sample",
		Version:     version,
		Size:        "1K",
		Kind:        metadata.KindSingle,
		MainFile:    id,
		SHA256:      strings.Repeat("a", 64),
	}
}

func TestManifestRoundTrip(t *testing.T) {
	manifest := NewManifest("demo", []metadata.Package{
		samplePackage("beta", "2.0.0"),
		samplePackage("alpha", "1.0.0"),
	}, time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC))

	data, err := manifest.Encode()
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	parsed, err := ParseManifest(data)
	if err != nil {
		t.Fatalf("ParseManifest() error = %v", err)
	}
	if len(parsed.Packages) != 2 || parsed.Packages[0].ID != "alpha" || parsed.Packages[1].ID != "beta" {
		t.Fatalf("packages = %+v", parsed.Packages)
	}
	if parsed.Packages[0].SHA256 != strings.Repeat("a", 64) {
		t.Errorf("sha256 = %q", parsed.Packages[0].SHA256)
	}
	if parsed.Generated != "2026-10-27T12:00:00Z" {
		t.Errorf("generated = %q", parsed.Generated)
	}
}

func TestParseManifestRejectsBadInput(t *testing.T) {
	cases := map[string]string{
		"schema":    `{"schema":2,"packages":[]}`,
		"json":      `{`,
		"duplicate": `{"schema":1,"packages":[{"id":"a","version":"1","type":"single","main":"a"},{"id":"a","version":"1","type":"single","main":"a"}]}`,
		"sha":       `{"schema":1,"packages":[{"id":"a","version":"1","type":"single","main":"a","sha256":"xyz"}]}`,
		"type":      `{"schema":1,"packages":[{"id":"a","version":"1","type":"zip","main":"a"}]}`,
		"main":      `{"schema":1,"packages":[{"id":"a","version":"1","type":"single","main":"../a"}]}`,
	}
	for name, body := range cases {
		if _, err := ParseManifest([]byte(body)); err == nil {
			t.Errorf("%s: ParseManifest() error = nil", name)
		}
	}
}

func TestParseManifestAllowsEmptyRepository(t *testing.T) {
	manifest, err := ParseManifest([]byte(`{"schema":1,"packages":[]}`))
	if err != nil {
		t.Fatalf("ParseManifest() error = %v", err)
	}
	if manifest.Packages == nil || len(manifest.Packages) != 0 {
		t.Errorf("packages = %#v, want empty slice", manifest.Packages)
	}
}
