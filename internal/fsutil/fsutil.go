package fsutil

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

func ReplaceFile(src, dst string) error {
	err := os.Rename(src, dst)
	if err == nil || runtime.GOOS != "windows" {
		return err
	}
	if removeErr := os.Remove(dst); removeErr != nil && !os.IsNotExist(removeErr) {
		return err
	}
	return os.Rename(src, dst)
}

func WriteFileAtomic(target string, data []byte, mode os.FileMode) error {
	return copyToFile(bytes.NewReader(data), target, mode, "")
}

func CopyToFile(reader io.Reader, target string, mode os.FileMode) error {
	return copyToFile(reader, target, mode, "")
}

func CopyToFileVerified(reader io.Reader, target string, mode os.FileMode, wantSHA256 string) error {
	return copyToFile(reader, target, mode, wantSHA256)
}

func copyToFile(reader io.Reader, target string, mode os.FileMode, wantSHA256 string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), "."+filepath.Base(target)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	hasher := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, hasher), reader); err != nil {
		tmp.Close()
		return fmt.Errorf("write temporary file: %w", err)
	}
	if wantSHA256 != "" {
		got := hex.EncodeToString(hasher.Sum(nil))
		if !strings.EqualFold(got, wantSHA256) {
			tmp.Close()
			return fmt.Errorf("checksum mismatch for %s: got %s, want %s", filepath.Base(target), got, wantSHA256)
		}
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync temporary file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary file: %w", err)
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		return fmt.Errorf("chmod temporary file: %w", err)
	}
	if err := ReplaceFile(tmpName, target); err != nil {
		return fmt.Errorf("replace file: %w", err)
	}
	return nil
}

func IsWithin(base, target string) bool {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel))
}

func ArchiveTarget(base, name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("archive entry has empty name")
	}
	normalized := strings.ReplaceAll(name, `\`, "/")
	clean := path.Clean(normalized)
	if clean == "." || clean == ".." || path.IsAbs(clean) || strings.HasPrefix(clean, "../") || strings.Contains(clean, ":") || strings.ContainsRune(clean, 0) {
		return "", fmt.Errorf("archive entry %q escapes destination", name)
	}
	target := filepath.Join(base, filepath.FromSlash(clean))
	if !IsWithin(base, target) {
		return "", fmt.Errorf("archive entry %q escapes destination", name)
	}
	return target, nil
}
