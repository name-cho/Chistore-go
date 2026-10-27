package repos

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/name-cho/chistore-go/internal/fsutil"
	"github.com/name-cho/chistore-go/internal/repo"
)

const (
	Schema          = 1
	DefaultPriority = 10
	DefaultName     = "gitgud"
	DefaultURL      = "https://gitgud.io/name-cho/Chistore/-/raw/main"
	OverrideName    = "custom"
)

var namePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

type Repo struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	Priority int    `json:"priority,omitempty"`
	Enabled  *bool  `json:"enabled,omitempty"`
}

func (r Repo) IsEnabled() bool {
	return r.Enabled == nil || *r.Enabled
}

type Config struct {
	Schema int    `json:"schema"`
	Repos  []Repo `json:"repos"`
}

func DefaultConfig() Config {
	return Config{
		Schema: Schema,
		Repos: []Repo{{
			Name:     DefaultName,
			URL:      DefaultURL,
			Priority: DefaultPriority,
			Enabled:  boolPtr(true),
		}},
	}
}

func ValidateName(name string) error {
	if !namePattern.MatchString(name) {
		return fmt.Errorf("invalid repository name %q", name)
	}
	return nil
}

func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return DefaultConfig(), nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("read %s: %w", path, err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse %s: %w", path, err)
	}
	if cfg.Schema == 0 {
		cfg.Schema = Schema
	}
	cfg.normalize()
	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

func (c *Config) Save(path string) error {
	c.Schema = Schema
	c.normalize()
	if err := c.Validate(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("encode repository list: %w", err)
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	if err := fsutil.WriteFileAtomic(path, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func (c *Config) Add(name, url string, priority int) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	if _, ok := c.Find(name); ok {
		return fmt.Errorf("repository %q already exists", name)
	}
	if priority <= 0 {
		priority = DefaultPriority
	}
	candidate := Repo{
		Name:     name,
		URL:      strings.TrimSpace(url),
		Priority: priority,
		Enabled:  boolPtr(true),
	}
	if err := validateRepo(candidate); err != nil {
		return err
	}
	c.Repos = append(c.Repos, candidate)
	sort.SliceStable(c.Repos, func(i, j int) bool {
		if c.Repos[i].Priority != c.Repos[j].Priority {
			return c.Repos[i].Priority < c.Repos[j].Priority
		}
		return c.Repos[i].Name < c.Repos[j].Name
	})
	return nil
}

func (c *Config) Remove(name string) error {
	for i, entry := range c.Repos {
		if entry.Name != name {
			continue
		}
		if c.enabledCount() == 1 && entry.IsEnabled() {
			return fmt.Errorf("cannot remove the last enabled repository")
		}
		c.Repos = append(c.Repos[:i], c.Repos[i+1:]...)
		return nil
	}
	return fmt.Errorf("repository %q not found", name)
}

func (c Config) Find(name string) (Repo, bool) {
	for _, entry := range c.Repos {
		if entry.Name == name {
			return entry, true
		}
	}
	return Repo{}, false
}

func (c Config) Enabled() []Repo {
	enabled := make([]Repo, 0, len(c.Repos))
	for _, entry := range c.Repos {
		if entry.IsEnabled() {
			enabled = append(enabled, entry)
		}
	}
	sort.SliceStable(enabled, func(i, j int) bool {
		if enabled[i].Priority != enabled[j].Priority {
			return enabled[i].Priority < enabled[j].Priority
		}
		return enabled[i].Name < enabled[j].Name
	})
	return enabled
}

func (c Config) Validate() error {
	if c.Schema != Schema {
		return fmt.Errorf("unsupported repository schema %d", c.Schema)
	}
	if len(c.Repos) == 0 {
		return fmt.Errorf("no repositories configured")
	}
	seen := map[string]bool{}
	for _, entry := range c.Repos {
		if err := validateRepo(entry); err != nil {
			return err
		}
		if seen[entry.Name] {
			return fmt.Errorf("duplicate repository name %q", entry.Name)
		}
		seen[entry.Name] = true
	}
	if c.enabledCount() == 0 {
		return fmt.Errorf("no enabled repositories")
	}
	return nil
}

func (c Config) enabledCount() int {
	count := 0
	for _, entry := range c.Repos {
		if entry.IsEnabled() {
			count++
		}
	}
	return count
}

func (c *Config) normalize() {
	for i := range c.Repos {
		if c.Repos[i].Priority <= 0 {
			c.Repos[i].Priority = DefaultPriority
		}
		if c.Repos[i].Enabled == nil {
			c.Repos[i].Enabled = boolPtr(true)
		}
		c.Repos[i].Name = strings.TrimSpace(c.Repos[i].Name)
		c.Repos[i].URL = strings.TrimSpace(c.Repos[i].URL)
	}
}

func validateRepo(entry Repo) error {
	if err := ValidateName(entry.Name); err != nil {
		return err
	}
	if entry.Priority <= 0 {
		return fmt.Errorf("repository %q has invalid priority %d", entry.Name, entry.Priority)
	}
	if _, err := repo.New(entry.URL, nil); err != nil {
		return fmt.Errorf("repository %q: %w", entry.Name, err)
	}
	return nil
}

func boolPtr(value bool) *bool {
	return &value
}
