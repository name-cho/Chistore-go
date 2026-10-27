package index

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strings"

	"github.com/name-cho/chistore-go/internal/fsutil"
	"github.com/name-cho/chistore-go/internal/metadata"
)

func Parse(data []byte) ([]string, error) {
	var ids []string
	seen := map[string]bool{}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if err := metadata.ValidateID(line); err != nil {
			return nil, fmt.Errorf("INDEX contains %w", err)
		}
		if seen[line] {
			continue
		}
		seen[line] = true
		ids = append(ids, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read INDEX: %w", err)
	}
	return ids, nil
}

func Read(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

func Write(path string, ids []string) error {
	var body strings.Builder
	for _, id := range ids {
		body.WriteString(id)
		body.WriteByte('\n')
	}
	return fsutil.WriteFileAtomic(path, []byte(body.String()), 0o644)
}
