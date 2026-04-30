// Package build renders standard.site PDS records into a static HTML
// site under dist/. The orchestrator (Build in build.go) reads the
// publication + document records, renders each, writes them to dist.tmp/,
// and atomically promotes dist.tmp/ to dist/.
package build

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// distOldPrefix is the per-promote rename of the existing dist/.
	// SweepStaleDistOld removes any leftover from prior crashes.
	distOldPrefix = "dist.old."
)

// AtomicPromote replaces target with tmpOut atomically:
//  1. mv target -> target.dist.old.<pid> (skip if target absent)
//  2. mv tmpOut -> target
//  3. rm -rf the old dir (best-effort; not fatal)
//
// On a same-filesystem layout, step 2 is the actual atomic operation;
// observers see either the old or new tree, never a half-move. Step 1
// is also a rename, also atomic, so the only window where target may
// be absent is between (1) and (2) — typically microseconds.
//
// tmpOut must exist and be a directory. target's parent must exist.
func AtomicPromote(tmpOut, target string) error {
	if _, err := os.Stat(tmpOut); err != nil {
		return fmt.Errorf("tmpOut %s: %w", tmpOut, err)
	}

	parent := filepath.Dir(target)
	old := filepath.Join(parent, distOldPrefix+pidString())

	// Step 1: rename target aside if it exists.
	targetExisted := false
	if _, err := os.Stat(target); err == nil {
		if err := os.Rename(target, old); err != nil {
			return fmt.Errorf("rename %s -> %s: %w", target, old, err)
		}
		targetExisted = true
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("stat %s: %w", target, err)
	}

	// Step 2: rename tmpOut -> target. The atomic step.
	if err := os.Rename(tmpOut, target); err != nil {
		// Try to put the old dir back so we don't leave a missing dist.
		if targetExisted {
			_ = os.Rename(old, target)
		}
		return fmt.Errorf("rename %s -> %s: %w", tmpOut, target, err)
	}

	// Step 3: best-effort cleanup of the old tree.
	if targetExisted {
		_ = os.RemoveAll(old)
	}
	return nil
}

// SweepStaleDistOld removes any dist.old.* directories under parent.
// Called at the start of Build so a prior crash mid-promote doesn't
// leave stale snapshots behind.
func SweepStaleDistOld(parent string) error {
	entries, err := os.ReadDir(parent)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if !strings.HasPrefix(e.Name(), distOldPrefix) {
			continue
		}
		if err := os.RemoveAll(filepath.Join(parent, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

// pidString builds a tag for the dist.old directory name. PID + nanos
// are unique enough to handle two concurrent builds (which we don't
// support but want to fail gracefully on).
func pidString() string {
	return fmt.Sprintf("%d.%d", os.Getpid(), time.Now().UnixNano())
}
