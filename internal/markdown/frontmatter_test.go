package markdown_test

import (
	"reflect"
	"strings"
	"testing"

	"aparker.io/fair/internal/markdown"
)

func TestSplit_FrontmatterAndBody(t *testing.T) {
	src := "---\ntitle: Hello\ndate: 2025-04-17\n---\n\nbody text\n"
	fm, body, err := markdown.Split(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fm != "title: Hello\ndate: 2025-04-17\n" {
		t.Errorf("frontmatter: %q", fm)
	}
	if body != "body text\n" {
		t.Errorf("body: %q", body)
	}
}

func TestSplit_NoFrontmatter(t *testing.T) {
	src := "just body, no frontmatter\n"
	fm, body, err := markdown.Split(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fm != "" {
		t.Errorf("frontmatter: %q (expected empty)", fm)
	}
	if body != src {
		t.Errorf("body: %q", body)
	}
}

func TestSplit_UnclosedFrontmatter_IsError(t *testing.T) {
	src := "---\ntitle: Hello\nbody text without closing fence\n"
	_, _, err := markdown.Split(src)
	if err == nil {
		t.Fatal("expected error for unterminated frontmatter")
	}
	if !strings.Contains(err.Error(), "frontmatter") {
		t.Errorf("error %q should mention frontmatter", err.Error())
	}
}

func TestSplit_EmptyFrontmatter(t *testing.T) {
	// "---\n---\n\nbody" — valid: empty frontmatter, body follows
	src := "---\n---\nbody\n"
	fm, body, err := markdown.Split(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fm != "" {
		t.Errorf("frontmatter should be empty, got %q", fm)
	}
	if body != "body\n" {
		t.Errorf("body: %q", body)
	}
}

func TestParse_KnownFields(t *testing.T) {
	src := "---\nid: 01HXTEST123\ntitle: \"Wide Events\"\ndate: \"2025-04-17\"\ndescription: An essay\ntags:\n  - observability\n  - personal\nslug: wide-events\ncover: ./images/cover.png\nupdated: \"2025-05-01\"\n---\n\nbody\n"
	fm, body, err := markdown.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if fm.ID != "01HXTEST123" {
		t.Errorf("ID: %q", fm.ID)
	}
	if fm.Title != "Wide Events" {
		t.Errorf("Title: %q", fm.Title)
	}
	if fm.Date != "2025-04-17" {
		t.Errorf("Date: %q", fm.Date)
	}
	if fm.Description != "An essay" {
		t.Errorf("Description: %q", fm.Description)
	}
	if !reflect.DeepEqual(fm.Tags, []string{"observability", "personal"}) {
		t.Errorf("Tags: %v", fm.Tags)
	}
	if fm.Slug != "wide-events" {
		t.Errorf("Slug: %q", fm.Slug)
	}
	if fm.Cover != "./images/cover.png" {
		t.Errorf("Cover: %q", fm.Cover)
	}
	if fm.Updated != "2025-05-01" {
		t.Errorf("Updated: %q", fm.Updated)
	}
	if body != "body\n" {
		t.Errorf("body: %q", body)
	}
}

func TestParse_MissingTitleOrDate_IsError(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"no title", "---\ndate: 2025-04-17\n---\nbody\n", "title"},
		{"no date", "---\ntitle: Hello\n---\nbody\n", "date"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := markdown.Parse(tc.src)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q should mention %q", err.Error(), tc.want)
			}
		})
	}
}
