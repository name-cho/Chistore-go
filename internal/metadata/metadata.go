package metadata

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"path"
	"regexp"
	"strings"
)

type Kind string

const (
	KindSingle  Kind = "single"
	KindArchive Kind = "archive"
	KindPython  Kind = "python"
)

func (k Kind) Valid() bool {
	switch k {
	case KindSingle, KindArchive, KindPython:
		return true
	default:
		return false
	}
}

type Manager string

const (
	ManagerSystem Manager = "sys"
	ManagerPip    Manager = "pip"
	ManagerNpm    Manager = "npm"
)

type Dependency struct {
	Manager  Manager  `json:"manager"`
	Packages []string `json:"packages"`
}

type Package struct {
	ID           string       `json:"id"`
	Name         string       `json:"name"`
	Description  string       `json:"description"`
	Version      string       `json:"version"`
	Size         string       `json:"size"`
	Kind         Kind         `json:"type"`
	MainFile     string       `json:"main"`
	Author       string       `json:"author,omitempty"`
	Homepage     string       `json:"homepage,omitempty"`
	Source       string       `json:"source,omitempty"`
	Dependencies []Dependency `json:"dependencies,omitempty"`
	SHA256       string       `json:"sha256,omitempty"`
	Repo         string       `json:"-"`
}

var idPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*$`)

func ValidateID(id string) error {
	if id == "" {
		return fmt.Errorf("package id is empty")
	}
	if !idPattern.MatchString(id) {
		return fmt.Errorf("invalid package id %q", id)
	}
	return nil
}

func ValidateMainFile(name string) error {
	if name == "" {
		return fmt.Errorf("main file is empty")
	}
	if strings.ContainsAny(name, `\:`+"\x00") {
		return fmt.Errorf("invalid main file %q", name)
	}
	if path.IsAbs(name) || path.Clean(name) != name {
		return fmt.Errorf("invalid main file %q", name)
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." || part == ".." {
			return fmt.Errorf("invalid main file %q", name)
		}
	}
	return nil
}

func Validate(pkg Package) error {
	if err := ValidateID(pkg.ID); err != nil {
		return err
	}
	if strings.TrimSpace(pkg.Version) == "" {
		return fmt.Errorf("package %s: version is empty", pkg.ID)
	}
	if !pkg.Kind.Valid() {
		return fmt.Errorf("package %s: unknown type %q", pkg.ID, pkg.Kind)
	}
	if err := ValidateMainFile(pkg.MainFile); err != nil {
		return fmt.Errorf("package %s: %w", pkg.ID, err)
	}
	for _, dependency := range pkg.Dependencies {
		switch dependency.Manager {
		case ManagerSystem, ManagerPip, ManagerNpm:
		default:
			return fmt.Errorf("package %s: unknown dependency manager %q", pkg.ID, dependency.Manager)
		}
		for _, name := range dependency.Packages {
			if strings.TrimSpace(name) == "" {
				return fmt.Errorf("package %s: empty %s dependency", pkg.ID, dependency.Manager)
			}
		}
	}
	if pkg.SHA256 != "" {
		sum, err := hex.DecodeString(pkg.SHA256)
		if err != nil || len(sum) != sha256.Size {
			return fmt.Errorf("package %s: invalid sha256 %q", pkg.ID, pkg.SHA256)
		}
	}
	return nil
}

func Parse(id string, r io.Reader) (Package, error) {
	if err := ValidateID(id); err != nil {
		return Package{}, err
	}

	pkg := Package{
		ID:          id,
		Name:        id,
		Description: "No description",
		Version:     "?",
		Size:        "?",
		Kind:        KindSingle,
		MainFile:    id,
	}

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 4096), 1024*1024)
	for scanner.Scan() {
		key, value, ok := parseFieldLine(scanner.Text())
		if !ok {
			continue
		}
		if err := pkg.setField(key, value); err != nil {
			return Package{}, err
		}
	}
	if err := scanner.Err(); err != nil {
		return Package{}, fmt.Errorf("read metadata for %s: %w", id, err)
	}
	if err := ValidateMainFile(pkg.MainFile); err != nil {
		return Package{}, err
	}
	return pkg, nil
}

func parseFieldLine(line string) (key, value string, ok bool) {
	line = strings.TrimSpace(strings.TrimPrefix(line, "\ufeff"))
	line = strings.TrimSpace(strings.TrimLeft(line, "#"))
	before, after, found := strings.Cut(line, ":")
	if !found {
		return "", "", false
	}
	key = strings.TrimSpace(before)
	if key == "" {
		return "", "", false
	}
	return key, strings.TrimSpace(after), true
}

func (p *Package) setField(key, value string) error {
	switch strings.ToLower(key) {
	case "name":
		if value != "" {
			p.Name = value
		}
	case "description":
		if value != "" {
			p.Description = value
		}
	case "version":
		if value != "" {
			p.Version = value
		}
	case "size":
		if value != "" {
			p.Size = value
		}
	case "type":
		kind := Kind(strings.ToLower(value))
		if !kind.Valid() {
			return fmt.Errorf("package %s: unknown type %q", p.ID, value)
		}
		p.Kind = kind
	case "mainfile":
		if value != "" {
			p.MainFile = value
		}
	case "author":
		p.Author = value
	case "homepage":
		p.Homepage = value
	case "source":
		p.Source = value
	case "dependencies":
		deps, err := ParseDependencies(value)
		if err != nil {
			return fmt.Errorf("package %s: %w", p.ID, err)
		}
		p.Dependencies = deps
	}
	return nil
}

func ParseDependencies(value string) ([]Dependency, error) {
	packages := map[Manager][]string{}

	for _, token := range strings.Fields(value) {
		rawManager, rawPackages, found := strings.Cut(token, ":")
		if !found {
			rawManager, rawPackages = string(ManagerSystem), token
		}
		manager, err := canonicalManager(rawManager)
		if err != nil {
			return nil, err
		}
		for _, name := range strings.Split(rawPackages, ",") {
			name = strings.TrimSpace(name)
			if name == "" || contains(packages[manager], name) {
				continue
			}
			packages[manager] = append(packages[manager], name)
		}
	}

	deps := make([]Dependency, 0, len(packages))
	for _, manager := range []Manager{ManagerSystem, ManagerPip, ManagerNpm} {
		if len(packages[manager]) > 0 {
			deps = append(deps, Dependency{Manager: manager, Packages: packages[manager]})
		}
	}
	return deps, nil
}

func canonicalManager(name string) (Manager, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "sys", "system", "pacman", "apt", "dnf", "apk", "brew":
		return ManagerSystem, nil
	case "pip", "python", "python3":
		return ManagerPip, nil
	case "npm", "node":
		return ManagerNpm, nil
	default:
		return "", fmt.Errorf("unknown dependency manager %q", name)
	}
}

func FormatDependencies(deps []Dependency) string {
	parts := make([]string, 0, len(deps))
	for _, dep := range deps {
		parts = append(parts, string(dep.Manager)+":"+strings.Join(dep.Packages, ","))
	}
	return strings.Join(parts, " ")
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
