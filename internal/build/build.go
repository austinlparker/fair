package build

import (
	"context"
	"crypto/x509"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	htmltemplate "html/template"
	"io"
	"io/fs"
	texttemplate "text/template"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"aparker.io/fair/internal/atproto"
	"aparker.io/fair/internal/markdown"
	"gopkg.in/yaml.v3"
)

// Embedded templates. These ship inside the binary so the build flow
// works without a working directory layout.
//
//go:embed templates/*
var templatesFS embed.FS

// Config bundles the per-call build inputs.
type Config struct {
	// PDSURL: the user's PDS, used for unauthenticated listRecords /
	// getRecord (public records don't need OAuth).
	PDSURL string

	// DID: the user's identity. The build flow only reads records
	// owned by this DID.
	DID string

	// PublicationURL: absolute URL where the rendered site lives. Used
	// for resolving relative image URLs and for canonical links in the
	// rendered HTML.
	PublicationURL string

	// PublicationName: display title used in <title> tags, etc.
	// Falls back to PublicationURL if empty.
	PublicationName string

	// Description: optional publication-wide tagline. Used for index
	// <meta description> and OpenGraph og:description.
	Description string

	// SocialLinks: URLs declared as <link rel="me"> in every page.
	SocialLinks []string

	// ImagesMirror: directory containing the staged inline images
	// (populated by the publish flow). Copied into <DistDir>/images/.
	ImagesMirror string

	// BlogPosts: directory containing the markdown source. The build
	// flow only reads one optional file from here — _index.md, used as
	// a free-form intro on the home page. Posts themselves come from
	// the PDS, not from this directory.
	BlogPosts string

	// DistDir: target output directory. Build writes to DistDir+".tmp"
	// then atomically renames.
	DistDir string

	// CAPool: optional extra trust anchors. Use for the sandbox
	// (mkcert root) to talk to https://pds.localtest.me without
	// system trust modifications.
	CAPool *x509.CertPool
}

// Result reports what Build produced.
type Result struct {
	PostCount int
	ColdStart bool // true when the publication record was missing
	OutDir    string
}

