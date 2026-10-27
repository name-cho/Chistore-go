package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/name-cho/chistore-go/internal/fsutil"
	"github.com/name-cho/chistore-go/internal/version"
)

const (
	DefaultAPIURL  = "https://api.github.com/repos/name-cho/Chistore-go/releases/latest"
	checksumsAsset = "checksums.txt"
	maxReleaseSize = 1 << 20
	maxBinarySize  = 100 << 20
	checkInterval  = 24 * time.Hour
)

var ErrDevelopmentBuild = errors.New("cannot self-update a development build (rebuild with a version or use --force)")

type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type Release struct {
	TagName string  `json:"tag_name"`
	Assets  []Asset `json:"assets"`
}

type Result struct {
	Updated bool
	From    string
	To      string
}

type Service struct {
	APIURL         string
	HTTP           *http.Client
	TargetPath     string
	GOOS           string
	GOARCH         string
	CurrentVersion string
	StateDir       string
	Force          bool
	Now            func() time.Time
}

func New(currentVersion string) (*Service, error) {
	target, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("find executable: %w", err)
	}
	return &Service{
		APIURL:         DefaultAPIURL,
		HTTP:           &http.Client{Timeout: 2 * time.Minute},
		TargetPath:     target,
		GOOS:           runtime.GOOS,
		GOARCH:         runtime.GOARCH,
		CurrentVersion: currentVersion,
		Now:            time.Now,
	}, nil
}

func AssetName(goos, goarch string) string {
	name := "chistore_" + goos + "_" + goarch
	if goos == "windows" {
		name += ".exe"
	}
	return name
}

func (s *Service) Update(ctx context.Context) (Result, error) {
	if s.CurrentVersion == "" || s.CurrentVersion == "dev" {
		if !s.Force {
			return Result{}, ErrDevelopmentBuild
		}
	}
	if s.TargetPath == "" {
		return Result{}, fmt.Errorf("executable path is empty")
	}

	release, err := s.latestRelease(ctx)
	if err != nil {
		return Result{}, err
	}
	latest := normalizeVersion(release.TagName)
	result := Result{From: s.CurrentVersion, To: latest}
	if version.Compare(latest, s.CurrentVersion) <= 0 {
		return result, nil
	}

	name := AssetName(s.goos(), s.goarch())
	asset, ok := findAsset(release.Assets, name)
	if !ok {
		return Result{}, fmt.Errorf("release %s has no asset %s", release.TagName, name)
	}
	checksums, err := s.fetchChecksums(ctx, release.Assets)
	if err != nil {
		return Result{}, err
	}
	if err := s.downloadAndReplace(ctx, asset, checksums); err != nil {
		return Result{}, err
	}
	result.Updated = true
	return result, nil
}

func (s *Service) MaybeUpdate(ctx context.Context) (Result, error) {
	if s.CurrentVersion == "" || s.CurrentVersion == "dev" || s.StateDir == "" {
		return Result{}, nil
	}
	if !s.checkDue() {
		return Result{}, nil
	}
	if err := s.markChecked(); err != nil {
		return Result{}, err
	}
	return s.Update(ctx)
}

func (s *Service) latestRelease(ctx context.Context) (Release, error) {
	var release Release
	if err := s.getJSON(ctx, s.APIURL, &release); err != nil {
		return Release{}, fmt.Errorf("check latest release: %w", err)
	}
	if release.TagName == "" {
		return Release{}, fmt.Errorf("latest release has no tag")
	}
	return release, nil
}

func (s *Service) getJSON(ctx context.Context, url string, target any) error {
	body, err := s.getBytes(ctx, url, maxReleaseSize, false)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("decode %s: %w", url, err)
	}
	return nil
}

func (s *Service) fetchChecksums(ctx context.Context, assets []Asset) (map[string]string, error) {
	asset, ok := findAsset(assets, checksumsAsset)
	if !ok {
		return nil, nil
	}
	body, err := s.getBytes(ctx, asset.BrowserDownloadURL, maxReleaseSize, true)
	if err != nil {
		return nil, fmt.Errorf("download checksums: %w", err)
	}
	if body == nil {
		return nil, nil
	}
	return ParseChecksums(body), nil
}

