package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if len(store.List()) != 0 {
		t.Fatalf("new store is not empty: %+v", store.List())
	}

	entries := []Package{
		{ID: "zeta", Version: "1.0", Binary: "/bin/zeta", InstalledAt: "2026-01-02"},
		{ID: "alpha", Version: "2.0", Binary: "/bin/alpha", ConfigDir: "/conf/alpha", DataDir: "/data/alpha", InstalledAt: "2026-01-01"},
	}
	for _, entry := range entries {
		if err := store.Put(entry); err != nil {
			t.Fatalf("Put(%s) error = %v", entry.ID, err)
		}
	}

	reopened, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen error = %v", err)
	}
	list := reopened.List()
	if len(list) != 2 || list[0].ID != "alpha" || list[1].ID != "zeta" {
		t.Fatalf("List() = %+v, want alpha then zeta", list)
	}
	got, ok := reopened.Get("alpha")
	if !ok || got.Version != "2.0" || got.DataDir != "/data/alpha" {
		t.Fatalf("Get(alpha) = %+v, %v", got, ok)
	}
}

func TestDelete(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := store.Put(Package{ID: "demo", Version: "1"}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if err := store.Delete("demo"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, ok := store.Get("demo"); ok {
		t.Fatal("demo still present")
	}
	if err := store.Delete("missing"); err != nil {
		t.Fatalf("Delete(missing) error = %v, want nil", err)
	}
}

func TestMigratesLegacyManifest(t *testing.T) {
	dir := t.TempDir()
	manifest := "envsnap|1.0.1|/home/cho/.local/bin/envsnap|/home/cho/.config/envsnap|/home/cho/.local/share/envsnap|2026-01-01\n" +
		"pystat|1.0.0|/home/cho/.local/bin/pystat|/home/cho/.config/pystat|/home/cho/.local/share/pystat|2026-01-02\n" +
		"broken line\n"
	if err := os.WriteFile(filepath.Join(dir, "manifest"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}

	store, err := Open(dir)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	list := store.List()
	if len(list) != 2 || list[0].ID != "envsnap" || list[1].Version != "1.0.0" {
		t.Fatalf("migrated list = %+v", list)
	}
	if _, err := os.Stat(filepath.Join(dir, "state.json")); err != nil {
		t.Errorf("state.json not created: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "manifest.migrated")); err != nil {
		t.Errorf("manifest not renamed: %v", err)
	}
}

func TestOpenRejectsCorruptState(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir); err == nil {
		t.Fatal("Open() error = nil, want corrupt state error")
	}
}

func TestPutPreservesOtherEntries(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	for _, id := range []string{"a", "b", "c"} {
		if err := store.Put(Package{ID: id, Version: "1"}); err != nil {
			t.Fatalf("Put(%s) error = %v", id, err)
		}
	}
	if err := store.Put(Package{ID: "b", Version: "2"}); err != nil {
		t.Fatalf("Put(b) error = %v", err)
	}
	if got, _ := store.Get("b"); got.Version != "2" {
		t.Errorf("b version = %q, want 2", got.Version)
	}
	if len(store.List()) != 3 {
		t.Errorf("List() length = %d, want 3", len(store.List()))
	}
}