// Build reads the publication + document records from the configured
// PDS and emits a complete static site in cfg.DistDir. The flow:
//  1. Sweep stale dist.old.* dirs from prior crashes.
//  2. Always emit <DistDir.tmp>/.well-known/oauth-client-metadata.json.
//  3. Try to fetch the publication record; on absence, emit a cold-start
//     stub site (well-known, empty feed/sitemap, stub index/404) and exit.
//  4. listRecords paginated; abort hard on any pagination error.
//  5. Render each document to dist.tmp/<path>/index.html.
//  6. Render index.html, feed.xml, sitemap.xml, .well-known/site.standard.publication, 404.html.
//  7. Copy ImagesMirror -> dist.tmp/images/.
//  8. Atomically promote dist.tmp -> dist.
func Build(ctx context.Context, cfg Config) (*Result, error) {
	if cfg.DistDir == "" {
		return nil, errors.New("DistDir is required")
	}
	// Strip trailing slash so filepath.Dir computes the actual parent
	// (Dir of "foo/" is "foo", not "."), and so AtomicPromote's rename
	// target doesn't end up inside the source.
	cfg.DistDir = strings.TrimRight(cfg.DistDir, "/")
	parent := filepath.Dir(cfg.DistDir)
	if err := SweepStaleDistOld(parent); err != nil {
		return nil, fmt.Errorf("sweep stale dist.old: %w", err)
	}

	tmpOut := cfg.DistDir + ".tmp"
	// Reset tmpOut so a failed prior build doesn't leak files.
	if err := os.RemoveAll(tmpOut); err != nil {
		return nil, fmt.Errorf("reset tmp: %w", err)
	}
	if err := os.MkdirAll(tmpOut, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir tmp: %w", err)
	}

	// Always emit OAuth client metadata first — it's part of the
	// cold-start contract: the auth flow needs it before any login
	// can happen, even on a virgin deploy.
	if err := atproto.EmitClientMetadata(tmpOut, cfg.PublicationURL); err != nil {
		return nil, fmt.Errorf("emit client metadata: %w", err)
	}

	httpClient := &http.Client{
		Timeout: 30 * time.Second,
		// Refuse to follow redirects from the PDS — XRPC endpoints don't
		// redirect normally, and a hostile PDS could otherwise bounce us
		// to an internal/metadata service on the build host (SSRF).
		CheckRedirect: noRedirectPolicy,
	}
	if cfg.CAPool != nil {
		httpClient.Transport = caTransport(cfg.CAPool)
	}

	// Step 3: try to fetch the publication record.
	pub, err := fetchPublication(ctx, httpClient, cfg.PDSURL, cfg.DID)
	if err != nil {
		// Hard error fetching from PDS — abort, leave existing dist alone.
		return nil, fmt.Errorf("fetch publication: %w", err)
	}

	siteData := siteContext{
		Publication: publicationView{
			URL:         strings.TrimRight(cfg.PublicationURL, "/"),
			Name:        coalesce(cfg.PublicationName, cfg.PublicationURL),
			Description: cfg.Description,
			ATURI:       htmltemplate.URL("at://" + cfg.DID + "/site.standard.publication/self"),
			SocialLinks: cfg.SocialLinks,
		},
		GeneratedAt: time.Now().UTC(),
	}
	if pub != nil {
		if pub.Name != "" {
			siteData.Publication.Name = pub.Name
		}
		if pub.Description != "" {
			siteData.Publication.Description = pub.Description
		}
		// publication.icon is a typed blob in the lexicon. Fetch it and
		// save as /favicon.<ext>; absent records leave FaviconPath empty.
		if pub.Icon != nil {
			if path, err := fetchAndSaveFavicon(ctx, httpClient, cfg, pub.Icon, tmpOut); err == nil && path != "" {
				siteData.Publication.FaviconPath = path
			} else if err != nil {
				fmt.Fprintf(os.Stderr, "warn: favicon fetch failed: %v\n", err)
			}
		}
	}

	if pub == nil {
		// Cold start: emit minimal site, atomic-promote, return.
		if err := emitColdStart(tmpOut, siteData); err != nil {
			return nil, err
		}
		if err := AtomicPromote(tmpOut, cfg.DistDir); err != nil {
			return nil, err
		}
		return &Result{ColdStart: true, OutDir: cfg.DistDir}, nil
	}

	// Step 4: paginated listRecords (read-only, unauthenticated).
	docs, err := listAllDocuments(ctx, httpClient, cfg.PDSURL, cfg.DID)
	if err != nil {
		return nil, fmt.Errorf("listRecords: %w", err)
	}

	posts := make([]postView, 0, len(docs))
	for _, d := range docs {
		pv, err := documentToPostView(d, cfg.PublicationURL)
		if err != nil {
			// Skip malformed records; log and move on.
			fmt.Fprintf(os.Stderr, "  skipping %s: %v\n", d.URI, err)
			continue
		}
		// Defense in depth: PDS-controlled `path` field gets joined into
		// the dist directory below. Reject anything that doesn't match
		// the canonical slug shape rather than trust filepath.Join's
		// .. cleaning to contain the path.
		if !validRecordPath(pv.Path) {
			fmt.Fprintf(os.Stderr, "  skipping %s: invalid path %q\n", d.URI, pv.Path)
			continue
		}
		// Filter out tags whose names would escape tags/<name>/. Drop the
		// invalid entries and keep the post.
		if len(pv.Tags) > 0 {
			kept := make([]string, 0, len(pv.Tags))
			for _, t := range pv.Tags {
				if validTagName(t) {
					kept = append(kept, t)
				} else {
					fmt.Fprintf(os.Stderr, "  %s: dropping invalid tag %q\n", d.URI, t)
				}
			}
			pv.Tags = kept
		}
		// Add srcset/sizes/width/height to <img> tags whose source has
		// resized variants in the mirror. No-op when mirror is empty
		// or when an image has no variants on disk.
		body := EnrichResponsiveImages(string(pv.HTMLBody), cfg.ImagesMirror, cfg.PublicationURL)
		// Add native lazy-loading + async decoding to all <img>.
		body = EnhanceImages(body)
		pv.HTMLBody = htmltemplate.HTML(body)
		// Fetch the cover blob (if any) into /covers/<slug>.<ext> for
		// og:image and the post-header image. Non-fatal: log and render
		// the post coverless on failure.
		if pv.coverImage != nil {
			slug := strings.TrimPrefix(pv.Path, "/")
			if path, err := fetchAndSaveCover(ctx, httpClient, cfg, pv.coverImage, slug, tmpOut); err == nil {
				pv.CoverPath = path
			} else {
				fmt.Fprintf(os.Stderr, "  warn: cover fetch failed for %s: %v\n", d.URI, err)
			}
		}
		posts = append(posts, pv)
	}

	// publishedAt DESC.
	sort.Slice(posts, func(i, j int) bool {
		return posts[i].PublishedAt.After(posts[j].PublishedAt)
	})

	// Wire prev/next now that order is final.
	// Convention: "prev" = older (further down the list), "next" = newer.
	// Index page is sorted newest-first, so posts[i+1] is older than posts[i].
	for i := range posts {
		if i+1 < len(posts) {
			posts[i].Prev = &postNeighbor{Title: posts[i+1].Title, Path: posts[i+1].Path}
		}
		if i > 0 {
			posts[i].Next = &postNeighbor{Title: posts[i-1].Title, Path: posts[i-1].Path}
		}
	}
	siteData.Posts = posts
	siteData.Years = groupByYear(posts)
	intro, homeMetaFM, err := loadHomeContent(cfg.BlogPosts, siteData.Publication.URL)
	if err != nil {
		return nil, fmt.Errorf("load home content: %w", err)
	}
	siteData.Intro = intro
	siteData.HomeMeta = homeMetaFM
	siteData.HideYears = homeMetaFM.HideYears
	// Resolve pinned slugs against posts for the Featured section.
	if len(homeMetaFM.Pinned) > 0 {
		bySlug := make(map[string]postView, len(posts))
		for _, p := range posts {
			bySlug[strings.TrimPrefix(p.Path, "/")] = p
		}
		for _, slug := range homeMetaFM.Pinned {
			if p, ok := bySlug[slug]; ok {
				siteData.Featured = append(siteData.Featured, p)
			} else {
				fmt.Fprintf(os.Stderr, "  warn: _index.md pinned slug %q not found among posts\n", slug)
			}
		}
	}
	outro, err := loadOutro(cfg.BlogPosts, siteData.Publication.URL)
	if err != nil {
		return nil, fmt.Errorf("load outro: %w", err)
	}
	siteData.Outro = outro

	// Pre-compute tag summaries so they're available for the home-page
	// template's tag cloud as well as the /tags/ index later in step 7.
	tagsByName := groupByTag(posts)
	tagNames := make([]string, 0, len(tagsByName))
	for name := range tagsByName {
		tagNames = append(tagNames, name)
	}
	sort.Strings(tagNames)
	tagSummaries := make([]tagSummary, 0, len(tagNames))
	for _, name := range tagNames {
		tagSummaries = append(tagSummaries, tagSummary{Name: name, Count: len(tagsByName[name])})
	}
	siteData.Tags = tagSummaries

	// Step 5: render per-post pages. documentToPostView leaves
	// p.Publication.Name empty (it doesn't have access to the
	// publication record); fill it in here so post.html's <title>
	// and footer link aren't half-rendered.
	for _, p := range posts {
		p.Publication = siteData.Publication
		dir := filepath.Join(tmpOut, strings.TrimPrefix(p.Path, "/"))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("mkdir %s: %w", dir, err)
		}
		out, err := os.Create(filepath.Join(dir, "index.html"))
		if err != nil {
			return nil, err
		}
		err = renderTemplate(out, "post.html", p)
		out.Close()
		if err != nil {
			return nil, fmt.Errorf("render post %s: %w", p.Path, err)
		}
	}

	// Step 6: index, feed, sitemap, well-known, 404.
	if err := writeRendered(tmpOut, "index.html", "index.html", siteData); err != nil {
		return nil, err
	}
	if err := writeRendered(tmpOut, "feed.xml", "feed.xml", siteData); err != nil {
		return nil, err
	}
	if err := writeRendered(tmpOut, "sitemap.xml", "sitemap.xml", siteData); err != nil {
		return nil, err
	}
	if err := writeRendered(tmpOut, "404.html", "404.html", siteData); err != nil {
		return nil, err
	}
	if err := writeRendered(tmpOut, ".well-known/site.standard.publication", "well-known-publication.json", siteData); err != nil {
		return nil, err
	}
	if err := writeRendered(tmpOut, "robots.txt", "robots.txt", siteData); err != nil {
		return nil, err
	}
	if err := writeRendered(tmpOut, "rss.xml", "rss.xml", siteData); err != nil {
		return nil, err
	}
	if err := writeJSONFeed(tmpOut, siteData); err != nil {
		return nil, err
	}
	if err := writeIndexJSON(tmpOut, siteData); err != nil {
		return nil, err
	}

	// Tag pages: one per distinct tag plus a /tags/ index. Summaries
	// were precomputed earlier so siteData.Tags could be used by the
	// home-page template; reuse them here.
	if len(tagsByName) > 0 {
		if err := writeRendered(tmpOut, "tags/index.html", "tags-index.html", struct {
			Publication publicationView
			Tags        []tagSummary
		}{siteData.Publication, tagSummaries}); err != nil {
			return nil, err
		}
		// /tags/<name>/index.html
		for _, name := range tagNames {
			rel := filepath.Join("tags", name, "index.html")
			if err := writeRendered(tmpOut, rel, "tag.html", struct {
				Publication publicationView
				Tag         string
				Posts       []postView
			}{siteData.Publication, name, tagsByName[name]}); err != nil {
				return nil, err
			}
		}
	}

	// Archive: posts grouped by year, descending.
	if len(posts) > 0 {
		years := groupByYear(posts)
		if err := writeRendered(tmpOut, "archive/index.html", "archive.html", struct {
			Publication publicationView
			Years       []yearSection
		}{siteData.Publication, years}); err != nil {
			return nil, err
		}
	}

	// Step 7: copy image mirror.
	if cfg.ImagesMirror != "" {
		if err := copyImagesMirror(cfg.ImagesMirror, filepath.Join(tmpOut, "images")); err != nil {
			return nil, err
		}
	}

	// Step 8: atomic promote.
	if err := AtomicPromote(tmpOut, cfg.DistDir); err != nil {
		return nil, err
	}
	return &Result{
		PostCount: len(posts),
		OutDir:    cfg.DistDir,
	}, nil
}

