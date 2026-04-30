package build

import (
	"sort"
)

// tagSummary is the per-tag entry rendered on /tags/index.html.
type tagSummary struct {
	Name  string
	Count int
}

// yearSection is one year's worth of posts, used for /archive/index.html.
type yearSection struct {
	Year  int
	Posts []postView
}

// groupByTag bucketizes posts by tag. Each post appears under each of
// its tags. The returned map's values are pre-sorted publishedAt DESC.
func groupByTag(posts []postView) map[string][]postView {
	out := map[string][]postView{}
	for _, p := range posts {
		for _, tag := range p.Tags {
			out[tag] = append(out[tag], p)
		}
	}
	// Posts inside each bucket: sort newest first.
	for name, ps := range out {
		sort.Slice(ps, func(i, j int) bool {
			return ps[i].PublishedAt.After(ps[j].PublishedAt)
		})
		out[name] = ps
	}
	return out
}

// groupByYear bucketizes posts by publishedAt year, returning years in
// descending order.
func groupByYear(posts []postView) []yearSection {
	byYear := map[int][]postView{}
	for _, p := range posts {
		y := p.PublishedAt.Year()
		byYear[y] = append(byYear[y], p)
	}
	years := make([]int, 0, len(byYear))
	for y := range byYear {
		years = append(years, y)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(years)))

	out := make([]yearSection, 0, len(years))
	for _, y := range years {
		ps := byYear[y]
		sort.Slice(ps, func(i, j int) bool {
			return ps[i].PublishedAt.After(ps[j].PublishedAt)
		})
		out = append(out, yearSection{Year: y, Posts: ps})
	}
	return out
}
