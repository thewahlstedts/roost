package main

import (
	"strings"
	"testing"
)

const testLog = `# Changelog
intro text

## Unreleased

- coming soon

## v0.5.0 — 2026-11-04

- five

## v0.4.2 — 2026-11-03

- four two

## Not a version

- ignored

## v0.4.1 — 2026-11-03

- four one
`

func headings(secs []changeSection) string {
	var h []string
	for _, s := range secs {
		h = append(h, strings.Fields(s.Heading)[0])
	}
	return strings.Join(h, ",")
}

func TestChangelog(t *testing.T) {
	if got := headings(parseChangelog(testLog)); got != "Unreleased,v0.5.0,v0.4.2,v0.4.1" {
		t.Errorf("parse = %s", got)
	}
	cases := []struct{ from, to, want string }{
		{"0.4.1", "0.5.0", "v0.5.0,v0.4.2"}, // skipped a version: both shown
		{"0.4.2", "0.5.0", "v0.5.0"},
		{"0.5.0", "0.5.0", ""},
		{"0.4.1", "0.4.2", "v0.4.2"}, // never shows versions newer than the binary
		{"0.4.2", "dev", "Unreleased,v0.5.0"},
	}
	for _, c := range cases {
		if got := headings(changesBetween(testLog, c.from, c.to)); got != c.want {
			t.Errorf("changesBetween(%s, %s) = %q, want %q", c.from, c.to, got, c.want)
		}
	}
	if s := parseChangelog(testLog)[2]; s.Body != "- four two" {
		t.Errorf("body = %q", s.Body)
	}
	if p := previousRelease(testLog, "0.5.0"); p != "0.4.2" {
		t.Errorf("previousRelease = %q", p)
	}
	if p := previousRelease(testLog, "0.4.1"); p != "" {
		t.Errorf("previousRelease of oldest = %q", p)
	}
}

func TestShippedChangelogParses(t *testing.T) {
	secs := parseChangelog(changelogMD)
	versions := 0
	for _, s := range secs {
		if !s.Unreleased {
			versions++
		}
	}
	if versions < 1 {
		t.Errorf("CHANGELOG.md has no version sections: %s", headings(secs))
	}
	for _, s := range secs {
		if s.Body == "" {
			t.Errorf("%s has no notes", s.Heading)
		}
	}
}
