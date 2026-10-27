package repo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/name-cho/chistore-go/internal/fsutil"
	"github.com/name-cho/chistore-go/internal/index"
	"github.com/name-cho/chistore-go/internal/metadata"
)

const (
	maxMetadataSize = 4 << 20
	userAgent       = "chistore/2 (+https://github.com/name-cho/Chistore-go)"
)

type Client struct {
	baseURL *url.URL
	http    *http.Client
}

func New(baseURL string, httpClient *http.Client) (*Client, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return nil, fmt.Errorf("parse repository URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("repository URL must use http or https")
	}
	if parsed.Host == "" {
		return nil, fmt.Errorf("repository URL has no host")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawQuery = ""
	parsed.Fragment = ""
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 5 * time.Minute}
	}
	return &Client{baseURL: parsed, http: httpClient}, nil
}

func (c *Client) BaseURL() string {
	return strings.TrimRight(c.baseURL.String(), "/")
}

func (c *Client) Index(ctx context.Context) ([]string, error) {
	data, err := c.fetchBytes(ctx, "INDEX", maxMetadataSize)
	if err != nil {
		return nil, err
	}
	return index.Parse(data)
}

func (c *Client) Packages(ctx context.Context) ([]metadata.Package, error) {
	data, err := c.fetchBytes(ctx, index.ManifestFile, maxMetadataSize)
	if err == nil {
		manifest, parseErr := index.ParseManifest(data)
		if parseErr != nil {
			return nil, parseErr
		}
		return manifest.Packages, nil
	}
	if !IsNotFound(err) {
		return nil, err
	}

	// Repositories without index.json are served through the legacy INDEX and info.md files.
	ids, err := c.Index(ctx)
	if err != nil {
		return nil, err
	}
	packages := make([]metadata.Package, 0, len(ids))
	for _, id := range ids {
		pkg, err := c.Info(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("read %s metadata: %w", id, err)
		}
		packages = append(packages, pkg)
	}
	return packages, nil
}

func (c *Client) Info(ctx context.Context, id string) (metadata.Package, error) {
	if err := metadata.ValidateID(id); err != nil {
		return metadata.Package{}, err
	}
	data, err := c.fetchBytes(ctx, id+"/info.md", maxMetadataSize)
	if err != nil {
		return metadata.Package{}, err
	}
	return metadata.Parse(id, bytes.NewReader(data))
}

func (c *Client) OpenMain(ctx context.Context, id, mainFile string) (io.ReadCloser, string, error) {
	if err := metadata.ValidateID(id); err != nil {
		return nil, "", err
	}
	if err := metadata.ValidateMainFile(mainFile); err != nil {
		return nil, "", err
	}

	resp, err := c.get(ctx, c.urlFor(append([]string{id}, strings.Split(mainFile, "/")...)...))
	if err == nil {
		return resp.Body, mainFile, nil
	}
	if !IsNotFound(err) || strings.HasSuffix(mainFile, ".py") {
		return nil, "", err
	}

	fallback := mainFile + ".py"
	resp, err = c.get(ctx, c.urlFor(append([]string{id}, strings.Split(fallback, "/")...)...))
	if err != nil {
		return nil, "", err
	}
	return resp.Body, fallback, nil
}

func (c *Client) Open(ctx context.Context, id, file string) (io.ReadCloser, error) {
	if err := metadata.ValidateID(id); err != nil {
		return nil, err
	}
	if err := metadata.ValidateMainFile(file); err != nil {
		return nil, err
	}
	resp, err := c.get(ctx, c.urlFor(append([]string{id}, strings.Split(file, "/")...)...))
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

func (c *Client) FetchTo(ctx context.Context, id, file, dest string) error {
	body, err := c.Open(ctx, id, file)
	if err != nil {
		return err
	}
	defer body.Close()

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("create download directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), "."+filepath.Base(dest)+".part-*")
	if err != nil {
		return fmt.Errorf("create temporary download: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := io.Copy(tmp, body); err != nil {
		tmp.Close()
		return fmt.Errorf("download %s: %w", file, err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync download: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close download: %w", err)
	}
	if err := fsutil.ReplaceFile(tmpName, dest); err != nil {
		return fmt.Errorf("store download: %w", err)
	}
	return nil
}

func (c *Client) fetchBytes(ctx context.Context, relative string, limit int64) ([]byte, error) {
	resp, err := c.get(ctx, c.urlFor(relative))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", relative, err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s is larger than %d bytes", relative, limit)
	}
	return data, nil
}

func (c *Client) get(ctx context.Context, rawURL string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", rawURL, err)
	}
	if resp.StatusCode == http.StatusOK {
		return resp, nil
	}

	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	resp.Body.Close()
	return nil, &HTTPError{URL: rawURL, StatusCode: resp.StatusCode, Status: resp.Status}
}

func (c *Client) urlFor(parts ...string) string {
	u := *c.baseURL
	return u.JoinPath(parts...).String()
}

type HTTPError struct {
	URL        string
	StatusCode int
	Status     string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("GET %s: %s", e.URL, e.Status)
}

func IsNotFound(err error) bool {
	var httpErr *HTTPError
	return errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusNotFound
}
