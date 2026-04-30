package markdown

import (
	"sync"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
)

// Parser returns the shared, configured goldmark.Markdown used by every
// publish/build pipeline.
//
// Hash stability note: the content hash is over the *markdown source*
// (post-normalize, post-image-rewrite), NOT goldmark's rendered HTML.
// So adding/removing extensions here only affects rendered HTML output,
// not stored hashes. Consumers (e.g., Leaflet) re-render from source so
// they may see different HTML than us depending on their config.
//
// Active extensions:
//   - GFM (tables, strikethrough, task lists, autolinks)
//   - Footnote — markdown [^1] syntax produces <sup><a href="#fn1">
//     and a footnote list at the bottom
//   - DefinitionList — "Term\n: Definition" markdown produces <dl>
//
// Active parser options:
//   - WithAutoHeadingID — anchor IDs derived from heading text, stable
//     across renders, useful for permalink fragments
//
// Active renderer options:
//   - WithUnsafe — pass HTML in the source through unmodified. The publish flow
//     contains hand-authored <blockquote data-bluesky-uri> markers that
//     the build flow rewrites; this also lets posts use raw HTML widgets
//     (<details>, <kbd>, <progress>, etc.) goldmark doesn't natively
//     emit.
func Parser() goldmark.Markdown {
	parserOnce.Do(func() {
		sharedParser = goldmark.New(
			goldmark.WithExtensions(
				extension.GFM,
				extension.Footnote,
				extension.DefinitionList,
			),
			goldmark.WithParserOptions(
				parser.WithAutoHeadingID(),
			),
			goldmark.WithRendererOptions(
				html.WithUnsafe(),
			),
		)
	})
	return sharedParser
}

var (
	parserOnce   sync.Once
	sharedParser goldmark.Markdown
)
