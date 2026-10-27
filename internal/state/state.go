package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/name-cho/chistore-go/internal/fsutil"
)

const schemaVersion = 1

type Package struct {
	ID          string `json:"id"`
	Version     string `json:"version"`
	Binary      string `json:"binary"`
	ConfigDir   string `json:"config_dir"`
	DataDir     string `json:"data_dir"`
	InstalledAt string `json:"installed_at"`
	Repo        string `json:"repo,omitempty"`
}

type diskState struct {
	Schema   int                `json:"schema"`
	Packages map[string]Package `json:"packages"`
}

type Store struct {
	path  string
	state diskState
}

func Open(dir string) (*Store, error) {
	store := &Store{
		path: filepath.Join(dir, "state.json"),
		state: diskState{
			Schema:   schemaVersion,
			Packages: map[string]Package{},
		},
	}

	data, err := os.ReadFile(store.path)
	switch {
	case err == nil:
		if err := json.Unmarshal(data, &store.state); err != nil {
			return nil, fmt.Errorf("read %s: %w", store.path, err)
		}
		if store.state.Packages == nil {
			store.state.Packages = map[string]Package{}
		}
		if store.state.Schema == 0 {
			store.state.Schema = schemaVersion
		}
		return store, nil
	case !os.IsNotExist(err):
		return nil, fmt.Errorf("read %s: %w", store.path, err)
	}

	if err := store.migrateLegacyManifest(filepath.Join(dir, "manifest")); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Store) migrateLegacyManifest(path string) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read legacy manifest: %w", err)
	}
	s.state.Packages = parseManifest(string(data))
	if len(s.state.Packages) > 0 {
		if err := s.save(); err != nil {
			return fmt.Errorf("write migrated state: %w", err)
		}
	}
	if err := os.Rename(path, path+".migrated"); err != nil {
		return fmt.Errorf("rename legacy manifest: %w", err)
	}
	return nil
}

func parseManifest(data string) map[string]Package {
	packages := map[string]Package{}
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, "|", 6)
		if len(fields) != 6 || fields[0] == "" {
			continue
		}
		packages[fields[0]] = Package{
			ID:          fields[0],
			Version:     fields[1],
			Binary:      fields[2],
			ConfigDir:   fields[3],
			DataDir:     fields[4],
			InstalledAt: fields[5],
		}
	}
	return packages
}

func (s *Store) List() []Package {
	list := make([]Package, 0, len(s.state.Packages))
	for _, pkg := range s.state.Packages {
		list = append(list, pkg)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	return list
}

func (s *Store) Get(id string) (Package, bool) {
	pkg, ok := s.state.Packages[id]
	return pkg, ok
}

func (s *Store) Put(pkg Package) error {
	if pkg.ID == "" {
		return fmt.Errorf("cannot store package with empty id")
	}
	s.state.Packages[pkg.ID] = pkg
	return s.save()
}

func (s *Store) Delete(id string) error {
	if _, ok := s.state.Packages[id]; !ok {
		return nil
	}
	delete(s.state.Packages, id)
	return s.save()
}

func (s *Store) save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}
	s.state.Schema = schemaVersion
	data, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	data = append(data, '\n')
	if err := fsutil.WriteFileAtomic(s.path, data, 0o600); err != nil {
		return fmt.Errorf("replace state: %w", err)
	}
	return nil
}
