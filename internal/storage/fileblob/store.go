package fileblob

import (
	"os"
	"path/filepath"
	"strings"
)

type Store struct {
	Root string
}

func NewStore(root string) *Store {
	return &Store{Root: filepath.Clean(root)}
}

func (s *Store) WriteText(parts []string, body string) (string, error) {
	root := strings.TrimSpace(s.Root)
	if root == "" {
		root = "."
	}
	pathParts := make([]string, 0, len(parts)+1)
	pathParts = append(pathParts, root)
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		pathParts = append(pathParts, filepath.Clean(part))
	}
	fullPath := filepath.Join(pathParts...)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(fullPath, []byte(body), 0o644); err != nil {
		return "", err
	}
	return fullPath, nil
}
