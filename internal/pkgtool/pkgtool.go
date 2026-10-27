package pkgtool

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/name-cho/chistore-go/internal/fsutil"
	"github.com/name-cho/chistore-go/internal/index"
	"github.com/name-cho/chistore-go/internal/metadata"
)

type Tool struct {
	Root string
}

func New(root string) *Tool {
	return &Tool{Root: root}
}

type Report struct {
	Errors   []string
	Warnings []string
}

func (r Report) OK() bool {
	return len(r.Errors) == 0
}

func (t *Tool) Init(id string) error {
	if err := metadata.ValidateID(id); err != nil {
		return err
	}
	info, err := os.Stat(t.Root)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("repository root %s is not a directory", t.Root)
	}

	dir := filepath.Join(t.Root, id)
	infoPath := filepath.Join(dir, "info.md")
	mainPath := filepath.Join(dir, id)
	for _, path := range []string{infoPath, mainPath} {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("%s already exists", path)
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create package directory: %w", err)
	}

	if err := fsutil.WriteFileAtomic(infoPath, []byte(infoTemplate(id)), 0o644); err != nil {
		return err
	}
	if err := fsutil.WriteFileAtomic(mainPath, []byte(scriptTemplate(id)), 0o755); err != nil {
		return err
	}
	if _, err := t.Index(); err != nil {
		return fmt.Errorf("created %s, but rebuilding the index failed: %w", id, err)
	}
	return nil
}

func (t *Tool) Index() ([]string, error) {
	packages, problems := t.packages()
	for _, pkg := range packages {
		if _, err := t.payloadPath(pkg); err != nil {
			problems = append(problems, err.Error())
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return nil, fmt.Errorf("%s", strings.Join(problems, "; "))
	}

	manifest := index.NewManifest(t.name(), packages, time.Now())
	data, err := manifest.Encode()
	if err != nil {
		return nil, err
	}
	if err := fsutil.WriteFileAtomic(filepath.Join(t.Root, index.ManifestFile), data, 0o644); err != nil {
		return nil, err
	}

	ids := manifest.IDs()
	// Old repositories still have INDEX. Keep it in sync there, but do not
	// create it in new repositories.
	legacy := filepath.Join(t.Root, "INDEX")
	if _, err := os.Stat(legacy); err == nil {
		if err := index.Write(legacy, ids); err != nil {
			return nil, err
		}
	}
	return ids, nil
}

func (t *Tool) Validate() (Report, error) {
	var report Report
	packages, problems := t.packages()
	report.Errors = append(report.Errors, problems...)

	byID := make(map[string]metadata.Package, len(packages))
	for _, pkg := range packages {
		byID[pkg.ID] = pkg
	}

	hasManifest := t.validateManifest(byID, &report)
	t.validateLegacyIndex(byID, hasManifest, &report)

	for _, pkg := range packages {
		t.validatePackage(pkg, &report)
	}
	for _, name := range t.dirsWithoutInfo() {
		report.Warnings = append(report.Warnings, fmt.Sprintf("package directory %s has no info.md", name))
	}
	report.sort()
	return report, nil
}

func (t *Tool) validateManifest(byID map[string]metadata.Package, report *Report) bool {
	// index.json is authoritative; INDEX is only the compatibility mirror.
	data, err := os.ReadFile(filepath.Join(t.Root, index.ManifestFile))
	if os.IsNotExist(err) {
		report.Warnings = append(report.Warnings, "index.json is missing; run chistore pkg index")
		return false
	}
	if err != nil {
		report.Errors = append(report.Errors, err.Error())
		return false
	}

	manifest, err := index.ParseManifest(data)
	if err != nil {
		report.Errors = append(report.Errors, err.Error())
		return true
	}

	listed := make(map[string]bool, len(manifest.Packages))
	for _, entry := range manifest.Packages {
		listed[entry.ID] = true
		want, ok := byID[entry.ID]
		if !ok {
			report.Errors = append(report.Errors, fmt.Sprintf("index.json lists %s, but %s/info.md is missing", entry.ID, entry.ID))
			continue
		}
		report.Errors = append(report.Errors, compareManifest(entry, want)...)
	}
	for id := range byID {
		if !listed[id] {
			report.Errors = append(report.Errors, fmt.Sprintf("%s has info.md but is missing from index.json", id))
		}
	}
	return true
}

func (t *Tool) validateLegacyIndex(byID map[string]metadata.Package, hasManifest bool, report *Report) {
	ids, err := index.Read(filepath.Join(t.Root, "INDEX"))
	if os.IsNotExist(err) {
		if !hasManifest {
			report.Errors = append(report.Errors, "repository has neither index.json nor INDEX")
		}
		return
	}
	if err != nil {
		report.Errors = append(report.Errors, err.Error())
		return
	}

	listed := make(map[string]bool, len(ids))
	for _, id := range ids {
		listed[id] = true
		if _, ok := byID[id]; !ok {
			report.Errors = append(report.Errors, fmt.Sprintf("INDEX lists %s, but %s/info.md is missing", id, id))
		}
	}
	for id := range byID {
		if !listed[id] {
			report.Errors = append(report.Errors, fmt.Sprintf("%s has info.md but is not listed in INDEX", id))
		}
	}
}

func (t *Tool) packages() ([]metadata.Package, []string) {
	entries, err := os.ReadDir(t.Root)
	if err != nil {
		return nil, []string{fmt.Sprintf("read repository: %v", err)}
	}

	var packages []metadata.Package
	var problems []string
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() || strings.HasPrefix(name, ".") {
			continue
		}
		infoPath := filepath.Join(t.Root, name, "info.md")
		data, err := os.ReadFile(infoPath)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		if err := metadata.ValidateID(name); err != nil {
			continue
		}
		pkg, err := metadata.Parse(name, strings.NewReader(string(data)))
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		if payload, err := t.payloadPath(pkg); err == nil {
			if sum, err := fileSHA256(payload); err == nil {
				pkg.SHA256 = sum
			}
		}
		packages = append(packages, pkg)
	}
	sort.Slice(packages, func(i, j int) bool { return packages[i].ID < packages[j].ID })
	return packages, problems
}

