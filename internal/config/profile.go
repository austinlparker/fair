package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"
)

// ResolveProfile picks the active profile name from the explicit flag value,
// the FAIR_PROFILE environment variable, and the known-profiles allowlist
// (typically discovered by listing config.<name>.toml files).
//
// Resolution order: flag > env > error. The chosen name must appear in known.
// On a miss with a Levenshtein distance <= 2 to a known profile, the error
// includes a "did you mean" suggestion.
func ResolveProfile(flag, env string, known []string) (string, error) {
	if len(known) == 0 {
		return "", fmt.Errorf("no profiles configured (expected config.<name>.toml in config dir)")
	}

	chosen := flag
	if chosen == "" {
		chosen = env
	}
	if chosen == "" {
		return "", fmt.Errorf("profile required: pass --profile <name> or set FAIR_PROFILE (known: %s)", strings.Join(known, ", "))
	}

	if slices.Contains(known, chosen) {
		return chosen, nil
	}

	// Unknown — try to suggest a close match.
	suggestion := closestMatch(chosen, known, 2)
	if suggestion != "" {
		return "", fmt.Errorf("unknown profile %q; did you mean %q? (known: %s)", chosen, suggestion, strings.Join(known, ", "))
	}
	return "", fmt.Errorf("unknown profile %q (known: %s)", chosen, strings.Join(known, ", "))
}

// DiscoverProfiles returns the profile names found in dir, by scanning for
// files named "config.<name>.toml". A non-existent dir is not an error;
// it returns an empty slice. Returned names are sorted (callers should not
// rely on this for correctness, but it stabilizes error messages).
func DiscoverProfiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read config dir %s: %w", dir, err)
	}
	const prefix = "config."
	const suffix = ".toml"
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if !strings.HasPrefix(n, prefix) || !strings.HasSuffix(n, suffix) {
			continue
		}
		// Reject "config.toml" (no profile name) — the prefix and suffix
		// would overlap, so the file is too short to contain a profile.
		if len(n) <= len(prefix)+len(suffix) {
			continue
		}
		profile := n[len(prefix) : len(n)-len(suffix)]
		if profile == "" {
			continue
		}
		names = append(names, profile)
	}
	slices.Sort(names)
	return names, nil
}

// closestMatch returns the candidate with the smallest Levenshtein distance
// to s, provided that distance is at most maxDistance. Empty string means
// no candidate qualifies.
func closestMatch(s string, candidates []string, maxDistance int) string {
	best := ""
	bestDist := maxDistance + 1
	for _, c := range candidates {
		d := levenshtein(s, c)
		if d < bestDist {
			bestDist = d
			best = c
		}
	}
	if bestDist > maxDistance {
		return ""
	}
	return best
}

// levenshtein computes the edit distance between a and b using the standard
// dynamic-programming recurrence. Operates on runes so non-ASCII profile
// names (rare but possible) compare by character, not byte.
func levenshtein(a, b string) int {
	ra := []rune(a)
	rb := []rune(b)
	if len(ra) == 0 {
		return len(rb)
	}
	if len(rb) == 0 {
		return len(ra)
	}

	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(
				prev[j]+1,      // deletion
				cur[j-1]+1,     // insertion
				prev[j-1]+cost, // substitution
			)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}
