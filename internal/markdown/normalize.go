// Package markdown holds the deterministic transforms shared by the
// publish and build flows: text normalization, frontmatter canonicalization,
// AST parsing, and content hashing.
//
// Determinism is the contract. Same source on disk -> same bytes on PDS,
// across runs and across machines.
package markdown

import "strings"

// Normalize returns the canonical form of a markdown source string for
// hashing and for embedding into a PDS record's content.text field.
// It strips a leading BOM, converts CRLF and bare CR to LF, and ensures
// the result ends with exactly one trailing newline (or stays empty if
// the input was empty).
func Normalize(s string) string {
	if s == "" {
		return ""
	}

	// 1. Strip leading BOM.
	const bom = "\ufeff"
	s = strings.TrimPrefix(s, bom)

	// 2. CRLF -> LF, then bare CR -> LF. Order matters: CRLF must be
	//    handled first or "\r\n" becomes "\n\n".
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")

	// 3. Collapse trailing newlines to exactly one. After step 2 the
	//    string may end with "\n\n\n..." — strip back to bare body, then
	//    re-add a single newline.
	s = strings.TrimRight(s, "\n")
	if s == "" {
		// Input was all whitespace / line endings. Treat as empty.
		return ""
	}
	return s + "\n"
}
