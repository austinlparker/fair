package build

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// jsonFeed implements the JSON Feed v1.1 spec
// (https://www.jsonfeed.org/version/1.1/). Marshaled directly via
// encoding/json instead of a template so escaping is bulletproof.
type jsonFeed struct {
	Version     string         `json:"version"`
	Title       string         `json:"title"`
	HomePageURL string         `json:"home_page_url"`
	FeedURL     string         `json:"feed_url"`
	Description string         `json:"description,omitempty"`
	Items       []jsonFeedItem `json:"items"`
}

type jsonFeedItem struct {
	ID            string     `json:"id"`
	URL           string     `json:"url"`
	Title         string     `json:"title"`
	ContentHTML   string     `json:"content_html,omitempty"`
	Summary       string     `json:"summary,omitempty"`
	DatePublished time.Time  `json:"date_published"`
	DateModified  *time.Time `json:"date_modified,omitempty"`
	Tags          []string   `json:"tags,omitempty"`
	ExternalURL   string     `json:"external_url,omitempty"` // bsky discussion link
}

// writeJSONFeed emits feed.json into tmpOut.
func writeJSONFeed(tmpOut string, site siteContext) error {
	feed := jsonFeed{
		Version:     "https://jsonfeed.org/version/1.1",
		Title:       site.Publication.Name,
		HomePageURL: site.Publication.URL + "/",
		FeedURL:     site.Publication.URL + "/feed.json",
		Description: site.Publication.Description,
		Items:       make([]jsonFeedItem, 0, len(site.Posts)),
	}
	for _, p := range site.Posts {
		item := jsonFeedItem{
			ID:            p.AbsoluteURL,
			URL:           p.AbsoluteURL,
			Title:         p.Title,
			ContentHTML:   string(p.HTMLBody),
			Summary:       p.Description,
			DatePublished: p.PublishedAt,
			Tags:          p.Tags,
			ExternalURL:   p.BskyPostURL,
		}
		if !p.UpdatedAt.IsZero() {
			u := p.UpdatedAt
			item.DateModified = &u
		}
		feed.Items = append(feed.Items, item)
	}
	data, err := json.MarshalIndent(feed, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal json feed: %w", err)
	}
	data = append(data, '\n')
	return os.WriteFile(filepath.Join(tmpOut, "feed.json"), data, 0o644)
}

// writeWellKnownPublication emits /.well-known/site.standard.publication.
// Marshaled rather than templated so a quote in the publication name
// can't produce invalid JSON.
func writeWellKnownPublication(tmpOut string, site siteContext) error {
	doc := struct {
		Type        string `json:"$type"`
		URL         string `json:"url"`
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
	}{"site.standard.publication", site.Publication.URL, site.Publication.Name, site.Publication.Description}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal well-known publication: %w", err)
	}
	dir := filepath.Join(tmpOut, ".well-known")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "site.standard.publication"), append(data, '\n'), 0o644)
}

// indexJSONItem is the trimmed-down per-post entry rendered into
// index.json (no body, just metadata) for programmatic consumption.
type indexJSONItem struct {
	Title       string     `json:"title"`
	URL         string     `json:"url"`
	Path        string     `json:"path"`
	ATURI       string     `json:"at_uri"`
	PublishedAt time.Time  `json:"published_at"`
	UpdatedAt   *time.Time `json:"updated_at,omitempty"`
	Description string     `json:"description,omitempty"`
	Tags        []string   `json:"tags,omitempty"`
}

// writeIndexJSON emits a JSON listing of all posts (metadata only)
// at /index.json. Useful for anyone who wants to scrape the site
// programmatically without parsing HTML.
func writeIndexJSON(tmpOut string, site siteContext) error {
	items := make([]indexJSONItem, 0, len(site.Posts))
	for _, p := range site.Posts {
		entry := indexJSONItem{
			Title:       p.Title,
			URL:         p.AbsoluteURL,
			Path:        p.Path + "/",
			ATURI:       string(p.ATURI),
			PublishedAt: p.PublishedAt,
			Description: p.Description,
			Tags:        p.Tags,
		}
		if !p.UpdatedAt.IsZero() {
			u := p.UpdatedAt
			entry.UpdatedAt = &u
		}
		items = append(items, entry)
	}
	data, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal index json: %w", err)
	}
	data = append(data, '\n')
	return os.WriteFile(filepath.Join(tmpOut, "index.json"), data, 0o644)
}
