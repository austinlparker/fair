package markdown

import (
	"crypto/sha256"
	"encoding/hex"
)

// HashInput is the bundle of normalized inputs that drive content-hash
// idempotency for `fair publish`. All fields except Frontmatter are pre-
// normalized by the caller; Hash applies the same canonical-frontmatter
// transform to make the hash insensitive to id/updated/tag-order/case
// changes.
type HashInput struct {
	// NormalizedBody is the markdown body after Normalize() AND inline
	// image-path rewriting. Two equivalent posts must arrive here as
	// byte-identical strings or the hashes will differ.
	NormalizedBody string

	// Frontmatter is the typed YAML metadata. Hash uses Canonical(fm)
	// internally — id/updated are excluded, tags are normalized.
	Frontmatter Frontmatter

	// CoverSourceHash is the SHA-256 (hex) of the local cover-image file
	// bytes, computed before upload. Empty string means "no cover".
	// Hashing the source file (not the PDS blob CID) means the publish
	// flow can short-circuit before any network round-trip.
	CoverSourceHash string
}

// Hash returns a stable hex-encoded SHA-256 of the input. The hash is
// the only thing `fair publish` consults to decide whether a record put
// is needed (--force bypasses it).
//
// Field separators (\x00) are used between sections so concatenation
// can't smear adjacent fields together (e.g. an empty CoverSourceHash
// followed by a body starting with "abc" is distinct from a CoverSourceHash
// "abc" followed by an empty body).
func Hash(in HashInput) string {
	h := sha256.New()
	h.Write([]byte("body:"))
	h.Write([]byte(in.NormalizedBody))
	h.Write([]byte{0})
	h.Write([]byte("frontmatter:"))
	h.Write(Canonical(in.Frontmatter))
	h.Write([]byte{0})
	h.Write([]byte("cover:"))
	h.Write([]byte(in.CoverSourceHash))
	return hex.EncodeToString(h.Sum(nil))
}
