package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/name-cho/chistore-go/internal/repos"
)

type buffers struct {
	out, err *bytes.Buffer
}

func newBuffers() *buffers {
	return &buffers{out: &bytes.Buffer{}, err: &bytes.Buffer{}}
}

func (b *buffers) options(home, workDir string, input string, server *httptest.Server) Options {
	var client *http.Client
	if server != nil {
		client = server.Client()
	}
	return Options{
		Version:           "2.0.0",
		StdIn:             strings.NewReader(input),
		StdOut:            b.out,
		StdErr:            b.err,
		Home:              home,
		WorkDir:           workDir,
		GOOS:              "linux",
		DisableAutoUpdate: true,
		Now:               func() time.Time { return time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC) },
		HTTP:              client,
		Getenv: func(key string) string {
			switch key {
			case "CHISTORE_REPO":
				if server != nil {
					return server.URL
				}
				return ""
			case "XDG_DATA_HOME":
				return filepath.Join(home, "data")
			case "XDG_CONFIG_HOME":
				return filepath.Join(home, "config")
			case "XDG_BIN_HOME":
				return filepath.Join(home, "bin")
			default:
				return ""
			}
		},
	}
}

func repoServer(version string) (*httptest.Server, *string) {
	current := version
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/INDEX":
			fmt.Fprint(w, "demo\n")
		case "/demo/info.md":
			fmt.Fprintf(w, "## Name: Demo\n## Description: A demo package\n## Version: %s\n## Size: 1K\n## Type: single\n## MainFile: demo\n", current)
		case "/demo/demo":
			fmt.Fprintf(w, "#!/bin/sh\necho demo-%s\n", current)
		default:
			http.NotFound(w, r)
		}
	}))
	return server, &current
}

func TestLegacyFlow(t *testing.T) {
	server, _ := repoServer("1.0.0")
	defer server.Close()
	home := t.TempDir()
	buf := newBuffers()
	opts := buf.options(home, t.TempDir(), "", server)

	steps := [][]string{
		{"-y", "-S", "demo"},
		{"-Qet"},
		{"-Ss", "demo"},
		{"-Qi", "demo"},
		{"-Rns", "-y", "demo"},
	}
	for _, args := range steps {
		if code := Run(context.Background(), args, opts); code != 0 {
			t.Fatalf("Run(%v) = %d, stderr: %s", args, code, buf.err.String())
		}
	}

	output := buf.out.String()
	for _, want := range []string{"installed", "demo v1.0.0", "Demo", "A demo package", "removed"} {
		if !strings.Contains(output, want) {
			t.Errorf("output does not contain %q:\n%s", want, output)
		}
	}
	if _, err := os.Stat(filepath.Join(home, "bin", "demo")); !os.IsNotExist(err) {
		t.Errorf("binary still exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "config", "demo")); !os.IsNotExist(err) {
		t.Errorf("config still exists: %v", err)
	}
}

func TestSubcommandFlowAndUpgrade(t *testing.T) {
	server, version := repoServer("1.0.0")
	defer server.Close()
	home := t.TempDir()
	buf := newBuffers()
	opts := buf.options(home, t.TempDir(), "", server)

	if code := Run(context.Background(), []string{"install", "--yes", "demo"}, opts); code != 0 {
		t.Fatalf("install = %d, stderr: %s", code, buf.err.String())
	}
	*version = "1.1.0"
	buf.out.Reset()
	buf.err.Reset()
	if code := Run(context.Background(), []string{"upgrade", "--yes"}, opts); code != 0 {
		t.Fatalf("upgrade = %d, stderr: %s", code, buf.err.String())
	}
	if !strings.Contains(buf.out.String(), "updated to 1.1.0") {
		t.Errorf("upgrade output = %s", buf.out.String())
	}
	data, err := os.ReadFile(filepath.Join(home, "bin", "demo"))
	if err != nil || !strings.Contains(string(data), "demo-1.1.0") {
		t.Errorf("binary = %q, %v", data, err)
	}

	buf.out.Reset()
	if code := Run(context.Background(), []string{"list"}, opts); code != 0 {
		t.Fatalf("list = %d, stderr: %s", code, buf.err.String())
	}
	if !strings.Contains(buf.out.String(), "demo v1.1.0") {
		t.Errorf("list output = %s", buf.out.String())
	}
}

func TestInstallCancelled(t *testing.T) {
	server, _ := repoServer("1.0.0")
	defer server.Close()
	home := t.TempDir()
	buf := newBuffers()
	opts := buf.options(home, t.TempDir(), "n\n", server)

	if code := Run(context.Background(), []string{"install", "demo"}, opts); code != 0 {
		t.Fatalf("Run() = %d, stderr: %s", code, buf.err.String())
	}
	if !strings.Contains(buf.out.String(), "Cancelled") {
		t.Errorf("output = %s", buf.out.String())
	}
	if _, err := os.Stat(filepath.Join(home, "bin", "demo")); !os.IsNotExist(err) {
		t.Errorf("binary exists: %v", err)
	}
	for _, dir := range []string{filepath.Join(home, "config", "demo"), filepath.Join(home, "data", "demo")} {
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Errorf("package dir %q created after cancel: %v", dir, err)
		}
	}
}

