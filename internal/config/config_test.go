package config

import (
	"os"
	"path/filepath"
	"testing"
)

func env(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestResolveUnixDefaults(t *testing.T) {
	paths, err := Resolve("/home/cho", env(nil), "linux")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if paths.DataHome != filepath.Join("/home/cho", ".local/share") {
		t.Errorf("DataHome = %q", paths.DataHome)
	}
	if paths.ConfigHome != filepath.Join("/home/cho", ".config") {
		t.Errorf("ConfigHome = %q", paths.ConfigHome)
	}
	if paths.BinDir != filepath.Join("/home/cho", ".local/bin") {
		t.Errorf("BinDir = %q", paths.BinDir)
	}
	if paths.StateDir != filepath.Join("/home/cho", ".local/share/chistore") {
		t.Errorf("StateDir = %q", paths.StateDir)
	}
	if paths.VenvDir != filepath.Join("/home/cho", ".local/share/chistore/venv") {
		t.Errorf("VenvDir = %q", paths.VenvDir)
	}
}

func TestResolveXDGOverrides(t *testing.T) {
	paths, err := Resolve("/home/cho", env(map[string]string{
		"XDG_DATA_HOME":   "/data",
		"XDG_CONFIG_HOME": "/conf",
		"XDG_BIN_HOME":    "/bins",
	}), "linux")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if paths.DataHome != filepath.Clean("/data") || paths.ConfigHome != filepath.Clean("/conf") || paths.BinDir != filepath.Clean("/bins") {
		t.Errorf("XDG paths not applied: %+v", paths)
	}
	if paths.StateDir != filepath.Join("/data", "chistore") {
		t.Errorf("StateDir = %q", paths.StateDir)
	}
}

func TestResolveIgnoresRelativeXDGPaths(t *testing.T) {
	paths, err := Resolve("/home/cho", env(map[string]string{
		"XDG_DATA_HOME": "relative",
	}), "linux")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if paths.DataHome != filepath.Join("/home/cho", ".local/share") {
		t.Errorf("DataHome = %q, want default", paths.DataHome)
	}
}

func TestResolveWindowsDefaults(t *testing.T) {
	paths, err := Resolve(`C:\Users\cho`, env(map[string]string{
		"LOCALAPPDATA": `C:\Users\cho\AppData\Local`,
		"APPDATA":      `C:\Users\cho\AppData\Roaming`,
	}), "windows")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if paths.StateDir != filepath.Join(`C:\Users\cho\AppData\Local`, "chistore") {
		t.Errorf("StateDir = %q", paths.StateDir)
	}
	if paths.BinDir != filepath.Join(`C:\Users\cho\AppData\Local`, "Programs", "chistore", "bin") {
		t.Errorf("BinDir = %q", paths.BinDir)
	}
}

func TestPackageDirs(t *testing.T) {
	paths := Paths{
		DataHome:   filepath.Clean("/data"),
		ConfigHome: filepath.Clean("/conf"),
	}
	if got := paths.PackageDataDir("demo"); got != filepath.Join("/data", "demo") {
		t.Errorf("PackageDataDir = %q", got)
	}
	if got := paths.PackageConfigDir("demo"); got != filepath.Join("/conf", "demo") {
		t.Errorf("PackageConfigDir = %q", got)
	}
}

func TestEnsureDirs(t *testing.T) {
	root := t.TempDir()
	paths := Paths{
		StateDir: filepath.Join(root, "state"),
		BinDir:   filepath.Join(root, "bin"),
	}
	if err := paths.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs() error = %v", err)
	}
	for _, dir := range []string{paths.StateDir, paths.BinDir} {
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			t.Errorf("directory %q not created: %v", dir, err)
		}
	}
}
