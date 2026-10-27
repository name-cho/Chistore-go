package repos

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if len(cfg.Repos) != 1 || cfg.Repos[0].Name != DefaultName || cfg.Repos[0].URL != DefaultURL {
		t.Fatalf("DefaultConfig() = %+v", cfg)
	}
	if !cfg.Repos[0].IsEnabled() || cfg.Repos[0].Priority != DefaultPriority {
		t.Errorf("default repo = %+v", cfg.Repos[0])
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config", "chistore", "repos.json")
	cfg := DefaultConfig()
	if err := cfg.Add("mirror", "https://example.com/repo", 20); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	cfg.Repos[1].Enabled = boolPtr(false)
	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(loaded.Repos) != 2 {
		t.Fatalf("repos = %+v", loaded.Repos)
	}
	mirror, ok := loaded.Find("mirror")
	if !ok || mirror.IsEnabled() || mirror.Priority != 20 {
		t.Errorf("mirror = %+v, %v", mirror, ok)
	}
}

func TestLoadMissingFileReturnsDefault(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Repos) != 1 || cfg.Repos[0].Name != DefaultName {
		t.Errorf("cfg = %+v", cfg)
	}
}

func TestLoadMigratesLegacyDefaultRepo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "repos.json")
	body := `{"schema":1,"repos":[{"name":"gitgud","url":"` + LegacyDefaultURL + `","priority":10}]}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Repos) != 1 || cfg.Repos[0].URL != DefaultURL {
		t.Errorf("repos = %+v", cfg.Repos)
	}
}

func TestLoadKeepsCustomizedLegacyConfig(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		wantURL string
	}{
		{
			name:    "extra repository",
			body:    `{"schema":1,"repos":[{"name":"gitgud","url":"` + LegacyDefaultURL + `"},{"name":"mirror","url":"https://example.com/repo"}]}`,
			wantURL: LegacyDefaultURL,
		},
		{
			name:    "renamed repository",
			body:    `{"schema":1,"repos":[{"name":"legacy","url":"` + LegacyDefaultURL + `"}]}`,
			wantURL: LegacyDefaultURL,
		},
		{
			name:    "custom url",
			body:    `{"schema":1,"repos":[{"name":"gitgud","url":"https://example.com/repo"}]}`,
			wantURL: "https://example.com/repo",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "repos.json")
			if err := os.WriteFile(path, []byte(tc.body), 0o644); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load(path)
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if cfg.Repos[0].URL != tc.wantURL {
				t.Errorf("URL = %q, want %q", cfg.Repos[0].URL, tc.wantURL)
			}
		})
	}
}

func TestAddRejectsDuplicateAndBadURL(t *testing.T) {
	cfg := DefaultConfig()
	if err := cfg.Add(DefaultName, "https://example.com", 10); err == nil {
		t.Error("Add(duplicate) error = nil")
	}
	if err := cfg.Add("bad", "ftp://example.com", 10); err == nil {
		t.Error("Add(bad url) error = nil")
	}
	if err := cfg.Add("bad name", "https://example.com", 10); err == nil {
		t.Error("Add(bad name) error = nil")
	}
}

func TestRemoveKeepsAtLeastOneEnabled(t *testing.T) {
	cfg := DefaultConfig()
	if err := cfg.Remove(DefaultName); err == nil {
		t.Error("Remove(last) error = nil")
	}
	if err := cfg.Add("mirror", "https://example.com", 20); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Remove(DefaultName); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if err := cfg.Remove("missing"); err == nil {
		t.Error("Remove(missing) error = nil")
	}
}

func TestEnabledSortsByPriority(t *testing.T) {
	cfg := DefaultConfig()
	if err := cfg.Add("late", "https://late.example.com", 30); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Add("early", "https://early.example.com", 5); err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0)
	for _, entry := range cfg.Enabled() {
		names = append(names, entry.Name)
	}
	if strings.Join(names, ",") != "early,gitgud,late" {
		t.Errorf("enabled = %v", names)
	}
}

func TestLoadRejectsInvalidConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "repos.json")
	cases := []string{
		`{"schema":1,"repos":[]}`,
		`{"schema":1,"repos":[{"name":"a","url":"https://example.com","enabled":false}]}`,
		`{"schema":2,"repos":[{"name":"a","url":"https://example.com"}]}`,
		`{"schema":1,"repos":[{"name":"a","url":"https://example.com"},{"name":"a","url":"https://example.com"}]}`,
	}
	for _, body := range cases {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path); err == nil {
			t.Errorf("Load(%s) error = nil", body)
		}
	}
}
