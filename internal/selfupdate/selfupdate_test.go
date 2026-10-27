package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestAssetName(t *testing.T) {
	if got := AssetName("linux", "amd64"); got != "chistore_linux_amd64" {
		t.Errorf("AssetName(linux, amd64) = %q", got)
	}
	if got := AssetName("windows", "arm64"); got != "chistore_windows_arm64.exe" {
		t.Errorf("AssetName(windows, arm64) = %q", got)
	}
}

func TestParseChecksums(t *testing.T) {
	hash := strings.Repeat("a", 64)
	data := []byte("# sums\n" + strings.ToUpper(hash) + "  chistore_linux_amd64\n\nbadline\n")
	sums := ParseChecksums(data)
	if sums["chistore_linux_amd64"] != hash {
		t.Errorf("sums = %v", sums)
	}
}

func TestUpdateReplacesTarget(t *testing.T) {
	newBinary := []byte("#!/bin/sh\necho v2\n")
	sum := sha256.Sum256(newBinary)
	server, requests := updateServer(t, newBinary, hex.EncodeToString(sum[:]))
	target := writeTarget(t, "old")

	service := testService(server, target)
	result, err := service.Update(context.Background())
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if !result.Updated || result.From != "1.0.0" || result.To != "2.0.0" {
		t.Errorf("result = %+v", result)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != string(newBinary) {
		t.Fatalf("target = %q, %v", data, err)
	}
	if *requests < 3 {
		t.Errorf("requests = %d, want release+checksums+binary", *requests)
	}
}

func TestUpdateRejectsChecksumMismatch(t *testing.T) {
	sum := sha256.Sum256([]byte("different"))
	server, _ := updateServer(t, []byte("new"), hex.EncodeToString(sum[:]))
	target := writeTarget(t, "old")

	_, err := testService(server, target).Update(context.Background())
	if err == nil {
		t.Fatal("Update() error = nil")
	}
	data, _ := os.ReadFile(target)
	if string(data) != "old" {
		t.Errorf("target changed despite checksum error: %q", data)
	}
}

func TestUpdateSkipsSameOrOlderVersion(t *testing.T) {
	sum := sha256.Sum256([]byte("new"))
	server, requests := updateServer(t, []byte("new"), hex.EncodeToString(sum[:]))
	for _, current := range []string{"2.0.0", "3.0.0"} {
		target := writeTarget(t, "old")
		service := testService(server, target)
		service.CurrentVersion = current
		result, err := service.Update(context.Background())
		if err != nil {
			t.Fatalf("Update(%s) error = %v", current, err)
		}
		if result.Updated {
			t.Errorf("Update(%s) updated to %s", current, result.To)
		}
		data, _ := os.ReadFile(target)
		if string(data) != "old" {
			t.Errorf("target changed: %q", data)
		}
	}
	if *requests != 2 {
		t.Errorf("requests = %d, want only release metadata", *requests)
	}
}

func TestUpdateRejectsDevelopmentBuild(t *testing.T) {
	service := &Service{CurrentVersion: "dev", TargetPath: "x"}
	if _, err := service.Update(context.Background()); err != ErrDevelopmentBuild {
		t.Fatalf("Update() error = %v, want ErrDevelopmentBuild", err)
	}
}

func TestMaybeUpdateRespectsInterval(t *testing.T) {
	dir := t.TempDir()
	server, requests := updateServer(t, []byte("new"), "")
	service := testService(server, writeTarget(t, "old"))
	service.StateDir = dir
	service.Now = func() time.Time { return time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC) }

	if err := service.markChecked(); err != nil {
		t.Fatal(err)
	}
	result, err := service.MaybeUpdate(context.Background())
	if err != nil {
		t.Fatalf("MaybeUpdate() error = %v", err)
	}
	if result.Updated || *requests != 0 {
		t.Errorf("checked recently but requests = %d, result = %+v", *requests, result)
	}

	service.Now = func() time.Time { return time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC) }
	result, err = service.MaybeUpdate(context.Background())
	if err != nil {
		t.Fatalf("MaybeUpdate() after interval error = %v", err)
	}
	if !result.Updated {
		t.Errorf("result = %+v, want update", result)
	}
}

func testService(server *httptest.Server, target string) *Service {
	return &Service{
		APIURL:         server.URL + "/latest",
		HTTP:           server.Client(),
		TargetPath:     target,
		GOOS:           "linux",
		GOARCH:         "amd64",
		CurrentVersion: "1.0.0",
		Now:            time.Now,
	}
}

func updateServer(t *testing.T, binary []byte, checksum string) (*httptest.Server, *int) {
	t.Helper()
	requests := 0
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		switch r.URL.Path {
		case "/latest":
			checksums := ""
			if checksum != "" {
				checksums = fmt.Sprintf(`,{"name":"checksums.txt","browser_download_url":"%s/checksums"}`, server.URL)
			}
			fmt.Fprintf(w, `{"tag_name":"v2.0.0","assets":[{"name":"chistore_linux_amd64","browser_download_url":"%s/binary"}%s]}`,
				server.URL, checksums)
		case "/checksums":
			fmt.Fprintf(w, "%s  chistore_linux_amd64\n", checksum)
		case "/binary":
			_, _ = w.Write(binary)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server, &requests
}

func writeTarget(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "chistore")
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestChecksumVerification(t *testing.T) {
	sum := sha256.Sum256([]byte("data"))
	sums := map[string]string{"file": strings.ToLower(hex.EncodeToString(sum[:]))}
	if err := VerifyChecksum([]byte("data"), "file", sums); err != nil {
		t.Errorf("VerifyChecksum() error = %v", err)
	}
	if err := VerifyChecksum([]byte("other"), "file", sums); err == nil {
		t.Error("VerifyChecksum() error = nil for wrong data")
	}
	if err := VerifyChecksum([]byte("data"), "unknown", sums); err != nil {
		t.Errorf("VerifyChecksum(unknown) error = %v", err)
	}
}

func TestUpdateMissingAsset(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"tag_name":"v2.0.0","assets":[]}`)
	}))
	defer server.Close()

	service := testService(server, writeTarget(t, "old"))
	if _, err := service.Update(context.Background()); err == nil {
		t.Fatal("Update() error = nil")
	}
}

func TestAssetNameMatchesRuntimeForTestGOOS(t *testing.T) {
	name := AssetName(runtime.GOOS, runtime.GOARCH)
	if !strings.HasPrefix(name, "chistore_") {
		t.Errorf("AssetName() = %q", name)
	}
}
