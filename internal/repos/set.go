package repos

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/name-cho/chistore-go/internal/metadata"
	"github.com/name-cho/chistore-go/internal/repo"
)

type entry struct {
	repo   Repo
	client *repo.Client
}

type Set struct {
	entries []entry
	byName  map[string]*repo.Client
}

type Status struct {
	Repo     Repo
	Packages int
	Err      error
}

func New(cfg Config, httpClient *http.Client) (*Set, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	set := &Set{byName: map[string]*repo.Client{}}
	for _, item := range cfg.Enabled() {
		client, err := repo.New(item.URL, httpClient)
		if err != nil {
			return nil, fmt.Errorf("repository %q: %w", item.Name, err)
		}
		set.entries = append(set.entries, entry{repo: item, client: client})
		set.byName[item.Name] = client
	}
	return set, nil
}

func NewSingle(name, url string, httpClient *http.Client) (*Set, error) {
	if err := ValidateName(name); err != nil {
		return nil, err
	}
	client, err := repo.New(url, httpClient)
	if err != nil {
		return nil, err
	}
	return &Set{
		entries: []entry{{repo: Repo{Name: name, URL: client.BaseURL(), Priority: DefaultPriority}, client: client}},
		byName:  map[string]*repo.Client{name: client},
	}, nil
}

func (s *Set) Repositories() []Repo {
	list := make([]Repo, len(s.entries))
	for i, item := range s.entries {
		list[i] = item.repo
	}
	return list
}

func (s *Set) Client(name string) (*repo.Client, bool) {
	client, ok := s.byName[name]
	return client, ok
}

func (s *Set) Packages(ctx context.Context) ([]metadata.Package, []error) {
	var packages []metadata.Package
	var failures []error
	seen := map[string]bool{}

	// Entries are sorted by priority, so the first repository that lists an id wins.
	for _, item := range s.entries {
		remote, err := item.client.Packages(ctx)
		if err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", item.repo.Name, err))
			continue
		}
		for _, pkg := range remote {
			if seen[pkg.ID] {
				continue
			}
			seen[pkg.ID] = true
			pkg.Repo = item.repo.Name
			packages = append(packages, pkg)
		}
	}
	return packages, failures
}

func (s *Set) Search(ctx context.Context, query string) ([]metadata.Package, []error) {
	packages, failures := s.Packages(ctx)
	query = strings.ToLower(query)
	matches := make([]metadata.Package, 0, len(packages))
	for _, pkg := range packages {
		if matchesQuery(pkg, query) {
			matches = append(matches, pkg)
		}
	}
	return matches, failures
}

func (s *Set) Info(ctx context.Context, id, repoName string) (metadata.Package, error) {
	if repoName != "" {
		client, ok := s.byName[repoName]
		if !ok {
			return metadata.Package{}, fmt.Errorf("unknown repository %q", repoName)
		}
		packages, err := client.Packages(ctx)
		if err != nil {
			return metadata.Package{}, fmt.Errorf("%s: %w", repoName, err)
		}
		for _, pkg := range packages {
			if pkg.ID == id {
				pkg.Repo = repoName
				return pkg, nil
			}
		}
		return metadata.Package{}, fmt.Errorf("package %s not found in repository %s", id, repoName)
	}

	var failures []string
	for _, item := range s.entries {
		packages, err := item.client.Packages(ctx)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", item.repo.Name, err))
			continue
		}
		for _, pkg := range packages {
			if pkg.ID == id {
				pkg.Repo = item.repo.Name
				return pkg, nil
			}
		}
	}
	if len(failures) > 0 {
		return metadata.Package{}, fmt.Errorf("package %s not found (%s)", id, strings.Join(failures, "; "))
	}
	return metadata.Package{}, fmt.Errorf("package %s not found in configured repositories", id)
}

func (s *Set) Status(ctx context.Context) []Status {
	statuses := make([]Status, 0, len(s.entries))
	for _, item := range s.entries {
		packages, err := item.client.Packages(ctx)
		statuses = append(statuses, Status{Repo: item.repo, Packages: len(packages), Err: err})
	}
	return statuses
}

func matchesQuery(pkg metadata.Package, query string) bool {
	if query == "" {
		return true
	}
	for _, field := range []string{pkg.ID, pkg.Name, pkg.Description} {
		if strings.Contains(strings.ToLower(field), query) {
			return true
		}
	}
	return false
}
