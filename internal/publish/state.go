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
// re-publishing identical content; LastCoverHash decides whether the
// cover blob needs re-uploading; ID anchors rename detection (slug
// changes but ID stays).
type StateEntry struct {
	ID              string    `json:"id"`
	LastHash        string    `json:"last_hash"`
	LastCoverHash   string    `json:"last_cover_hash,omitempty"`
	LastPublishedAt time.Time `json:"last_published_at"`
}

// State is the on-disk publish-flow cache. Indexed by rkey (slug);
// callers needing reverse lookup by ID use RkeyByID.
type State struct {
	Entries map[string]StateEntry `json:"entries"`
}

// LoadState reads state JSON from path. A missing file returns an empty,
// non-nil State; the publish flow treats "no state file yet" as "first
// publish ever". Corrupt JSON is an error — the user should run `fair
// doctor` to rebuild.
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

// RkeyByID returns the rkey whose StateEntry has the given ID, or false
// when not found. Used by the publish flow's local-first rename
// detection: if the source markdown has id X and the on-disk state maps
// rkey Y to id X, but the slug derived from the file path is Z, that's
// a rename (Y -> Z).
func (s *State) RkeyByID(id string) (string, bool) {
	for rkey, entry := range s.Entries {
		if entry.ID == id {
			return rkey, true
		}
	}
	return "", false
}
