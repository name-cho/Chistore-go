package repos

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/name-cho/chistore-go/internal/index"
	"github.com/name-cho/chistore-go/internal/metadata"
)

func packageWith(id, version, description string) metadata.Package {
	return metadata.Package{
		ID:          id,
		Name:        strings.ToUpper(id),
		Description: description,
		Version:     version,
		Size:        "1K",
		Kind:        metadata.KindSingle,
		MainFile:    id,
	}
}

func manifestServer(t *testing.T, packages []metadata.Package) *httptest.Server {
	t.Helper()
	manifest := index.NewManifest("test", packages, time.Now())
	data, err := manifest.Encode()
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/index.json" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(data)
	}))
	t.Cleanup(server.Close)
	return server
}

func testConfig(entries ...Repo) Config {
	return Config{Schema: Schema, Repos: entries}
}

func TestPriorityResolvesDuplicates(t *testing.T) {
	high := manifestServer(t, []metadata.Package{packageWith("demo", "1.0.0", "high priority")})
	low := manifestServer(t, []metadata.Package{packageWith("demo", "2.0.0", "low priority")})

	set, err := New(testConfig(
		Repo{Name: "high", URL: high.URL, Priority: 10},
		Repo{Name: "low", URL: low.URL, Priority: 20},
	), nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	packages, failures := set.Packages(context.Background())
	if len(failures) != 0 {
		t.Fatalf("failures = %v", failures)
	}
	if len(packages) != 1 || packages[0].Version != "1.0.0" || packages[0].Repo != "high" {
		t.Fatalf("packages = %+v", packages)
	}
}

func TestInfoCanTargetRepository(t *testing.T) {
	high := manifestServer(t, []metadata.Package{packageWith("demo", "1.0.0", "")})
	low := manifestServer(t, []metadata.Package{packageWith("demo", "2.0.0", "")})

	set, err := New(testConfig(
		Repo{Name: "high", URL: high.URL, Priority: 10},
		Repo{Name: "low", URL: low.URL, Priority: 20},
	), nil)
	if err != nil {
		t.Fatal(err)
	}

	pkg, err := set.Info(context.Background(), "demo", "low")
	if err != nil {
		t.Fatalf("Info() error = %v", err)
	}
	if pkg.Version != "2.0.0" || pkg.Repo != "low" {
		t.Errorf("pkg = %+v", pkg)
	}
	if _, err := set.Info(context.Background(), "demo", "missing"); err == nil {
		t.Error("Info(unknown repo) error = nil")
	}
	if _, err := set.Info(context.Background(), "missing", ""); err == nil {
		t.Error("Info(missing package) error = nil")
	}
}

func TestSearchMatchesMetadata(t *testing.T) {
	server := manifestServer(t, []metadata.Package{
		packageWith("alpha", "1.0.0", "snapshot tool"),
		packageWith("beta", "1.0.0", "network helper"),
	})
	set, err := New(testConfig(Repo{Name: "main", URL: server.URL, Priority: 10}), nil)
	if err != nil {
		t.Fatal(err)
	}

	matches, failures := set.Search(context.Background(), "SNAP")
	if len(failures) != 0 || len(matches) != 1 || matches[0].ID != "alpha" {
		t.Errorf("matches = %+v, failures = %v", matches, failures)
	}
}

func TestLegacyRepositoryFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/INDEX":
			_, _ = w.Write([]byte("demo\n"))
		case "/demo/info.md":
			_, _ = w.Write([]byte("## Name: Demo\n## Version: 1.0.0\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	set, err := New(testConfig(Repo{Name: "legacy", URL: server.URL, Priority: 10}), nil)
	if err != nil {
		t.Fatal(err)
	}
	packages, failures := set.Packages(context.Background())
	if len(failures) != 0 {
		t.Fatalf("failures = %v", failures)
	}
	if len(packages) != 1 || packages[0].Repo != "legacy" {
		t.Fatalf("packages = %+v", packages)
	}
}

func TestPackagesReportsRepositoryFailures(t *testing.T) {
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(broken.Close)
	good := manifestServer(t, []metadata.Package{packageWith("demo", "1.0.0", "")})

	set, err := New(testConfig(
		Repo{Name: "broken", URL: broken.URL, Priority: 10},
		Repo{Name: "good", URL: good.URL, Priority: 20},
	), nil)
	if err != nil {
		t.Fatal(err)
	}

	packages, failures := set.Packages(context.Background())
	if len(packages) != 1 || packages[0].Repo != "good" {
		t.Errorf("packages = %+v", packages)
	}
	if len(failures) != 1 || !strings.Contains(failures[0].Error(), "broken") {
		t.Errorf("failures = %v", failures)
	}
}

func TestNewSingle(t *testing.T) {
	server := manifestServer(t, nil)
	set, err := NewSingle(OverrideName, server.URL, nil)
	if err != nil {
		t.Fatalf("NewSingle() error = %v", err)
	}
	list := set.Repositories()
	if len(list) != 1 || list[0].Name != OverrideName {
		t.Errorf("repositories = %+v", list)
	}
	if _, ok := set.Client(OverrideName); !ok {
		t.Error("Client() not found")
	}
}

func TestNewRejectsEmptySet(t *testing.T) {
	if _, err := New(testConfig(), nil); err == nil {
		t.Error("New(empty) error = nil")
	}
}