func TestListAndRemoveDoNotNeedRepository(t *testing.T) {
	server, _ := repoServer("1.0.0")
	defer server.Close()
	home := t.TempDir()
	buf := newBuffers()
	opts := buf.options(home, t.TempDir(), "", server)
	if code := Run(context.Background(), []string{"install", "--yes", "demo"}, opts); code != 0 {
		t.Fatalf("install = %d, stderr: %s", code, buf.err.String())
	}

	broken := opts
	broken.Getenv = func(key string) string {
		if key == "CHISTORE_REPO" {
			return "://broken"
		}
		return opts.Getenv(key)
	}
	buf.out.Reset()
	buf.err.Reset()
	if code := Run(context.Background(), []string{"list"}, broken); code != 0 {
		t.Fatalf("list = %d, stderr: %s", code, buf.err.String())
	}
	if !strings.Contains(buf.out.String(), "demo") {
		t.Errorf("list output = %s", buf.out.String())
	}
	if code := Run(context.Background(), []string{"purge", "--yes", "demo"}, broken); code != 0 {
		t.Fatalf("purge = %d, stderr: %s", code, buf.err.String())
	}
}

func TestSearchDoesNotCreateState(t *testing.T) {
	server, _ := repoServer("1.0.0")
	defer server.Close()
	home := t.TempDir()
	buf := newBuffers()
	opts := buf.options(home, t.TempDir(), "", server)
	opts.DisableAutoUpdate = false

	if code := Run(context.Background(), []string{"search", "demo"}, opts); code != 0 {
		t.Fatalf("search = %d, stderr: %s", code, buf.err.String())
	}
	if _, err := os.Stat(filepath.Join(home, "data")); !os.IsNotExist(err) {
		t.Errorf("search created state dir: %v", err)
	}
}

func TestHelpDoesNotCreateState(t *testing.T) {
	home := t.TempDir()
	buf := newBuffers()
	opts := buf.options(home, t.TempDir(), "", nil)
	opts.HTTP = http.DefaultClient

	if code := Run(context.Background(), []string{"-h"}, opts); code != 0 {
		t.Fatalf("Run() = %d", code)
	}
	if !strings.Contains(buf.out.String(), "Usage:") {
		t.Errorf("help output = %s", buf.out.String())
	}
	if _, err := os.Stat(filepath.Join(home, "data")); !os.IsNotExist(err) {
		t.Errorf("help created state dir: %v", err)
	}
}

