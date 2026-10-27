package deps

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/name-cho/chistore-go/internal/metadata"
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

func (f *fakeRunner) joined() []string {
	out := make([]string, 0, len(f.commands))
	for _, command := range f.commands {
		out = append(out, strings.Join(command, " "))
	}
	return out
}

func testInstaller(runner Runner, venv string) *Installer {
	return &Installer{
		Runner:  runner,
		UI:      ui.New(strings.NewReader(""), io.Discard, io.Discard, false, true),
		VenvDir: venv,
		Python:  "python3",
	}
}

func mustDeps(t *testing.T, value string) []metadata.Dependency {
	t.Helper()
	deps, err := metadata.ParseDependencies(value)
	if err != nil {
		t.Fatalf("ParseDependencies(%q) error = %v", value, err)
	}
	return deps
}

func TestBuildPlan(t *testing.T) {
	plan := BuildPlan(
		mustDeps(t, "sys:bash,curl pip:rich,psutil npm:prettier"),
		func(name string) bool { return name == "bash" },
		false,
	)
	if strings.Join(plan.System, ",") != "curl" {
		t.Errorf("System = %v, want curl", plan.System)
	}
	if strings.Join(plan.Pip, ",") != "rich,psutil" {
		t.Errorf("Pip = %v", plan.Pip)
	}
	if strings.Join(plan.Npm, ",") != "prettier" {
		t.Errorf("Npm = %v", plan.Npm)
	}
	if !plan.NeedVenv {
		t.Error("NeedVenv = false, want true")
	}
	if plan.Empty() {
		t.Error("Empty() = true")
	}
}

func TestBuildPlanWhenEverythingIsReady(t *testing.T) {
	plan := BuildPlan(
		mustDeps(t, "sys:bash"),
		func(string) bool { return true },
		true,
	)
	if !plan.Empty() {
		t.Errorf("plan = %+v, want empty", plan)
	}
}

func TestPlanDescribe(t *testing.T) {
	plan := Plan{System: []string{"curl"}, Pip: []string{"rich"}, Npm: []string{"prettier"}}
	lines := plan.Describe()
	if len(lines) != 3 {
		t.Fatalf("Describe() = %v", lines)
	}
	if !strings.Contains(lines[0], "System:") || !strings.Contains(lines[0], "curl") {
		t.Errorf("line 0 = %q", lines[0])
	}
}

func TestDetectPrefersPacman(t *testing.T) {
	runner := &fakeRunner{paths: map[string]bool{"pacman": true, "apt": true}}
	tool, err := Detect(runner)
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if tool.Manager != "pacman" || strings.Join(tool.InstallArgs, " ") != "-S --noconfirm" || !tool.Sudo {
		t.Errorf("tool = %+v", tool)
	}
}

func TestDetectNoManager(t *testing.T) {
	if _, err := Detect(&fakeRunner{}); err == nil {
		t.Fatal("Detect() error = nil")
	}
}

func TestToolSystemInstalled(t *testing.T) {
	runner := &fakeRunner{
		paths:   map[string]bool{"apt": true},
		outputs: map[string]string{"dpkg-query": "install ok installed"},
	}
	tool, err := Detect(runner)
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if !tool.SystemInstalled(context.Background(), runner, "curl") {
		t.Error("SystemInstalled(curl) = false")
	}
}

func TestEnsureVenvCreatesMissingVenv(t *testing.T) {
	runner := &fakeRunner{}
	installer := testInstaller(runner, filepath.Join(t.TempDir(), "venv"))
	if err := installer.EnsureVenv(context.Background()); err != nil {
		t.Fatalf("EnsureVenv() error = %v", err)
	}
	commands := runner.joined()
	if len(commands) != 2 {
		t.Fatalf("commands = %v", commands)
	}
	if !strings.Contains(commands[0], "python3 -m venv") {
		t.Errorf("command 0 = %q", commands[0])
	}
	if !strings.Contains(commands[1], "install --upgrade pip") {
		t.Errorf("command 1 = %q", commands[1])
	}
}

func TestEnsureVenvSkipsExisting(t *testing.T) {
	venv := t.TempDir()
	pip := PipPath(venv)
	if err := os.MkdirAll(filepath.Dir(pip), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pip, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	if err := testInstaller(runner, venv).EnsureVenv(context.Background()); err != nil {
		t.Fatalf("EnsureVenv() error = %v", err)
	}
	if len(runner.commands) != 0 {
		t.Errorf("commands = %v, want none", runner.joined())
	}
}

func TestApplySystemUsesSudo(t *testing.T) {
	runner := &fakeRunner{paths: map[string]bool{"pacman": true}}
	installer := testInstaller(runner, t.TempDir())
	installer.Root = false

	if err := installer.Apply(context.Background(), Plan{System: []string{"curl", "bash"}}); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	want := "sudo pacman -S --noconfirm curl bash"
	if got := runner.joined(); len(got) != 1 || got[0] != want {
		t.Errorf("commands = %v, want %q", got, want)
	}
}

func TestApplyPipAndNpm(t *testing.T) {
	venv := t.TempDir()
	pip := PipPath(venv)
	if err := os.MkdirAll(filepath.Dir(pip), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pip, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	runner := &fakeRunner{paths: map[string]bool{"npm": true}}
	installer := testInstaller(runner, venv)
	if err := installer.Apply(context.Background(), Plan{Pip: []string{"rich"}, Npm: []string{"prettier"}}); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	commands := runner.joined()
	if len(commands) != 2 {
		t.Fatalf("commands = %v", commands)
	}
	if !strings.Contains(commands[0], "pip") || !strings.Contains(commands[0], "install rich") {
		t.Errorf("command 0 = %q", commands[0])
	}
	if commands[1] != "npm install -g prettier" {
		t.Errorf("command 1 = %q", commands[1])
	}
}

func TestApplyEmptyPlanDoesNothing(t *testing.T) {
	runner := &fakeRunner{}
	if err := testInstaller(runner, t.TempDir()).Apply(context.Background(), Plan{}); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if len(runner.commands) != 0 {
		t.Errorf("commands = %v", runner.joined())
	}
}

func TestPipPathPerOS(t *testing.T) {
	pip := PipPath(filepath.FromSlash("/venv"))
	if runtime.GOOS == "windows" {
		if !strings.HasSuffix(pip, filepath.Join("Scripts", "pip.exe")) {
			t.Errorf("PipPath() = %q", pip)
		}
		return
	}
	if pip != filepath.FromSlash("/venv/bin/pip") {
		t.Errorf("PipPath() = %q", pip)
	}
}
