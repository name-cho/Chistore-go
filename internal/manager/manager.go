package manager

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/name-cho/chistore-go/internal/config"
	"github.com/name-cho/chistore-go/internal/deps"
	"github.com/name-cho/chistore-go/internal/fsutil"
	"github.com/name-cho/chistore-go/internal/metadata"
	"github.com/name-cho/chistore-go/internal/repo"
	"github.com/name-cho/chistore-go/internal/repos"
	"github.com/name-cho/chistore-go/internal/state"
	"github.com/name-cho/chistore-go/internal/ui"
	"github.com/name-cho/chistore-go/internal/version"
)

var (
	ErrNotInstalled = errors.New("package is not installed")
	ErrCancelled    = errors.New("cancelled")
)

type Manager struct {
	Repos      *repos.Set
	Paths      config.Paths
	State      *state.Store
	UI         *ui.UI
	Deps       *deps.Installer
	DepsRunner deps.Runner
	Root       bool
	Now        func() time.Time
}

type Upgrade struct {
	Installed state.Package
	Remote    metadata.Package
}

func SplitRef(ref string) (string, string, error) {
	id, repoName, found := strings.Cut(ref, "@")
	if err := metadata.ValidateID(id); err != nil {
		return "", "", err
	}
	if found {
		if err := repos.ValidateName(repoName); err != nil {
			return "", "", err
		}
	}
	return id, repoName, nil
}

func (m *Manager) Info(ctx context.Context, ref string) (metadata.Package, error) {
	id, repoName, err := SplitRef(ref)
	if err != nil {
		return metadata.Package{}, err
	}
	return m.Repos.Info(ctx, id, repoName)
}

func (m *Manager) Search(ctx context.Context, query string) ([]metadata.Package, error) {
	matches, failures := m.Repos.Search(ctx, query)
	for _, failure := range failures {
		m.UI.Warnf("Repository unavailable: %v", failure)
	}
	if len(matches) == 0 && len(failures) > 0 && len(failures) == len(m.Repos.Repositories()) {
		return nil, fmt.Errorf("cannot read any repository: %v", failures[0])
	}
	return matches, nil
}

func (m *Manager) Installed(id string) (state.Package, bool) {
	return m.State.Get(id)
}

func (m *Manager) List() []state.Package {
	return m.State.List()
}

func (m *Manager) SearchInstalled(query string) []state.Package {
	query = strings.ToLower(query)
	var matches []state.Package
	for _, pkg := range m.State.List() {
		if strings.Contains(strings.ToLower(pkg.ID), query) || strings.Contains(strings.ToLower(pkg.Version), query) {
			matches = append(matches, pkg)
		}
	}
	return matches
}

func (m *Manager) Install(ctx context.Context, pkg metadata.Package) error {
	if existing, ok := m.Installed(pkg.ID); ok {
		m.UI.Infof("%s %s is already installed; reinstalling", pkg.ID, existing.Version)
	}
	if err := m.applyPackage(ctx, pkg); err != nil {
		return err
	}
	m.UI.Okf("%s installed", pkg.ID)
	m.warnMissingPath()
	return nil
}

func (m *Manager) Upgrade(ctx context.Context, upgrade Upgrade) error {
	m.UI.Infof("Updating %s %s -> %s", upgrade.Installed.ID, upgrade.Installed.Version, upgrade.Remote.Version)
	if err := m.applyPackage(ctx, upgrade.Remote); err != nil {
		return fmt.Errorf("update %s: %w", upgrade.Remote.ID, err)
	}
	m.UI.Okf("%s updated to %s", upgrade.Remote.ID, upgrade.Remote.Version)
	return nil
}

func (m *Manager) applyPackage(ctx context.Context, pkg metadata.Package) error {
	if err := m.Paths.EnsureDirs(); err != nil {
		return err
	}
	configDir := m.Paths.PackageConfigDir(pkg.ID)
	dataDir := m.Paths.PackageDataDir(pkg.ID)

	if err := m.ensureDependencies(ctx, pkg); err != nil {
		return err
	}
	for _, dir := range []string{configDir, dataDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}
	if err := m.installFiles(ctx, pkg, dataDir); err != nil {
		return fmt.Errorf("install %s: %w", pkg.ID, err)
	}

	return m.State.Put(state.Package{
		ID:          pkg.ID,
		Version:     pkg.Version,
		Binary:      m.binaryPath(pkg.ID),
		ConfigDir:   configDir,
		DataDir:     dataDir,
		InstalledAt: m.now().Format("2006-01-02"),
		Repo:        pkg.Repo,
	})
}

func (m *Manager) ensureDependencies(ctx context.Context, pkg metadata.Package) error {
	plan := m.dependencyPlan(ctx, pkg)
	if plan.Empty() {
		return nil
	}

	m.UI.Printf("\n%s\n", m.UI.Bold("Required dependencies:"))
	for _, line := range plan.Describe() {
		m.UI.Printf("  %s\n", line)
	}
	confirmed, err := m.UI.Confirm("Install dependencies?")
	if err != nil {
		return err
	}
	if !confirmed {
		return ErrCancelled
	}
	return m.Deps.Apply(ctx, plan)
}