// emitColdStart writes a complete-but-empty site so the well-known
// endpoints, feed, sitemap, and index serve correctly even before any
// publication record exists.
func emitColdStart(tmpOut string, siteData siteContext) error {
	for _, t := range []struct{ outRel, tmpl string }{
		{"index.html", "index.html"},
		{"feed.xml", "feed.xml"},
		{"sitemap.xml", "sitemap.xml"},
		{"404.html", "404.html"},
		{".well-known/site.standard.publication", "well-known-publication.json"},
		{"robots.txt", "robots.txt"},
		{"rss.xml", "rss.xml"},
	} {
		if err := writeRendered(tmpOut, t.outRel, t.tmpl, siteData); err != nil {
			return err
		}
	}
	if err := writeJSONFeed(tmpOut, siteData); err != nil {
		return err
	}
	if err := writeIndexJSON(tmpOut, siteData); err != nil {
		return err
	}
	return nil
}

// writeRendered renders the named template into <tmpOut>/<outRel>.
func writeRendered(tmpOut, outRel, tmplName string, data any) error {
	full := filepath.Join(tmpOut, outRel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	out, err := os.Create(full)
	if err != nil {
		return err
	}
	defer out.Close()
	return renderTemplate(out, tmplName, data)
}

// renderTemplate parses templates/<name> from the embedded FS and writes
// the rendered bytes to w. HTML templates use html/template (auto-
// escaping), XML/JSON templates use text/template (verbatim) so the
// "<?xml ?>" declaration and JSON braces survive intact.
func renderTemplate(w io.Writer, name string, data any) error {
	src, err := fs.ReadFile(templatesFS, "templates/"+name)
	if err != nil {
		return fmt.Errorf("read template %s: %w", name, err)
	}
	if isHTMLTemplate(name) {
		t, err := htmltemplate.New(name).Parse(string(src))
		if err != nil {
			return fmt.Errorf("parse html template %s: %w", name, err)
		}
		return t.Execute(w, data)
	}
	t, err := texttemplate.New(name).Parse(string(src))
	if err != nil {
		return fmt.Errorf("parse text template %s: %w", name, err)
	}
	return t.Execute(w, data)
}

// isHTMLTemplate returns true for files whose output should be auto-
// escaped HTML. Anything else (XML, JSON) goes through text/template
// verbatim.
func isHTMLTemplate(name string) bool {
	return strings.HasSuffix(name, ".html")
}

// coalesce returns the first non-empty string of its args.
func coalesce(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// caTransport returns an http.Transport pinned to the given pool.
func caTransport(pool *x509.CertPool) http.RoundTripper {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.TLSClientConfig = httpsClientConfig(pool)
	return t
}

// --- view types -------------------------------------------------------------

type publicationView struct {
	URL         string
	Name        string
	Description string
	ATURI       htmltemplate.URL // at://<did>/site.standard.publication/self — emitted as rel="site.standard.publication" for discovery
	SocialLinks []string         // raw URLs; emit as <link rel="me"> tags in the document head
	FaviconPath string           // e.g. "/favicon.png"; empty when no icon blob
}

type postNeighbor struct {
	Title string
	Path  string
}

type postView struct {
	Title          string
	Path           string             // leading slash, no trailing slash
	AbsoluteURL    string             // publication.URL + path + "/"
	ATURI          htmltemplate.URL   // at://did/site.standard.document/<rkey> (marked safe so html/template doesn't rewrite to #ZgotmplZ)
	PdsLsURL       string             // https://pdsls.dev/at://... — web viewer for the canonical record
	PublishedAt    time.Time
	UpdatedAt      time.Time
	Lastmod        time.Time // = UpdatedAt or PublishedAt; precomputed for sitemap.xml
	Description    string
	HTMLBody       htmltemplate.HTML
	CoverPath      string             // "/covers/<slug>.<ext>" for og:image + header <img>; empty when no cover blob
	coverImage     *blobRef           // unexported: cover blob ref awaiting getBlob in Build's loop; not rendered
	Tags           []string
	BskyPostURL    string // bsky.app web URL derived from bskyPostRef
	BskyPostAtURI  string // at-uri of the bsky discussion thread; used by the inline-JS comments fetch
	ReadingMinutes int    // wordcount/250 rounded up, computed from markdown source body
	Prev           *postNeighbor
	Next           *postNeighbor
	Publication    publicationView // copied so post template can reach it
}

type siteContext struct {
	Publication publicationView
	Posts       []postView
	Years       []yearSection     // posts bucketed by publishedAt year, descending; used by the home page
	Intro       htmltemplate.HTML // optional rendered _index.md body content shown above the year disclosures
	Outro       htmltemplate.HTML // optional rendered _outro.md content shown below the year disclosures
	Tags        []tagSummary      // alphabetical tag list; rendered as a footer cloud on the home page
	HomeMeta    homeMeta          // optional frontmatter from _index.md driving home-only customization
	Featured    []postView        // posts pinned via _index.md frontmatter, in declared order
	HideYears   bool              // true when _index.md frontmatter sets `hide_years: true`
	GeneratedAt time.Time
}

// homeMeta is the optional YAML frontmatter on _index.md. None of these
// fields are required; missing file or missing fields fall back to the
// publication record (description) or to default rendering.
type homeMeta struct {
	Description string   `yaml:"description"` // overrides Publication.Description on the home page only
	Subtitle    string   `yaml:"subtitle"`    // extra <p> between <h1> and the intro body
	Pinned      []string `yaml:"pinned"`      // slugs to render as a Featured section above year groups
	HideYears   bool     `yaml:"hide_years"`  // suppress the year-group disclosures (e.g., for a fully-curated home)
}

// loadHomeContent reads <blogPostsDir>/_index.md if it exists, splits
// optional YAML frontmatter from the markdown body, and renders the
// body through the same goldmark + post-process pipeline used for posts.
// Missing file → zero values, no error. Used by the home page for the
// intro section + frontmatter-driven customization (subtitle, pinned,
// description override, hide_years).
func loadHomeContent(blogPostsDir, publicationURL string) (htmltemplate.HTML, homeMeta, error) {
	if blogPostsDir == "" {
		return "", homeMeta{}, nil
	}
	src, err := os.ReadFile(filepath.Join(blogPostsDir, "_index.md"))
	if err != nil {
		if os.IsNotExist(err) {
			return "", homeMeta{}, nil
		}
		return "", homeMeta{}, err
	}
	fmRaw, body, err := markdown.Split(string(src))
	if err != nil {
		return "", homeMeta{}, fmt.Errorf("_index.md frontmatter: %w", err)
	}
	var meta homeMeta
	if fmRaw != "" {
		if err := yaml.Unmarshal([]byte(fmRaw), &meta); err != nil {
			return "", homeMeta{}, fmt.Errorf("_index.md frontmatter: %w", err)
		}
	}
	html, err := RenderMarkdown(body, publicationURL)
	if err != nil {
		return "", homeMeta{}, err
	}
	return htmltemplate.HTML(html), meta, nil
}

// loadOutro reads <blogPostsDir>/_outro.md if it exists and renders it
// through the markdown pipeline. Missing file → empty string, no error.
// Rendered below the year groups + above the home-page footer.
func loadOutro(blogPostsDir, publicationURL string) (htmltemplate.HTML, error) {
	if blogPostsDir == "" {
		return "", nil
	}
	body, err := os.ReadFile(filepath.Join(blogPostsDir, "_outro.md"))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	html, err := RenderMarkdown(string(body), publicationURL)
	if err != nil {
		return "", err
	}
	return htmltemplate.HTML(html), nil
}

// --- record fetching --------------------------------------------------------

type rawDocument struct {
	URI   string          `json:"uri"`
	CID   string          `json:"cid"`
	Value json.RawMessage `json:"value"`
}

type listRecordsResponse struct {
	Records []rawDocument `json:"records"`
	Cursor  string        `json:"cursor,omitempty"`
}

// fetchPublication returns the publication record for did, or nil when
// the record is absent (404 from getRecord). Other errors are returned
// as-is.
func fetchPublication(ctx context.Context, hc *http.Client, pdsURL, did string) (*publicationRecord, error) {
	q := url.Values{}
	q.Set("repo", did)
	q.Set("collection", "site.standard.publication")
	q.Set("rkey", "self")
	u := strings.TrimRight(pdsURL, "/") + "/xrpc/com.atproto.repo.getRecord?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusNotFound {
		// PDS returns 400 with InvalidRequest on a missing record.
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("getRecord status %d", resp.StatusCode)
	}
	var out struct {
		Value publicationRecord `json:"value"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode publication: %w", err)
	}
	return &out.Value, nil
}

type publicationRecord struct {
	URL         string    `json:"url"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Icon        *blobRef  `json:"icon,omitempty"`
}

// blobRef is the JSON shape of an ATProto typed blob. We only use it
// to extract the CID + mimeType for fetching the icon via getBlob.
type blobRef struct {
	Type     string `json:"$type"`
	Ref      struct {
		Link string `json:"$link"`
	} `json:"ref"`
	MimeType string `json:"mimeType"`
	Size     int64  `json:"size"`
}

// listAllDocuments paginates through site.standard.document records.
func listAllDocuments(ctx context.Context, hc *http.Client, pdsURL, did string) ([]rawDocument, error) {
	var all []rawDocument
	cursor := ""
	for {
		q := url.Values{}
		q.Set("repo", did)
		q.Set("collection", "site.standard.document")
		q.Set("limit", "100")
		if cursor != "" {
			q.Set("cursor", cursor)
		}
		u := strings.TrimRight(pdsURL, "/") + "/xrpc/com.atproto.repo.listRecords?" + q.Encode()
		req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
		if err != nil {
			return nil, err
		}
		resp, err := hc.Do(req)
		if err != nil {
			return nil, err
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("listRecords status %d: %s", resp.StatusCode, string(body))
		}
		var page listRecordsResponse
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, fmt.Errorf("decode listRecords: %w", err)
		}
		all = append(all, page.Records...)
		if page.Cursor == "" || len(page.Records) == 0 {
			return all, nil
		}
		cursor = page.Cursor
	}
}

// --- conversion -------------------------------------------------------------

type rawDocumentValue struct {
	Title       string   `json:"title"`
	Path        string   `json:"path"`
	PublishedAt string   `json:"publishedAt"`
	UpdatedAt   string   `json:"updatedAt,omitempty"`
	Description string   `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Content     struct {
		Type    string `json:"$type"`
		Text    string `json:"text"`
		Version string `json:"version,omitempty"`
	} `json:"content"`
	CoverImage  *blobRef `json:"coverImage,omitempty"`
	BskyPostRef *struct {
		URI string `json:"uri"`
		CID string `json:"cid"`
	} `json:"bskyPostRef,omitempty"`
}

func documentToPostView(d rawDocument, publicationURL string) (postView, error) {
	var v rawDocumentValue
	if err := json.Unmarshal(d.Value, &v); err != nil {
		return postView{}, fmt.Errorf("decode value: %w", err)
	}
	if v.Title == "" || v.Path == "" || v.PublishedAt == "" || v.Content.Text == "" {
		return postView{}, errors.New("missing required field (title/path/publishedAt/content.text)")
	}

	publishedAt, err := time.Parse(time.RFC3339, v.PublishedAt)
	if err != nil {
		return postView{}, fmt.Errorf("parse publishedAt: %w", err)
	}
	var updatedAt time.Time
	if v.UpdatedAt != "" {
		if t, e := time.Parse(time.RFC3339, v.UpdatedAt); e == nil {
			updatedAt = t
		}
	}
	html, err := RenderMarkdown(v.Content.Text, publicationURL)
	if err != nil {
		return postView{}, err
	}

	lastmod := publishedAt
	if !updatedAt.IsZero() {
		lastmod = updatedAt
	}

	pub := publicationView{
		URL:  strings.TrimRight(publicationURL, "/"),
		Name: "", // populated in Build's pass via field assignment
	}

	path := strings.TrimRight(v.Path, "/")
	absoluteURL := pub.URL + path + "/"

	// Extract rkey from the at-uri tail for the ATProto-canonical
	// alternate link. d.URI looks like
	// "at://did:plc:.../site.standard.document/<rkey>".
	atURI := d.URI

	bskyPostURL := ""
	bskyPostAtURI := ""
	if v.BskyPostRef != nil && v.BskyPostRef.URI != "" && validATURI(v.BskyPostRef.URI) {
		if w, err := bskyWebURL(v.BskyPostRef.URI); err == nil {
			bskyPostURL = w
		}
		bskyPostAtURI = v.BskyPostRef.URI
	}

	// at-uris pass through to <link rel="alternate"> via htmltemplate.URL
	// (which normally allows arbitrary schemes). A compromised PDS could
	// return uri: "javascript:..." otherwise — gate on the at:// prefix.
	if !validATURI(atURI) {
		return postView{}, fmt.Errorf("invalid at-uri %q", atURI)
	}
	return postView{
		Title:          v.Title,
		Path:           path,
		AbsoluteURL:    absoluteURL,
		ATURI:          htmltemplate.URL(atURI),
		PdsLsURL:       "https://pdsls.dev/" + atURI,
		PublishedAt:    publishedAt,
		UpdatedAt:      updatedAt,
		Lastmod:        lastmod,
		Description:    v.Description,
		HTMLBody:       htmltemplate.HTML(html),
		coverImage:     v.CoverImage,
		Tags:           v.Tags,
		BskyPostURL:    bskyPostURL,
		BskyPostAtURI:  bskyPostAtURI,
		ReadingMinutes: estimateReadingMinutes(v.Content.Text),
		Publication:    pub,
	}, nil
}

// estimateReadingMinutes returns ceil(wordcount / 250). 250 wpm is the
// common middle-ground for adult silent reading; useful as a "how long
// is this post going to take" hint, not an exact figure.
func estimateReadingMinutes(body string) int {
	words := len(strings.Fields(body))
	if words == 0 {
		return 0
	}
	mins := words / 250
	if words%250 != 0 {
		mins++
	}
	if mins < 1 {
		mins = 1
	}
	return mins
}

// --- helpers ----------------------------------------------------------------

// copyImagesMirror walks src and copies each file to dest, preserving
// the relative path. Idempotent on identical files.
func copyImagesMirror(src, dest string) error {
	if _, err := os.Stat(src); errors.Is(err, fs.ErrNotExist) {
		return nil // nothing to copy
	}
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

// _ keeps markdown imported for the future render pipeline if we move
// transforms here.
var _ = markdown.Normalize