func TestPackageTooling(t *testing.T) {
	buf := newBuffers()
	workDir := t.TempDir()
	opts := buf.options(t.TempDir(), workDir, "", nil)
	opts.HTTP = http.DefaultClient

	for _, args := range [][]string{{"pkg", "init", "demo"}, {"pkg", "index"}, {"pkg", "validate"}} {
		if code := Run(context.Background(), args, opts); code != 0 {
			t.Fatalf("Run(%v) = %d, stderr: %s", args, code, buf.err.String())
		}
	}
	if _, err := os.Stat(filepath.Join(workDir, "demo", "info.md")); err != nil {
		t.Errorf("info.md missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workDir, "INDEX")); !os.IsNotExist(err) {
		t.Errorf("INDEX should not be created in a new repository")
	}

	if err := os.Remove(filepath.Join(workDir, "demo", "demo")); err != nil {
		t.Fatal(err)
	}
	buf.out.Reset()
	if code := Run(context.Background(), []string{"pkg", "validate"}, opts); code == 0 {
		t.Fatal("validate succeeded with missing main file")
	}
}

func TestRepoCommands(t *testing.T) {
	home := t.TempDir()
	buf := newBuffers()
	opts := buf.options(home, t.TempDir(), "", nil)
	opts.HTTP = http.DefaultClient

	if code := Run(context.Background(), []string{"repo", "add", "mirror", "https://example.com/repo", "--priority", "5"}, opts); code != 0 {
		t.Fatalf("repo add = %d, stderr: %s", code, buf.err.String())
	}
	configPath := filepath.Join(home, "config", "chistore", "repos.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("repos.json error = %v", err)
	}
	if !strings.Contains(string(data), `"mirror"`) || !strings.Contains(string(data), `"priority": 5`) {
		t.Errorf("repos.json = %s", data)
	}

	buf.out.Reset()
	buf.err.Reset()
	if code := Run(context.Background(), []string{"repo", "list"}, opts); code != 0 {
		t.Fatalf("repo list = %d, stderr: %s", code, buf.err.String())
	}
	if !strings.Contains(buf.out.String(), "mirror") || !strings.Contains(buf.out.String(), "gitgud") {
		t.Errorf("repo list output = %s", buf.out.String())
	}

	if code := Run(context.Background(), []string{"repo", "add", "mirror", "https://example.com/other"}, opts); code == 0 {
		t.Error("duplicate repo add succeeded")
	}
	if code := Run(context.Background(), []string{"repo", "remove", "mirror"}, opts); code != 0 {
		t.Fatalf("repo remove = %d, stderr: %s", code, buf.err.String())
	}
	if code := Run(context.Background(), []string{"repo", "remove", "gitgud"}, opts); code == 0 {
		t.Error("removing the last repository succeeded")
	}
}

func TestMultiRepositoryInstallAndUpgrade(t *testing.T) {
	firstVersion := "9.9.9"
	secondVersion := "1.0.0"
	handler := func(version *string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/INDEX":
				fmt.Fprint(w, "demo\n")
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
	defer first.Close()
	second := httptest.NewServer(handler(&secondVersion))
	defer second.Close()

	home := t.TempDir()
	cfg := repos.Config{Schema: repos.Schema, Repos: []repos.Repo{
		{Name: "first", URL: first.URL, Priority: 10},
		{Name: "second", URL: second.URL, Priority: 20},
	}}
	if err := cfg.Save(filepath.Join(home, "config", "chistore", "repos.json")); err != nil {
		t.Fatal(err)
	}

	buf := newBuffers()
	opts := buf.options(home, t.TempDir(), "", nil)
	opts.HTTP = http.DefaultClient

	if code := Run(context.Background(), []string{"install", "--yes", "demo@second"}, opts); code != 0 {
		t.Fatalf("install = %d, stderr: %s", code, buf.err.String())
	}
	data, err := os.ReadFile(filepath.Join(home, "bin", "demo"))
	if err != nil || !strings.Contains(string(data), "demo-1.0.0") {
		t.Fatalf("binary = %q, %v", data, err)
	}
	buf.out.Reset()
	if code := Run(context.Background(), []string{"list"}, opts); code != 0 {
		t.Fatalf("list = %d, stderr: %s", code, buf.err.String())
	}
	if !strings.Contains(buf.out.String(), "@second") {
		t.Errorf("list output = %s", buf.out.String())
	}

	secondVersion = "2.0.0"
	buf.out.Reset()
	buf.err.Reset()
	if code := Run(context.Background(), []string{"upgrade", "--yes"}, opts); code != 0 {
		t.Fatalf("upgrade = %d, stderr: %s", code, buf.err.String())
	}
	data, err = os.ReadFile(filepath.Join(home, "bin", "demo"))
	if err != nil || !strings.Contains(string(data), "demo-2.0.0") {
		t.Fatalf("binary after upgrade = %q, %v", data, err)
	}

	buf.out.Reset()
	if code := Run(context.Background(), []string{"repo", "refresh"}, opts); code != 0 {
		t.Fatalf("repo refresh = %d, stderr: %s", code, buf.err.String())
	}
	if !strings.Contains(buf.out.String(), "first") || !strings.Contains(buf.out.String(), "second") {
		t.Errorf("refresh output = %s", buf.out.String())
	}
}

func TestSelfUpdateCommand(t *testing.T) {
	newBinary := []byte("#!/bin/sh\necho new\n")
	sum := sha256.Sum256(newBinary)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			fmt.Fprintf(w, `{"tag_name":"v2.0.0","assets":[{"name":"chistore_linux_amd64","browser_download_url":"%s/bin"},{"name":"checksums.txt","browser_download_url":"%s/sums"}]}`, server.URL, server.URL)
		case "/sums":
			fmt.Fprintf(w, "%s  chistore_linux_amd64\n", hex.EncodeToString(sum[:]))
		case "/bin":
			_, _ = w.Write(newBinary)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	target := filepath.Join(t.TempDir(), "chistore")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	buf := newBuffers()
	opts := buf.options(t.TempDir(), t.TempDir(), "", server)
	opts.Version = "1.0.0"
	opts.UpdateURL = server.URL + "/latest"
	opts.Executable = target

	if code := Run(context.Background(), []string{"self-update"}, opts); code != 0 {
		t.Fatalf("Run() = %d, stderr: %s", code, buf.err.String())
	}
	data, _ := os.ReadFile(target)
	if string(data) != string(newBinary) {
		t.Errorf("target = %q", data)
	}
	if !strings.Contains(buf.out.String(), "updated") {
		t.Errorf("output = %s", buf.out.String())
	}
}

func TestBadRepoURL(t *testing.T) {
	buf := newBuffers()
	opts := buf.options(t.TempDir(), t.TempDir(), "", nil)
	opts.HTTP = http.DefaultClient
	if code := Run(context.Background(), []string{"--repo", "://bad", "search"}, opts); code == 0 {
		t.Fatal("Run() = 0 for bad repo URL")
	}
	if !strings.Contains(buf.err.String(), "repository") {
		t.Errorf("stderr = %s", buf.err.String())
	}
}

func TestUnknownCommand(t *testing.T) {
	buf := newBuffers()
	opts := buf.options(t.TempDir(), t.TempDir(), "", nil)
	if code := Run(context.Background(), []string{"nope"}, opts); code == 0 {
		t.Fatal("Run() = 0 for unknown command")
	}
	if !strings.Contains(buf.err.String(), "unknown command") {
		t.Errorf("stderr = %s", buf.err.String())
	}
}
