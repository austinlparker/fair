package publish

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// StateEntry is the per-rkey idempotency cache. LastHash short-circuits
// re-publishing identical content; ID is the post's frontmatter ULID.
type StateEntry struct {
	ID              string    `json:"id"`
	LastHash        string    `json:"last_hash"`
	LastPublishedAt time.Time `json:"last_published_at"`
}

// State is the on-disk publish-flow cache, indexed by the TID rkey. It is
// only a cache: with no entry for a post, Publish compares against the
// record on the PDS instead, so the file is safe to delete and doesn't
// need to travel between machines.
type State struct {
	Entries map[string]StateEntry `json:"entries"`
}

// LoadState reads state JSON from path. A missing file returns an empty,
// non-nil State; the publish flow treats "no state file yet" as "first
// publish ever". Corrupt JSON is an error; deleting the file is safe
// (the next publish of each post re-puts it and rebuilds the entry).
func LoadState(path string) (*State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &State{Entries: map[string]StateEntry{}}, nil
		}
		return nil, fmt.Errorf("read state %s: %w", path, err)
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse state %s: %w", path, err)
	}
	if s.Entries == nil {
		s.Entries = map[string]StateEntry{}
	}
	return &s, nil
}

// Save writes state JSON to path atomically (temp + rename) at 0600.
// The temp file is created in the same directory as path so the rename
// is atomic on the same filesystem.
func (s *State) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("ensure state dir: %w", err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("chmod temp: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp: %w", err)
	}
	return os.Rename(tmpName, path)
}