func (s *Service) downloadAndReplace(ctx context.Context, asset Asset, checksums map[string]string) error {
	resp, err := s.get(ctx, asset.BrowserDownloadURL)
	if err != nil {
		return fmt.Errorf("download %s: %w", asset.Name, err)
	}
	defer resp.Body.Close()

	tmp, err := os.CreateTemp(filepath.Dir(s.TargetPath), ".chistore-update-*")
	if err != nil {
		return fmt.Errorf("create update file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	hasher := sha256.New()
	limited := io.LimitReader(resp.Body, maxBinarySize+1)
	written, err := io.Copy(io.MultiWriter(tmp, hasher), limited)
	if err != nil {
		tmp.Close()
		return fmt.Errorf("download %s: %w", asset.Name, err)
	}
	if written > maxBinarySize {
		tmp.Close()
		return fmt.Errorf("%s is larger than %d bytes", asset.Name, maxBinarySize)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync update file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close update file: %w", err)
	}

	if err := verifyHash(hex.EncodeToString(hasher.Sum(nil)), asset.Name, checksums); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o755); err != nil {
		return fmt.Errorf("chmod update file: %w", err)
	}
	if err := replaceExecutable(tmpName, s.TargetPath); err != nil {
		return fmt.Errorf("replace executable: %w", err)
	}
	return nil
}

func (s *Service) getBytes(ctx context.Context, url string, limit int64, allowNotFound bool) ([]byte, error) {
	resp, err := s.get(ctx, url)
	if err != nil {
		var httpErr *HTTPError
		if allowNotFound && errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusNotFound {
			return nil, nil
		}
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s is larger than %d bytes", url, limit)
	}
	return data, nil
}

func (s *Service) get(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "chistore/2 (+https://github.com/name-cho/Chistore-go)")
	client := s.HTTP
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Minute}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusOK {
		return resp, nil
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	resp.Body.Close()
	return nil, &HTTPError{URL: url, StatusCode: resp.StatusCode, Status: resp.Status}
}

func (s *Service) checkDue() bool {
	data, err := os.ReadFile(filepath.Join(s.StateDir, "update-check"))
	if err != nil {
		return true
	}
	last, err := time.Parse(time.RFC3339, strings.TrimSpace(string(data)))
	if err != nil {
		return true
	}
	return !s.now().Before(last.Add(checkInterval))
}

func (s *Service) markChecked() error {
	path := filepath.Join(s.StateDir, "update-check")
	return fsutil.WriteFileAtomic(path, []byte(s.now().UTC().Format(time.RFC3339)+"\n"), 0o600)
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) goos() string {
	if s.GOOS != "" {
		return s.GOOS
	}
	return runtime.GOOS
}

func (s *Service) goarch() string {
	if s.GOARCH != "" {
		return s.GOARCH
	}
	return runtime.GOARCH
}

func findAsset(assets []Asset, name string) (Asset, bool) {
	for _, asset := range assets {
		if asset.Name == name {
			return asset, true
		}
	}
	return Asset{}, false
}

func normalizeVersion(tag string) string {
	tag = strings.TrimSpace(tag)
	if len(tag) > 1 && (tag[0] == 'v' || tag[0] == 'V') && tag[1] >= '0' && tag[1] <= '9' {
		return tag[1:]
	}
	return tag
}

func ParseChecksums(data []byte) map[string]string {
	sums := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 || len(fields[0]) != sha256.Size*2 {
			continue
		}
		sums[strings.TrimPrefix(fields[1], "*")] = strings.ToLower(fields[0])
	}
	return sums
}

func VerifyChecksum(data []byte, name string, sums map[string]string) error {
	sum := sha256.Sum256(data)
	return verifyHash(hex.EncodeToString(sum[:]), name, sums)
}

func verifyHash(actual, name string, sums map[string]string) error {
	expected, ok := sums[name]
	if !ok || expected == "" {
		return nil
	}
	if !strings.EqualFold(actual, expected) {
		return fmt.Errorf("checksum mismatch for %s", name)
	}
	return nil
}

func replaceExecutable(newPath, target string) error {
	if err := fsutil.ReplaceFile(newPath, target); err == nil {
		return nil
	} else if runtime.GOOS != "windows" {
		return err
	}

	backup := target + ".old"
	_ = os.Remove(backup)
	if err := os.Rename(target, backup); err != nil {
		return err
	}
	if err := os.Rename(newPath, target); err != nil {
		_ = os.Rename(backup, target)
		return err
	}
	_ = os.Remove(backup)
	return nil
}

type HTTPError struct {
	URL        string
	StatusCode int
	Status     string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("GET %s: %s", e.URL, e.Status)
}
