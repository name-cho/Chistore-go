package manager

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/name-cho/chistore-go/internal/config"
	"github.com/name-cho/chistore-go/internal/deps"
	"github.com/name-cho/chistore-go/internal/repos"
	"github.com/name-cho/chistore-go/internal/state"
	"github.com/name-cho/chistore-go/internal/ui"
)

type fakeRunner struct {
	commands [][]string
	paths    map[string]bool
	outputs  map[string]string
}

func (f *fakeRunner) LookPath(name string) (string, error) {
	if f.paths[name] {
		return "/usr/bin/" + name, nil
	}
	return "", os.ErrNotExist
}

func (f *fakeRunner) Run(ctx context.Context, name string, args ...string) error {
	f.commands = append(f.commands, append([]string{name}, args...))
	return nil
}

func (f *fakeRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	f.commands = append(f.commands, append([]string{name}, args...))
	return []byte(f.outputs[name]), nil
}

type testEnv struct {
	manager *Manager
	state   *state.Store
	paths   config.Paths
	runner  *fakeRunner
	out     *bytes.Buffer
}

func newTestEnv(t *testing.T, files map[string]string, input string, yes bool) *testEnv {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)

	client, err := repos.NewSingle("test", server.URL, server.Client())
	if err != nil {
		t.Fatalf("repos.NewSingle() error = %v", err)
	}

	root := t.TempDir()
	paths := config.Paths{
		Home:       root,
		DataHome:   filepath.Join(root, "data"),
		ConfigHome: filepath.Join(root, "config"),
		BinDir:     filepath.Join(root, "bin"),
		StateDir:   filepath.Join(root, "data", "chistore"),
		VenvDir:    filepath.Join(root, "data", "chistore", "venv"),
	}
	if err := paths.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs() error = %v", err)
	}
	store, err := state.Open(paths.StateDir)
	if err != nil {
		t.Fatalf("state.Open() error = %v", err)
	}

	out := &bytes.Buffer{}
	terminal := ui.New(strings.NewReader(input), out, out, false, yes)
	runner := &fakeRunner{paths: map[string]bool{}, outputs: map[string]string{}}
	installer := &deps.Installer{
		Runner:  runner,
		UI:      terminal,
		VenvDir: paths.VenvDir,
		Python:  "python3",
		Root:    true,
	}

	return &testEnv{
		manager: &Manager{
			Repos:      client,
			Paths:      paths,
			State:      store,
			UI:         terminal,
			Deps:       installer,
			DepsRunner: runner,
			Root:       true,
			Now:        func() time.Time { return time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC) },
		},
		state:  store,
		paths:  paths,
		runner: runner,
		out:    out,
	}
}

func info(kind, version, mainFile, dependencies string) string {
	body := "## Name: Demo\n## Version: " + version + "\n## Type: " + kind + "\n## MainFile: " + mainFile + "\n"
	if dependencies != "" {
		body += "## Dependencies: " + dependencies + "\n"
	}
	return body
}

func TestInstallSingleAndRemove(t *testing.T) {
	env := newTestEnv(t, map[string]string{
		"/INDEX":        "demo\n",
		"/demo/info.md": info("single", "1.0.0", "demo", ""),
		"/demo/demo":    "#!/bin/sh\necho demo\n",
	}, "", true)

	ctx := context.Background()
	pkg, err := env.manager.Info(ctx, "demo")
	if err != nil {
		t.Fatalf("Info() error = %v", err)
	}
	if err := env.manager.Install(ctx, pkg); err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	bin := filepath.Join(env.paths.BinDir, "demo")
	data, err := os.ReadFile(bin)
	if err != nil || !strings.Contains(string(data), "echo demo") {
		t.Fatalf("binary = %q, %v", data, err)
	}
	installed, ok := env.manager.Installed("demo")
	if !ok || installed.Version != "1.0.0" || installed.InstalledAt != "2026-10-27" {
		t.Fatalf("installed = %+v, %v", installed, ok)
	}
	for _, dir := range []string{installed.ConfigDir, installed.DataDir} {
		if _, err := os.Stat(dir); err != nil {
			t.Errorf("package dir %q not created: %v", dir, err)
		}
	}

	if err := env.manager.Remove(ctx, "demo", false); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if _, err := os.Stat(bin); !os.IsNotExist(err) {
		t.Errorf("binary still exists: %v", err)
	}
	if _, ok := env.manager.Installed("demo"); ok {
		t.Error("package still in state")
	}
	for _, dir := range []string{installed.ConfigDir, installed.DataDir} {
		if _, err := os.Stat(dir); err != nil {
			t.Errorf("Remove(keep config) removed %q: %v", dir, err)
		}
	}
}