func (m *Manager) dependencyPlan(ctx context.Context, pkg metadata.Package) deps.Plan {
	needsSystem := false
	for _, dependency := range pkg.Dependencies {
		if dependency.Manager == metadata.ManagerSystem {
			needsSystem = true
			break
		}
	}

	var tool deps.Tool
	var toolErr error
	if needsSystem {
		tool, toolErr = deps.Detect(m.DepsRunner)
	}
	installed := func(name string) bool {
		if toolErr == nil {
			return tool.SystemInstalled(ctx, m.DepsRunner, name)
		}

		_, err := m.DepsRunner.LookPath(name)
		return err == nil
	}
	venvReady := isFile(deps.PipPath(m.Paths.VenvDir))
	return deps.BuildPlan(pkg.Dependencies, installed, venvReady)
}

func (m *Manager) installFiles(ctx context.Context, pkg metadata.Package, dataDir string) error {
	switch pkg.Kind {
	case metadata.KindArchive:
		return m.installArchive(ctx, pkg, dataDir)
	case metadata.KindPython:
		return m.installPython(ctx, pkg, dataDir)
	default:
		return m.installSingle(ctx, pkg)
	}
}

func (m *Manager) installSingle(ctx context.Context, pkg metadata.Package) error {
	client, err := m.clientFor(pkg)
	if err != nil {
		return err
	}
	body, _, err := client.OpenMain(ctx, pkg.ID, pkg.MainFile)
	if err != nil {
		return err
	}
	defer body.Close()
	return fsutil.CopyToFileVerified(body, m.binaryPath(pkg.ID), 0o755, pkg.SHA256)
}

func (m *Manager) installPython(ctx context.Context, pkg metadata.Package, dataDir string) error {
	if err := m.Deps.EnsureVenv(ctx); err != nil {
		return err
	}
	client, err := m.clientFor(pkg)
	if err != nil {
		return err
	}
	body, used, err := client.OpenMain(ctx, pkg.ID, pkg.MainFile)
	if err != nil {
		return err
	}
	defer body.Close()

	target := filepath.Join(dataDir, filepath.FromSlash(used))
	if err := fsutil.CopyToFileVerified(body, target, 0o755, pkg.SHA256); err != nil {
		return err
	}
	return m.writeLauncher(m.binaryPath(pkg.ID), deps.PythonPath(m.Paths.VenvDir), target)
}

func (m *Manager) installArchive(ctx context.Context, pkg metadata.Package, dataDir string) error {
	client, err := m.clientFor(pkg)
	if err != nil {
		return err
	}
	body, err := client.Open(ctx, pkg.ID, pkg.ID+".tar.gz")
	if err != nil {
		return err
	}
	defer body.Close()

	tmpDir, err := os.MkdirTemp("", "chistore-archive-*")
	if err != nil {
		return fmt.Errorf("create temporary directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	archivePath := filepath.Join(tmpDir, pkg.ID+".tar.gz")
	if err := fsutil.CopyToFileVerified(body, archivePath, 0o600, pkg.SHA256); err != nil {
		return err
	}
	archive, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open downloaded archive: %w", err)
	}
	defer archive.Close()

	if err := extractTarGz(archive, dataDir); err != nil {
		return err
	}
	target, err := fsutil.ArchiveTarget(dataDir, pkg.MainFile)
	if err != nil {
		return err
	}
	info, err := os.Stat(target)
	if err != nil || info.IsDir() {
		return fmt.Errorf("main file %s is missing after extraction", pkg.MainFile)
	}
	_ = os.Chmod(target, info.Mode()|0o111)

	binPath := m.binaryPath(pkg.ID)
	_ = os.Remove(binPath)
	if runtime.GOOS != "windows" {
		if err := os.Symlink(target, binPath); err == nil {
			return nil
		}
	}
	return m.writeLauncher(binPath, target)
}

func (m *Manager) writeLauncher(path string, command ...string) error {
	quoted := make([]string, len(command))
	for i, part := range command {
		quoted[i] = shellQuote(part)
	}
	script := "#!/bin/sh\nexec " + strings.Join(quoted, " ") + ` "$@"` + "\n"
	return fsutil.WriteFileAtomic(path, []byte(script), 0o755)
}

func (m *Manager) Remove(ctx context.Context, id string, purge bool) error {
	entry, ok := m.State.Get(id)
	if !ok {
		return ErrNotInstalled
	}

	var configDir, dataDir string
	var err error
	if purge {
		if configDir, err = safePackageDir(entry.ConfigDir, m.Paths.ConfigHome, id); err != nil {
			return err
		}
		if dataDir, err = safePackageDir(entry.DataDir, m.Paths.DataHome, id); err != nil {
			return err
		}
	}

	if entry.Binary != "" {
		if err := m.removeBinary(entry.Binary); err != nil {
			return err
		}
	}
	if purge {
		if configDir != "" {
			if err := os.RemoveAll(configDir); err != nil {
				return fmt.Errorf("remove config: %w", err)
			}
			m.UI.Okf("Config purged: %s", configDir)
		}
		if dataDir != "" {
			if err := os.RemoveAll(dataDir); err != nil {
				return fmt.Errorf("remove data: %w", err)
			}
			m.UI.Okf("Data purged: %s", dataDir)
		}
	}
	if err := m.State.Delete(id); err != nil {
		return err
	}
	m.UI.Okf("%s removed", id)
	return nil
}

func (m *Manager) removeBinary(path string) error {
	clean := filepath.Clean(path)
	if !fsutil.IsWithin(m.Paths.BinDir, clean) {
		return fmt.Errorf("refusing to remove binary outside %s: %s", m.Paths.BinDir, path)
	}
	if err := os.Remove(clean); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove binary: %w", err)
	}
	m.UI.Okf("Binary removed: %s", clean)
	return nil
}

