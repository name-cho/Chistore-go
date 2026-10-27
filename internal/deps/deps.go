package deps

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/name-cho/chistore-go/internal/metadata"
	"github.com/name-cho/chistore-go/internal/ui"
)

type Runner interface {
	LookPath(name string) (string, error)
	Run(ctx context.Context, name string, args ...string) error
	Output(ctx context.Context, name string, args ...string) ([]byte, error)
}

type ExecRunner struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

func (r ExecRunner) LookPath(name string) (string, error) {
	return exec.LookPath(name)
}

func (r ExecRunner) Run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = r.Stdin
	cmd.Stdout = r.Stdout
	cmd.Stderr = r.Stderr
	return cmd.Run()
}

func (r ExecRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

type Plan struct {
	System   []string
	Pip      []string
	Npm      []string
	NeedVenv bool
}

func (p Plan) Empty() bool {
	return len(p.System) == 0 && len(p.Pip) == 0 && len(p.Npm) == 0
}

func (p Plan) Describe() []string {
	var lines []string
	if len(p.System) > 0 {
		lines = append(lines, "System: "+strings.Join(p.System, " "))
	}
	if len(p.Pip) > 0 {
		lines = append(lines, "Python (venv): "+strings.Join(p.Pip, " "))
	}
	if len(p.Npm) > 0 {
		lines = append(lines, "Node: "+strings.Join(p.Npm, " "))
	}
	return lines
}

func BuildPlan(dependencies []metadata.Dependency, systemInstalled func(string) bool, venvReady bool) Plan {
	var plan Plan
	for _, dep := range dependencies {
		switch dep.Manager {
		case metadata.ManagerSystem:
			for _, name := range dep.Packages {
				if !systemInstalled(name) {
					plan.System = append(plan.System, name)
				}
			}
		case metadata.ManagerPip:
			plan.Pip = append(plan.Pip, dep.Packages...)
			plan.NeedVenv = plan.NeedVenv || !venvReady
		case metadata.ManagerNpm:
			plan.Npm = append(plan.Npm, dep.Packages...)
		}
	}
	return plan
}

type Tool struct {
	Manager     string
	InstallArgs []string
	Sudo        bool
}

var tools = []Tool{
	{Manager: "pacman", InstallArgs: []string{"-S", "--noconfirm"}, Sudo: true},
	{Manager: "apt", InstallArgs: []string{"install", "-y"}, Sudo: true},
	{Manager: "dnf", InstallArgs: []string{"install", "-y"}, Sudo: true},
	{Manager: "zypper", InstallArgs: []string{"install", "-y"}, Sudo: true},
	{Manager: "apk", InstallArgs: []string{"add"}, Sudo: true},
	{Manager: "brew", InstallArgs: []string{"install"}, Sudo: false},
}

func Detect(runner Runner) (Tool, error) {
	for _, tool := range tools {
		if _, err := runner.LookPath(tool.Manager); err == nil {
			return tool, nil
		}
	}
	return Tool{}, fmt.Errorf("no supported system package manager found (pacman, apt, dnf, zypper, apk, brew)")
}

func (t Tool) SystemInstalled(ctx context.Context, runner Runner, name string) bool {
	var output []byte
	var err error

	switch t.Manager {
	case "pacman":
		_, err = runner.Output(ctx, "pacman", "-Q", name)
	case "apt":
		output, err = runner.Output(ctx, "dpkg-query", "-W", "-f=${Status}", name)
	case "dnf", "zypper":
		_, err = runner.Output(ctx, "rpm", "-q", name)
	case "apk":
		_, err = runner.Output(ctx, "apk", "info", "-e", name)
	case "brew":
		_, err = runner.Output(ctx, "brew", "list", "--versions", name)
	default:
		return false
	}
	if err != nil {
		return false
	}
	return t.Manager != "apt" || strings.Contains(string(output), "install ok installed")
}

type Installer struct {
	Runner  Runner
	UI      *ui.UI
	VenvDir string
	Python  string
	Root    bool
}

func (i *Installer) Apply(ctx context.Context, plan Plan) error {
	if err := i.installSystem(ctx, plan.System); err != nil {
		return err
	}
	if len(plan.Pip) > 0 {
		if err := i.EnsureVenv(ctx); err != nil {
			return err
		}
		i.UI.Infof("Installing Python packages into %s", i.VenvDir)
		if err := i.Runner.Run(ctx, PipPath(i.VenvDir), append([]string{"install"}, plan.Pip...)...); err != nil {
			return fmt.Errorf("pip install: %w", err)
		}
	}
	if len(plan.Npm) > 0 {
		i.UI.Infof("Installing Node packages globally")
		if err := i.Runner.Run(ctx, "npm", append([]string{"install", "-g"}, plan.Npm...)...); err != nil {
			return fmt.Errorf("npm install: %w", err)
		}
	}
	return nil
}

func (i *Installer) installSystem(ctx context.Context, packages []string) error {
	if len(packages) == 0 {
		return nil
	}
	tool, err := Detect(i.Runner)
	if err != nil {
		return err
	}
	i.UI.Infof("Installing system packages with %s", tool.Manager)
	args := append(append([]string{}, tool.InstallArgs...), packages...)
	return i.runPrivileged(ctx, tool, tool.Manager, args)
}

func (i *Installer) EnsureVenv(ctx context.Context) error {
	if fileExists(PipPath(i.VenvDir)) {
		return nil
	}
	i.UI.Infof("Initializing Python venv at %s", i.VenvDir)
	python := i.Python
	if python == "" {
		python = "python3"
		if runtime.GOOS == "windows" {
			python = "python"
		}
	}
	if err := i.Runner.Run(ctx, python, "-m", "venv", i.VenvDir); err != nil {
		return fmt.Errorf("create Python venv: %w", err)
	}
	if err := i.Runner.Run(ctx, PipPath(i.VenvDir), "install", "--upgrade", "pip", "-q"); err != nil {
		return fmt.Errorf("upgrade pip: %w", err)
	}
	return nil
}

func (i *Installer) runPrivileged(ctx context.Context, tool Tool, name string, args []string) error {
	if tool.Sudo && !i.Root {
		return i.Runner.Run(ctx, "sudo", append([]string{name}, args...)...)
	}
	return i.Runner.Run(ctx, name, args...)
}

func PipPath(venvDir string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(venvDir, "Scripts", "pip.exe")
	}
	return filepath.Join(venvDir, "bin", "pip")
}

func PythonPath(venvDir string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(venvDir, "Scripts", "python.exe")
	}
	return filepath.Join(venvDir, "bin", "python3")
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
