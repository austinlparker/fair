package atproto

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// ErrSessionNotFound is returned by TokenStore.Load when no session
// exists for the given profile. Use errors.Is to check.
var ErrSessionNotFound = errors.New("session not found")

// TokenStore persists Session values keyed by profile name. Implementations
// must isolate profiles — saving "sandbox" must never affect "prod".
type TokenStore interface {
	Save(profile string, s Session) error
	Load(profile string) (Session, error)
	Delete(profile string) error
}

// FileStore stores sessions as JSON files at <dir>/session.<profile>.json
// with 0600 perms. It's the always-available fallback when no OS keychain
// backend is reachable, and the implementation tests exercise.
type FileStore struct {
	dir string
}

// NewFileStore returns a FileStore writing to dir. The directory is
// created lazily on the first Save; callers don't need to mkdir it.
func NewFileStore(dir string) *FileStore {
	return &FileStore{dir: dir}
}

func (s *FileStore) path(profile string) string {
	return filepath.Join(s.dir, "session."+profile+".json")
}

// Save writes session JSON atomically: marshal -> tmpfile -> rename.
// The temp file lives in the same directory as the target so rename is
// guaranteed to be atomic on the same filesystem.
func (s *FileStore) Save(profile string, sess Session) error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("ensure store dir: %w", err)
	}
	data, err := json.MarshalIndent(sess, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal session: %w", err)
	}

	target := s.path(profile)
	tmp, err := os.CreateTemp(s.dir, "session."+profile+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmp.Name()
	// Best-effort cleanup if anything below fails; rename will replace it.
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
	if err := os.Rename(tmpName, target); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

// Load returns the persisted session for profile, or ErrSessionNotFound.
func (s *FileStore) Load(profile string) (Session, error) {
	data, err := os.ReadFile(s.path(profile))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Session{}, ErrSessionNotFound
		}
		return Session{}, fmt.Errorf("read session: %w", err)
	}
	var sess Session
	if err := json.Unmarshal(data, &sess); err != nil {
		return Session{}, fmt.Errorf("parse session %s: %w", s.path(profile), err)
	}
	return sess, nil
}

// Delete removes the session for profile. Missing files are not an error.
func (s *FileStore) Delete(profile string) error {
	err := os.Remove(s.path(profile))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove session: %w", err)
	}
	return nil
}