func TestInstallAcceptsDependenciesAlreadyOnPath(t *testing.T) {
	env := newTestEnv(t, map[string]string{
		"/INDEX":        "demo\n",
		"/demo/info.md": info("single", "1.0.0", "demo", "sys:curl,bash"),
		"/demo/demo":    "#!/bin/sh\n",
	}, "", true)
	env.runner.paths["curl"] = true
	env.runner.paths["bash"] = true

	ctx := context.Background()
	pkg, _ := env.manager.Info(ctx, "demo")
	if err := env.manager.Install(ctx, pkg); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if len(env.runner.commands) != 0 {
		t.Errorf("commands = %v, want none", env.runner.commands)
	}
	if _, ok := env.manager.Installed("demo"); !ok {
		t.Error("package not installed")
	}
}

func TestInstallFailsWhenDependencyCannotBeInstalled(t *testing.T) {
	env := newTestEnv(t, map[string]string{
		"/INDEX":        "demo\n",
		"/demo/info.md": info("single", "1.0.0", "demo", "sys:missing-tool"),
		"/demo/demo":    "#!/bin/sh\n",
	}, "", true)

	ctx := context.Background()
	pkg, _ := env.manager.Info(ctx, "demo")
	err := env.manager.Install(ctx, pkg)
	if err == nil {
		t.Fatal("Install() error = nil, want missing package manager error")
	}
	if _, ok := env.manager.Installed("demo"); ok {
		t.Error("package installed without its dependency")
	}
	if _, statErr := os.Stat(filepath.Join(env.paths.BinDir, "demo")); !os.IsNotExist(statErr) {
		t.Errorf("binary exists after failed install: %v", statErr)
	}
}

