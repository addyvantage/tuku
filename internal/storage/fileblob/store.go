package fileblob

import "path/filepath"

type Store struct {
	Root string
}

func NewStore(root string) *Store {
	return &Store{Root: filepath.Clean(root)}
}

// TODO(v1): add methods for large artifact blobs (worker output, diff snapshots, proof card files).