func (t *Tool) payloadPath(pkg metadata.Package) (string, error) {
	dir := filepath.Join(t.Root, pkg.ID)
	var target string
	if pkg.Kind == metadata.KindArchive {
		target = filepath.Join(dir, pkg.ID+".tar.gz")
	} else {
		var err error
		target, err = fsutil.ArchiveTarget(dir, pkg.MainFile)
		if err != nil {
			return "", fmt.Errorf("%s: %w", pkg.ID, err)
		}
	}
	info, err := os.Stat(target)
	if err != nil || info.IsDir() {
		return "", fmt.Errorf("%s: payload %s is missing", pkg.ID, filepath.Base(target))
	}
	return target, nil
}

func (t *Tool) name() string {
	abs, err := filepath.Abs(t.Root)
	if err != nil {
		return filepath.Base(t.Root)
	}
	return filepath.Base(abs)
}

func (t *Tool) dirsWithoutInfo() []string {
	entries, err := os.ReadDir(t.Root)
	if err != nil {
		return nil
	}
	var names []string
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() || strings.HasPrefix(name, ".") {
			continue
		}
		if _, err := os.Stat(filepath.Join(t.Root, name, "info.md")); os.IsNotExist(err) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func (t *Tool) validatePackage(pkg metadata.Package, report *Report) {
	dir := filepath.Join(t.Root, pkg.ID)
	if pkg.Kind == metadata.KindArchive {
		archive := filepath.Join(dir, pkg.ID+".tar.gz")
		if _, err := os.Stat(archive); err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("%s: archive %s.tar.gz is missing", pkg.ID, pkg.ID))
		}
	} else {
		mainPath, err := fsutil.ArchiveTarget(dir, pkg.MainFile)
		if err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("%s: %v", pkg.ID, err))
			return
		}
		info, err := os.Stat(mainPath)
		if err != nil || info.IsDir() {
			report.Errors = append(report.Errors, fmt.Sprintf("%s: main file %s is missing", pkg.ID, pkg.MainFile))
		} else if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
			report.Warnings = append(report.Warnings, fmt.Sprintf("%s: main file %s is not executable", pkg.ID, pkg.MainFile))
		}
	}
	if pkg.Version == "?" {
		report.Warnings = append(report.Warnings, fmt.Sprintf("%s: version is not set", pkg.ID))
	}
	if pkg.Size == "?" {
		report.Warnings = append(report.Warnings, fmt.Sprintf("%s: size is not set", pkg.ID))
	}
}

func compareManifest(entry, want metadata.Package) []string {
	var problems []string
	prefix := "index.json: " + entry.ID
	if entry.Version != want.Version {
		problems = append(problems, fmt.Sprintf("%s version is %q, info.md says %q", prefix, entry.Version, want.Version))
	}
	if entry.Kind != want.Kind {
		problems = append(problems, fmt.Sprintf("%s type is %q, info.md says %q", prefix, entry.Kind, want.Kind))
	}
	if entry.MainFile != want.MainFile {
		problems = append(problems, fmt.Sprintf("%s main is %q, info.md says %q", prefix, entry.MainFile, want.MainFile))
	}
	if entry.SHA256 != "" && want.SHA256 != "" && !strings.EqualFold(entry.SHA256, want.SHA256) {
		problems = append(problems, fmt.Sprintf("%s sha256 is %s, payload is %s", prefix, entry.SHA256, want.SHA256))
	}
	return problems
}

func (r *Report) sort() {
	sort.Strings(r.Errors)
	sort.Strings(r.Warnings)
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func infoTemplate(id string) string {
	return fmt.Sprintf(`## Name: %s
## Description: 
## Version: 0.1.0
## Size: ?
## Type: single
## MainFile: %s
## Author: 
## Homepage: 
## Dependencies: 
`, id, id)
}

func scriptTemplate(id string) string {
	return fmt.Sprintf(`#!/usr/bin/env bash
set -euo pipefail

echo "Hello from %s"
`, id)
}
