package build

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakePDS serves getRecord for the publication and listRecords for
// documents. pubStatus/pubBody control the publication response.
func fakePDS(t *testing.T, pubStatus int, pubBody string, docs []map[string]any) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/xrpc/com.atproto.repo.getRecord":
			w.WriteHeader(pubStatus)
			fmt.Fprint(w, pubBody)
		case "/xrpc/com.atproto.repo.listRecords":
			records := make([]map[string]any, 0, len(docs))
			for i, d := range docs {
				records = append(records, map[string]any{
					"uri":   fmt.Sprintf("at://did:plc:test/site.standard.document/3k%011d", i),
					"cid":   "bafy",
					"value": d,
				})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"records": records})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func testConfig(t *testing.T, pdsURL string) Config {
	t.Helper()
	return Config{
		PDSURL:          pdsURL,
		DID:             "did:plc:test",
		PublicationURL:  "https://example.com",
		PublicationName: "Fallback",
		DistDir:         filepath.Join(t.TempDir(), "dist"),
	}
}

func TestBuild_ColdStartOnRecordNotFound(t *testing.T) {
	srv := fakePDS(t, http.StatusBadRequest, `{"error":"RecordNotFound","message":"Could not locate record"}`, nil)
	cfg := testConfig(t, srv.URL)

	res, err := Build(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !res.ColdStart {
		t.Errorf("expected cold start")
	}
	for _, f := range []string{"index.html", "feed.xml", ".well-known/oauth-client-metadata.json", ".well-known/site.standard.publication"} {
		if _, err := os.Stat(filepath.Join(cfg.DistDir, f)); err != nil {
			t.Errorf("cold-start site missing %s: %v", f, err)
		}
	}
}

// A PDS error that isn't "record does not exist" must not be mistaken
// for a cold start — that would replace the live site with a stub.
func TestBuild_OtherPDSErrorLeavesDistAlone(t *testing.T) {
	srv := fakePDS(t, http.StatusBadRequest, `{"error":"InvalidRequest","message":"Could not find repo"}`, nil)
	cfg := testConfig(t, srv.URL)
	if err := os.MkdirAll(cfg.DistDir, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(cfg.DistDir, "index.html")
	if err := os.WriteFile(marker, []byte("live site"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Build(context.Background(), cfg); err == nil {
		t.Fatal("expected Build to fail")
	}
	got, err := os.ReadFile(marker)
	if err != nil || string(got) != "live site" {
		t.Errorf("existing dist was disturbed: %q, %v", got, err)
	}
}

func TestBuild_EscapesMetadataInFeedsAndWellKnown(t *testing.T) {
	pub := `{"uri":"at://x","cid":"y","value":{"url":"https://example.com","name":"Q&A \"Weekly\" <beta>"}}`
	srv := fakePDS(t, http.StatusOK, pub, []map[string]any{{
		"title":       "Tips & <Tricks>",
		"path":        "/tips",
		"publishedAt": "2025-04-17T00:00:00Z",
		"description": "a < b && c",
		"tags":        []string{"go"},
		"content":     map[string]any{"text": "Literal `]]>` in a post.\n"},
	}})
	cfg := testConfig(t, srv.URL)

	res, err := Build(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if res.PostCount != 1 {
		t.Fatalf("PostCount: got %d, want 1", res.PostCount)
	}

	for _, name := range []string{"feed.xml", "rss.xml", "sitemap.xml"} {
		f, err := os.Open(filepath.Join(cfg.DistDir, name))
		if err != nil {
			t.Fatal(err)
		}
		dec := xml.NewDecoder(f)
		var text strings.Builder
		for {
			tok, err := dec.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("%s is not well-formed XML: %v", name, err)
			}
			if cd, ok := tok.(xml.CharData); ok {
				text.Write(cd)
			}
		}
		f.Close()
		if name != "sitemap.xml" && !strings.Contains(text.String(), "Tips & <Tricks>") {
			t.Errorf("%s: title did not round-trip: %q", name, text.String())
		}
	}

	raw, err := os.ReadFile(filepath.Join(cfg.DistDir, ".well-known", "site.standard.publication"))
	if err != nil {
		t.Fatal(err)
	}
	var wk struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &wk); err != nil {
		t.Fatalf("well-known is not valid JSON: %v\n%s", err, raw)
	}
	if wk.Name != `Q&A "Weekly" <beta>` {
		t.Errorf("well-known name: got %q", wk.Name)
	}

	for _, f := range []string{"tips/index.html", "tags/go/index.html", "archive/index.html", "feed.json", "index.json", "robots.txt", "404.html"} {
		if _, err := os.Stat(filepath.Join(cfg.DistDir, f)); err != nil {
			t.Errorf("missing %s: %v", f, err)
		}
	}
}
