package build

import (
	"net/http"
	"regexp"
	"strings"
)

// Slug-grammar regexes used to validate record fields that get
// interpolated into filesystem paths during the build. The publish flow
// enforces a strict slug grammar via internal/publish/slug.go, but the
// build reads records from a PDS that fair does not necessarily trust
// — a compromised PDS could return a record with `path: "../../tmp/x"`
// or `tags: ["../../etc/passwd"]`. These guards keep PDS-controlled
// strings from escaping the build output directory.

// pathFieldRE accepts the shape produced by publish.go (`/<slug>` —
// single component, lowercase ASCII alphanumeric and hyphen).
var pathFieldRE = regexp.MustCompile(`^/[a-z0-9-]+$`)

// tagNameRE accepts the same character set as a slug. Tags get
// interpolated into `<dist>/tags/<name>/index.html`, so the same
// path-component constraints apply.
var tagNameRE = regexp.MustCompile(`^[a-z0-9-]+$`)

// validRecordPath returns true for paths produced by the canonical
// publish flow (one slug-shaped component with a leading slash).
func validRecordPath(p string) bool {
	return pathFieldRE.MatchString(p)
}

// validTagName returns true for strings safe to use as a single
// directory component under tags/.
func validTagName(t string) bool {
	return tagNameRE.MatchString(t)
}

// validATURI returns true when u parses as a syntactically plausible
// at-uri (`at://...`). Used to gate htmltemplate.URL casts on
// PDS-controlled values; without this gate a hostile record could set
// `uri: "javascript:..."` and have it carry through to a clickable
// rendered link.
func validATURI(u string) bool {
	return strings.HasPrefix(u, "at://") && len(u) > len("at://")
}

// noRedirectPolicy is a CheckRedirect callback that refuses to follow
// any HTTP redirects. Used on the build's HTTP client and the OAuth
// loopback flow's HTTP client so a malicious PDS can't redirect us at
// metadata-IP services on the build host (cloud-metadata SSRF, internal
// CI runner exposure). XRPC endpoints don't redirect in normal
// operation.
func noRedirectPolicy(req *http.Request, via []*http.Request) error {
	return http.ErrUseLastResponse
}
