package index

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/name-cho/chistore-go/internal/metadata"
)

const (
	ManifestFile   = "index.json"
	ManifestSchema = 1
)

type Manifest struct {
	Schema    int                `json:"schema"`
	Name      string             `json:"name,omitempty"`
	Generated string             `json:"generated,omitempty"`
	Packages  []metadata.Package `json:"packages"`
}

func ParseManifest(data []byte) (Manifest, error) {
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("parse %s: %w", ManifestFile, err)
	}
	if manifest.Schema != ManifestSchema {
		return Manifest{}, fmt.Errorf("%s: unsupported schema %d", ManifestFile, manifest.Schema)
	}
	if manifest.Packages == nil {
		manifest.Packages = []metadata.Package{}
	}

	seen := make(map[string]bool, len(manifest.Packages))
	for i := range manifest.Packages {
		manifest.Packages[i].Repo = ""
		pkg := manifest.Packages[i]
		if err := metadata.Validate(pkg); err != nil {
			return Manifest{}, fmt.Errorf("%s: %w", ManifestFile, err)
		}
		if seen[pkg.ID] {
			return Manifest{}, fmt.Errorf("%s: duplicate package %s", ManifestFile, pkg.ID)
		}
		seen[pkg.ID] = true
	}
	return manifest, nil
}

func NewManifest(name string, packages []metadata.Package, generated time.Time) Manifest {
	sorted := make([]metadata.Package, len(packages))
	copy(sorted, packages)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })

	manifest := Manifest{
		Schema:   ManifestSchema,
		Name:     name,
		Packages: sorted,
	}
	if !generated.IsZero() {
		manifest.Generated = generated.UTC().Format(time.RFC3339)
	}
	return manifest
}

func (m Manifest) Encode() ([]byte, error) {
	if m.Packages == nil {
		m.Packages = []metadata.Package{}
	}
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(m); err != nil {
		return nil, fmt.Errorf("encode %s: %w", ManifestFile, err)
	}
	return buffer.Bytes(), nil
}

func (m Manifest) IDs() []string {
	ids := make([]string, len(m.Packages))
	for i, pkg := range m.Packages {
		ids[i] = pkg.ID
	}
	return ids
}
