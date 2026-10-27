package repo

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := New(server.URL+"/", server.Client())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return client
}

func TestIndex(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/INDEX" {
			t.Errorf("path = %q, want /INDEX", r.URL.Path)
		}
		_, _ = w.Write([]byte("# packages\n\nenvsnap\n  netdiag  \nenvsnap\n"))
	})

	ids, err := client.Index(context.Background())
	if err != nil {
		t.Fatalf("Index() error = %v", err)
	}
	if strings.Join(ids, ",") != "envsnap,netdiag" {
		t.Errorf("Index() = %v", ids)
	}
}

func TestIndexRejectsUnsafeID(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("good\n../evil\n"))
	})
	if _, err := client.Index(context.Background()); err == nil {
		t.Fatal("Index() error = nil, want unsafe id error")
	}
}

func TestInfo(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/demo/info.md" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		_, _ = w.Write([]byte("## Name: Demo\n## Version: 1.2.3\n## Type: python\n"))
	})

	pkg, err := client.Info(context.Background(), "demo")
	if err != nil {
		t.Fatalf("Info() error = %v", err)
	}
	if pkg.ID != "demo" || pkg.Name != "Demo" || pkg.Version != "1.2.3" || pkg.MainFile != "demo" {
		t.Errorf("Info() = %+v", pkg)
	}
}

func TestHTTPErrorIsNotFound(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	_, err := client.Info(context.Background(), "missing")
	if err == nil {
		t.Fatal("Info() error = nil")
	}
	if !IsNotFound(err) {
		t.Errorf("IsNotFound(%v) = false", err)
	}
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusNotFound {
		t.Errorf("error = %#v, want HTTPError 404", err)
	}
}

func TestFetchTo(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/demo/demo" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		_, _ = w.Write([]byte("#!/bin/sh\necho demo\n"))
	})

	dest := filepath.Join(t.TempDir(), "demo")
	if err := client.FetchTo(context.Background(), "demo", "demo", dest); err != nil {
		t.Fatalf("FetchTo() error = %v", err)
	}
	data, err := os.ReadFile(dest)
	if err != nil || !strings.Contains(string(data), "echo demo") {
		t.Fatalf("downloaded file = %q, %v", data, err)
	}
}

func TestPackagesPrefersManifest(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/index.json" {
			t.Fatalf("path = %q, want /index.json", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"schema":1,"packages":[{"id":"demo","version":"1.0.0","type":"single","main":"demo","sha256":"` + strings.Repeat("a", 64) + `"}]}`))
	})

	packages, err := client.Packages(context.Background())
	if err != nil {
		t.Fatalf("Packages() error = %v", err)
	}
	if len(packages) != 1 || packages[0].ID != "demo" || packages[0].SHA256 == "" {
		t.Errorf("packages = %+v", packages)
	}
}

func TestPackagesFallsBackToIndex(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/index.json":
			http.NotFound(w, r)
		case "/INDEX":
			_, _ = w.Write([]byte("demo\n"))
		case "/demo/info.md":
			_, _ = w.Write([]byte("## Name: Demo\n## Version: 2.0.0\n"))
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	})

	packages, err := client.Packages(context.Background())
	if err != nil {
		t.Fatalf("Packages() error = %v", err)
	}
	if len(packages) != 1 || packages[0].Version != "2.0.0" {
		t.Errorf("packages = %+v", packages)
	}
}

func TestFetchToRemovesPartialFileOnError(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})

	dest := filepath.Join(t.TempDir(), "demo")
	if err := client.FetchTo(context.Background(), "demo", "demo", dest); err == nil {
		t.Fatal("FetchTo() error = nil")
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Errorf("partial file exists: %v", err)
	}
}

func TestOpenMainFallsBackToPythonFile(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/demo/demo":
			http.NotFound(w, r)
		case "/demo/demo.py":
			_, _ = w.Write([]byte("print('demo')\n"))
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	})

	body, used, err := client.OpenMain(context.Background(), "demo", "demo")
	if err != nil {
		t.Fatalf("OpenMain() error = %v", err)
	}
	defer body.Close()
	if used != "demo.py" {
		t.Errorf("used = %q, want demo.py", used)
	}
}

func TestRejectsUnsafePackageIDWithoutRequest(t *testing.T) {
	requests := 0
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.NotFound(w, r)
	})

	if _, err := client.Info(context.Background(), "../evil"); err == nil {
		t.Fatal("Info() error = nil")
	}
	if requests != 0 {
		t.Errorf("server received %d requests, want 0", requests)
	}
}

func TestNewRejectsInvalidBaseURL(t *testing.T) {
	for _, base := range []string{"", "ftp://example.com", "://bad"} {
		if _, err := New(base, nil); err == nil {
			t.Errorf("New(%q) error = nil", base)
		}
	}
}