func TestRemovePurgesPackageDirs(t *testing.T) {
	env := newTestEnv(t, map[string]string{
		"/INDEX":        "demo\n",
		"/demo/info.md": info("single", "1.0.0", "demo", ""),
		"/demo/demo":    "#!/bin/sh\n",
	}, "", true)

	ctx := context.Background()
	pkg, _ := env.manager.Info(ctx, "demo")
	if err := env.manager.Install(ctx, pkg); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	installed, _ := env.manager.Installed("demo")
	for _, dir := range []string{installed.ConfigDir, installed.DataDir} {
		if err := os.WriteFile(filepath.Join(dir, "keep.txt"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if err := env.manager.Remove(ctx, "demo", true); err != nil {
		t.Fatalf("Remove(purge) error = %v", err)
	}
	for _, dir := range []string{installed.ConfigDir, installed.DataDir} {
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Errorf("directory %q still exists: %v", dir, err)
		}
	}
}

func TestRemoveMissing(t *testing.T) {
	env := newTestEnv(t, map[string]string{}, "", true)
	err := env.manager.Remove(context.Background(), "missing", false)
	if !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("Remove() error = %v, want ErrNotInstalled", err)
	}
}

func TestRemoveRefusesPurgeOutsideExpectedDirs(t *testing.T) {
	env := newTestEnv(t, map[string]string{}, "", true)
	outside := filepath.Join(t.TempDir(), "precious")
	if err := os.WriteFile(outside, []byte("do not delete"), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(env.paths.BinDir, "demo")
	if err := os.WriteFile(bin, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := env.state.Put(state.Package{
		ID:        "demo",
		Version:   "1.0.0",
		Binary:    bin,
		ConfigDir: filepath.Dir(outside),
		DataDir:   env.paths.PackageDataDir("demo"),
	}); err != nil {
		t.Fatal(err)
	}

	if err := env.manager.Remove(context.Background(), "demo", true); err == nil {
		t.Fatal("Remove() error = nil, want path guard")
	}
	if _, err := os.Stat(outside); err != nil {
		t.Errorf("outside file was touched: %v", err)
	}
}

func TestInstallArchive(t *testing.T) {
	archive := makeTarGz(t, map[string]string{"bin/demo": "#!/bin/sh\necho archive\n"})
	env := newTestEnv(t, map[string]string{
		"/INDEX":          "arc\n",
		"/arc/info.md":    info("archive", "1.0.0", "bin/demo", ""),
		"/arc/arc.tar.gz": string(archive),
	}, "", true)

	ctx := context.Background()
	pkg, err := env.manager.Info(ctx, "arc")
	if err != nil {
		t.Fatalf("Info() error = %v", err)
	}
	if err := env.manager.Install(ctx, pkg); err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	installed, _ := env.manager.Installed("arc")
	target := filepath.Join(installed.DataDir, "bin", "demo")
	data, err := os.ReadFile(target)
	if err != nil || !strings.Contains(string(data), "echo archive") {
		t.Fatalf("extracted file = %q, %v", data, err)
	}
	if _, err := os.Stat(installed.Binary); err != nil {
		t.Errorf("binary link missing: %v", err)
	}
}

func TestInstallPython(t *testing.T) {
	env := newTestEnv(t, map[string]string{
		"/INDEX":          "pystat\n",
		"/pystat/info.md": info("python", "1.0.0", "pystat", ""),
		"/pystat/pystat":  "print('hi')\n",
	}, "", true)

	pip := deps.PipPath(env.paths.VenvDir)
	if err := os.MkdirAll(filepath.Dir(pip), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pip, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	pkg, _ := env.manager.Info(ctx, "pystat")
	if err := env.manager.Install(ctx, pkg); err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	installed, _ := env.manager.Installed("pystat")
	script, err := os.ReadFile(filepath.Join(installed.DataDir, "pystat"))
	if err != nil || !strings.Contains(string(script), "print") {
		t.Fatalf("python file = %q, %v", script, err)
	}
	wrapper, err := os.ReadFile(installed.Binary)
	if err != nil || !strings.Contains(string(wrapper), deps.PythonPath(env.paths.VenvDir)) {
		t.Fatalf("wrapper = %q, %v", wrapper, err)
	}
	if len(env.runner.commands) != 0 {
		t.Errorf("venv commands = %v, want none when pip exists", env.runner.commands)
	}
}

func TestInstallDeclinedDependencies(t *testing.T) {
	env := newTestEnv(t, map[string]string{
		"/INDEX":        "demo\n",
		"/demo/info.md": info("single", "1.0.0", "demo", "sys:zzz"),
		"/demo/demo":    "#!/bin/sh\n",
	}, "n\n", false)

	ctx := context.Background()
	pkg, _ := env.manager.Info(ctx, "demo")
	err := env.manager.Install(ctx, pkg)
	if !errors.Is(err, ErrCancelled) {
		t.Fatalf("Install() error = %v, want ErrCancelled", err)
	}
	if _, ok := env.manager.Installed("demo"); ok {
		t.Error("package installed after declined dependencies")
	}
	if _, err := os.Stat(filepath.Join(env.paths.BinDir, "demo")); !os.IsNotExist(err) {
		t.Errorf("binary exists after cancel: %v", err)
	}
}

func TestSearchKeepsRepositoryOrder(t *testing.T) {
	env := newTestEnv(t, map[string]string{
		"/INDEX":           "alpha\nbeta\namazing\n",
		"/alpha/info.md":   "## Name: Alpha\n",
		"/beta/info.md":    "## Name: Beta\n",
		"/amazing/info.md": "## Name: Amazing\n",
	}, "", true)

	all, err := env.manager.Search(context.Background(), "")
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(all) != 3 || all[0].ID != "alpha" || all[2].ID != "amazing" {
		t.Fatalf("Search(all) = %+v", all)
	}

	filtered, err := env.manager.Search(context.Background(), "AM")
	if err != nil {
		t.Fatalf("Search(AM) error = %v", err)
	}
	if len(filtered) != 1 || filtered[0].ID != "amazing" {
		t.Fatalf("Search(AM) = %+v", filtered)
	}
}

func TestUpgradesOnlyNewerVersions(t *testing.T) {
	env := newTestEnv(t, map[string]string{
		"/INDEX":         "alpha\nbeta\ndelta\n",
		"/alpha/info.md": "## Version: 1.1.0\n",
		"/beta/info.md":  "## Version: 2.0.0\n",
		"/delta/info.md": "## Version: 1.0.0\n",
	}, "", true)

	for _, entry := range []state.Package{
		{ID: "alpha", Version: "1.0.0"},
		{ID: "beta", Version: "2.0.0"},
		{ID: "delta", Version: "2.0.0"},
	} {
		if err := env.state.Put(entry); err != nil {
			t.Fatal(err)
		}
	}

	upgrades, err := env.manager.Upgrades(context.Background())
	if err != nil {
		t.Fatalf("Upgrades() error = %v", err)
	}
	if len(upgrades) != 1 {
		t.Fatalf("Upgrades() = %+v, want alpha only", upgrades)
	}
	if upgrades[0].Installed.ID != "alpha" || upgrades[0].Remote.Version != "1.1.0" {
		t.Errorf("upgrade = %+v", upgrades[0])
	}
}

func TestInstallRejectsChecksumMismatch(t *testing.T) {
	env := newTestEnv(t, map[string]string{
		"/index.json": `{"schema":1,"packages":[{"id":"demo","version":"1.0.0","type":"single","main":"demo","sha256":"` + strings.Repeat("0", 64) + `"}]}`,
		"/demo/demo":  "#!/bin/sh\necho demo\n",
	}, "", true)

	ctx := context.Background()
	pkg, err := env.manager.Info(ctx, "demo")
	if err != nil {
		t.Fatalf("Info() error = %v", err)
	}
	err = env.manager.Install(ctx, pkg)
	if err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("Install() error = %v, want checksum mismatch", err)
	}
	if _, ok := env.manager.Installed("demo"); ok {
		t.Error("package installed despite checksum mismatch")
	}
	if _, err := os.Stat(filepath.Join(env.paths.BinDir, "demo")); !os.IsNotExist(err) {
		t.Errorf("binary exists after checksum mismatch: %v", err)
	}
}

func TestUpgradeUsesInstalledRepository(t *testing.T) {
	firstVersion := "9.9.9"
	secondVersion := "1.0.0"
	handler := func(version *string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/INDEX":
				_, _ = io.WriteString(w, "demo\n")
			case "/demo/info.md":
				fmt.Fprintf(w, "## Name: Demo\n## Version: %s\n## Type: single\n## MainFile: demo\n", *version)
			case "/demo/demo":
				fmt.Fprintf(w, "#!/bin/sh\necho demo-%s\n", *version)
			default:
				http.NotFound(w, r)
			}
		}
	}
	first := httptest.NewServer(handler(&firstVersion))
	t.Cleanup(first.Close)
	second := httptest.NewServer(handler(&secondVersion))
	t.Cleanup(second.Close)

	env := newTestEnv(t, map[string]string{}, "", true)
	set, err := repos.New(repos.Config{Schema: repos.Schema, Repos: []repos.Repo{
		{Name: "first", URL: first.URL, Priority: 10},
		{Name: "second", URL: second.URL, Priority: 20},
	}}, nil)
	if err != nil {
		t.Fatalf("repos.New() error = %v", err)
	}
	env.manager.Repos = set

	ctx := context.Background()
	pkg, err := env.manager.Info(ctx, "demo@second")
	if err != nil {
		t.Fatalf("Info() error = %v", err)
	}
	if pkg.Repo != "second" || pkg.Version != "1.0.0" {
		t.Fatalf("pkg = %+v", pkg)
	}
	if err := env.manager.Install(ctx, pkg); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	installed, _ := env.manager.Installed("demo")
	if installed.Repo != "second" {
		t.Errorf("installed repo = %q", installed.Repo)
	}

	secondVersion = "2.0.0"
	upgrades, err := env.manager.Upgrades(ctx)
	if err != nil {
		t.Fatalf("Upgrades() error = %v", err)
	}
	if len(upgrades) != 1 || upgrades[0].Remote.Version != "2.0.0" || upgrades[0].Remote.Repo != "second" {
		t.Fatalf("upgrades = %+v", upgrades)
	}
	if err := env.manager.Upgrade(ctx, upgrades[0]); err != nil {
		t.Fatalf("Upgrade() error = %v", err)
	}
	installed, _ = env.manager.Installed("demo")
	if installed.Version != "2.0.0" || installed.Repo != "second" {
		t.Errorf("installed after upgrade = %+v", installed)
	}
}

func TestSplitRef(t *testing.T) {
	id, repoName, err := SplitRef("demo@mirror")
	if err != nil || id != "demo" || repoName != "mirror" {
		t.Errorf("SplitRef() = %q, %q, %v", id, repoName, err)
	}
	if _, _, err := SplitRef("../evil@mirror"); err == nil {
		t.Error("SplitRef(../evil) error = nil")
	}
	if _, _, err := SplitRef("demo@"); err == nil {
		t.Error("SplitRef(empty repo) error = nil")
	}
}

func TestExtractRejectsTraversal(t *testing.T) {
	archive := makeTarGz(t, map[string]string{"../evil": "boom"})
	dest := t.TempDir()
	if err := extractTarGz(bytes.NewReader(archive), dest); err == nil {
		t.Fatal("extractTarGz() error = nil")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dest), "evil")); !os.IsNotExist(err) {
		t.Errorf("traversal file created: %v", err)
	}
}

func makeTarGz(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&buffer)
	tarWriter := tar.NewWriter(gzipWriter)
	for name, content := range entries {
		header := &tar.Header{Name: name, Mode: 0o755, Size: int64(len(content))}
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(tarWriter, content); err != nil {
			t.Fatal(err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
