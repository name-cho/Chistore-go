package config

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

type Paths struct {
	Home       string
	DataHome   string
	ConfigHome string
	BinDir     string
	StateDir   string
	VenvDir    string
}

func Load() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, fmt.Errorf("find home directory: %w", err)
	}
	return Resolve(home, os.Getenv, runtime.GOOS)
}

func Resolve(home string, getenv func(string) string, goos string) (Paths, error) {
	if home == "" {
		return Paths{}, fmt.Errorf("home directory is empty")
	}
	home = filepath.Clean(home)

	var dataHome, configHome, binDir string
	if goos == "windows" {
		local := absolute(getenv("LOCALAPPDATA"))
		if local == "" {
			local = filepath.Join(home, "AppData", "Local")
		}
		roaming := absolute(getenv("APPDATA"))
		if roaming == "" {
			roaming = filepath.Join(home, "AppData", "Roaming")
		}
		dataHome = local
		configHome = roaming
		binDir = filepath.Join(local, "Programs", "chistore", "bin")
	} else {
		dataHome = absolute(getenv("XDG_DATA_HOME"))
		if dataHome == "" {
			dataHome = filepath.Join(home, ".local", "share")
		}
		configHome = absolute(getenv("XDG_CONFIG_HOME"))
		if configHome == "" {
			configHome = filepath.Join(home, ".config")
		}
		binDir = absolute(getenv("XDG_BIN_HOME"))
		if binDir == "" {
			binDir = filepath.Join(home, ".local", "bin")
		}
	}

	stateDir := filepath.Join(dataHome, "chistore")
	return Paths{
		Home:       home,
		DataHome:   dataHome,
		ConfigHome: configHome,
		BinDir:     binDir,
		StateDir:   stateDir,
		VenvDir:    filepath.Join(stateDir, "venv"),
	}, nil
}

func absolute(value string) string {
	if value == "" {
		return ""
	}
	if filepath.IsAbs(value) || path.IsAbs(value) || isWindowsAbsolute(value) {
		return filepath.Clean(value)
	}
	return ""
}

func isWindowsAbsolute(value string) bool {
	if strings.HasPrefix(value, `\\`) {
		return true
	}
	if len(value) < 3 || value[1] != ':' {
		return false
	}
	drive := value[0]
	if (drive < 'A' || drive > 'Z') && (drive < 'a' || drive > 'z') {
		return false
	}
	return value[2] == '\\' || value[2] == '/'
}

func (p Paths) EnsureDirs() error {
	for _, dir := range []string{p.StateDir, p.BinDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}
	return nil
}

func (p Paths) PackageDataDir(id string) string {
	return filepath.Join(p.DataHome, id)
}

func (p Paths) PackageConfigDir(id string) string {
	return filepath.Join(p.ConfigHome, id)
}

func (p Paths) ReposFile() string {
	return filepath.Join(p.ConfigHome, "chistore", "repos.json")
}