func (m *Manager) clientFor(pkg metadata.Package) (*repo.Client, error) {
	if pkg.Repo == "" {
		repositories := m.Repos.Repositories()
		if len(repositories) == 1 {
			if client, ok := m.Repos.Client(repositories[0].Name); ok {
				return client, nil
			}
		}
		return nil, fmt.Errorf("package %s has no source repository", pkg.ID)
	}
	client, ok := m.Repos.Client(pkg.Repo)
	if !ok {
		return nil, fmt.Errorf("repository %q is not configured", pkg.Repo)
	}
	return client, nil
}

func (m *Manager) Upgrades(ctx context.Context) ([]Upgrade, error) {
	var upgrades []Upgrade
	for _, installed := range m.State.List() {
		remote, err := m.remoteFor(ctx, installed)
		if err != nil {
			m.UI.Warnf("%v", err)
			continue
		}
		if remote.Version == installed.Version {
			continue
		}
		if version.Compare(remote.Version, installed.Version) <= 0 {
			m.UI.Warnf("%s: repository has %s, installed %s; not newer", installed.ID, remote.Version, installed.Version)
			continue
		}
		upgrades = append(upgrades, Upgrade{Installed: installed, Remote: remote})
	}
	return upgrades, nil
}

func (m *Manager) remoteFor(ctx context.Context, installed state.Package) (metadata.Package, error) {
	if installed.Repo != "" {
		// Stay on the repository the package was installed from; mirrors may lag behind.
		if _, ok := m.Repos.Client(installed.Repo); !ok {
			return metadata.Package{}, fmt.Errorf("%s: repository %q is no longer configured; skipping", installed.ID, installed.Repo)
		}
		remote, err := m.Repos.Info(ctx, installed.ID, installed.Repo)
		if err != nil {
			return metadata.Package{}, fmt.Errorf("%s: %w; skipping", installed.ID, err)
		}
		return remote, nil
	}
	remote, err := m.Repos.Info(ctx, installed.ID, "")
	if err != nil {
		return metadata.Package{}, fmt.Errorf("%s: %w; skipping", installed.ID, err)
	}
	return remote, nil
}

func (m *Manager) binaryPath(id string) string {
	return filepath.Join(m.Paths.BinDir, id)
}

func (m *Manager) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

func (m *Manager) warnMissingPath() {
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if filepath.Clean(dir) == filepath.Clean(m.Paths.BinDir) {
			return
		}
	}
	m.UI.Warnf("%s is not in PATH; add it to run installed tools", m.Paths.BinDir)
}

func safePackageDir(recorded, base, id string) (string, error) {
	if recorded == "" {
		return "", nil
	}
	clean := filepath.Clean(recorded)
	if !fsutil.IsWithin(base, clean) || filepath.Base(clean) != id {
		return "", fmt.Errorf("refusing to purge unexpected path %s", recorded)
	}
	return clean, nil
}

func extractTarGz(reader io.Reader, dest string) error {
	gzipReader, err := gzip.NewReader(reader)
	if err != nil {
		return fmt.Errorf("read archive: %w", err)
	}
	defer gzipReader.Close()

	archive := tar.NewReader(gzipReader)
	for {
		header, err := archive.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read archive entry: %w", err)
		}
		target, err := fsutil.ArchiveTarget(dest, header.Name)
		if err != nil {
			return err
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("create archive directory: %w", err)
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("create archive directory: %w", err)
			}
			mode := os.FileMode(header.Mode) & 0o777
			if mode == 0 {
				mode = 0o644
			}
			file, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
			if err != nil {
				return fmt.Errorf("create archive file: %w", err)
			}
			if _, err := io.Copy(file, archive); err != nil {
				file.Close()
				return fmt.Errorf("extract %s: %w", header.Name, err)
			}
			if err := file.Close(); err != nil {
				return fmt.Errorf("close %s: %w", header.Name, err)
			}
			if err := os.Chmod(target, mode); err != nil {
				return fmt.Errorf("chmod %s: %w", header.Name, err)
			}
		case tar.TypeSymlink, tar.TypeLink:
			return fmt.Errorf("archive links are not supported: %s", header.Name)
		}
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
