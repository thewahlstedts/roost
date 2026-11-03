package main

// Folders for the "+ new" picker: where you've opened tabs (with any CLI),
// where Claude has run, and every git repo under your source folders.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func recentPath() string { return filepath.Join(homeDir(), "recent.json") }

// recentEntry is one folder roost has opened a tab in.
type recentEntry struct {
	Last time.Time            `json:"last"`
	Used map[string]time.Time `json:"used"` // CLI name ("shell" for none) -> last use
}

const maxRecent = 300

func loadRecent() map[string]*recentEntry {
	m := map[string]*recentEntry{}
	if b, err := os.ReadFile(recentPath()); err == nil {
		json.Unmarshal(b, &m)
	}
	return m
}

// recordRecent notes that a tab was opened in dir running cli ("" = shell).
func recordRecent(dir, cli string) {
	if dir == "" {
		return
	}
	if cli == "" {
		cli = "shell"
	}
	dir = canonical(dir)
	m := loadRecent()
	e := m[dir]
	if e == nil {
		e = &recentEntry{Used: map[string]time.Time{}}
		m[dir] = e
	}
	now := time.Now()
	e.Last = now
	if e.Used == nil {
		e.Used = map[string]time.Time{}
	}
	e.Used[cli] = now
	if len(m) > maxRecent { // forget the oldest
		dirs := make([]string, 0, len(m))
		for d := range m {
			dirs = append(dirs, d)
		}
		sort.Slice(dirs, func(i, j int) bool { return m[dirs[i]].Last.After(m[dirs[j]].Last) })
		for _, d := range dirs[maxRecent:] {
			delete(m, d)
		}
	}
	if os.MkdirAll(homeDir(), 0o700) != nil {
		return
	}
	b, _ := json.Marshal(m)
	os.WriteFile(recentPath(), b, 0o600)
}

// canonical resolves symlinks (e.g. macOS /tmp -> /private/tmp) so the same
// folder isn't listed twice.
func canonical(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

// defaultSourceRoots are common places people keep code.
var defaultSourceRoots = []string{
	"~/Source", "~/source", "~/src", "~/code", "~/Code", "~/Projects", "~/projects",
	"~/dev", "~/Developer", "~/workspace", "~/repos", "~/git", "~/go/src",
}

// sourceRoots is @roost_source_dirs (space-separated) or the defaults that
// exist on this machine.
func sourceRoots() []string {
	candidates := defaultSourceRoots
	if v, ok := tmuxOption("@roost_source_dirs"); ok {
		candidates = strings.Fields(v)
	}
	var out []string
	seen := map[string]bool{}
	for _, c := range candidates {
		p, err := filepath.EvalSymlinks(expandTilde(c))
		if err != nil || seen[p] {
			continue
		}
		if fi, err := os.Stat(p); err == nil && fi.IsDir() {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

var skipDirs = map[string]bool{
	"node_modules": true, "vendor": true, "target": true, "dist": true, "build": true,
	"Pods": true, "venv": true, "__pycache__": true, "DerivedData": true,
}

// findRepos walks roots (up to depth levels down) for git repositories,
// without descending into a repo once found. It stops after limit repos or
// when the deadline passes, so a huge folder never stalls the picker.
func findRepos(roots []string, depth, limit int, deadline time.Time) []string {
	var out []string
	seen := map[string]bool{}
	var walk func(dir string, d int)
	walk = func(dir string, d int) {
		if len(out) >= limit || time.Now().After(deadline) {
			return
		}
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			if !seen[dir] {
				seen[dir] = true
				out = append(out, dir)
			}
			return
		}
		if d == 0 {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			n := e.Name()
			if !e.IsDir() || strings.HasPrefix(n, ".") || skipDirs[n] {
				continue
			}
			walk(filepath.Join(dir, n), d-1)
		}
	}
	for _, r := range roots {
		walk(r, depth)
	}
	sort.Strings(out)
	return out
}

// pickerFolder is one row in the picker.
type pickerFolder struct {
	Dir  string
	Tags []string // CLIs used here, most recent first
}

// pickerFolders lists the current folder, then recently used folders (newest
// first), then the rest of the repos under your source folders.
func pickerFolders(cwd string) []pickerFolder {
	recent := map[string]*recentEntry{}
	for d, e := range loadRecent() {
		d = canonical(d)
		if cur := recent[d]; cur == nil || e.Last.After(cur.Last) {
			recent[d] = e
		}
	}
	claudeDirs := recentDirs() // Claude's own history, newest first
	for i, d := range claudeDirs {
		claudeDirs[i] = canonical(d)
	}
	type rec struct {
		dir  string
		last time.Time
	}
	var recs []rec
	for d, e := range recent {
		recs = append(recs, rec{d, e.Last})
	}
	for _, d := range claudeDirs {
		if _, ok := recent[d]; !ok {
			recs = append(recs, rec{d, claudeLastUsed(d)})
		}
	}
	sort.SliceStable(recs, func(i, j int) bool { return recs[i].last.After(recs[j].last) })

	claude := map[string]bool{}
	for _, d := range claudeDirs {
		claude[d] = true
	}
	tags := func(dir string) []string {
		var t []string
		if e := recent[dir]; e != nil {
			type u struct {
				n string
				t time.Time
			}
			var us []u
			for n, at := range e.Used {
				us = append(us, u{n, at})
			}
			sort.Slice(us, func(i, j int) bool { return us[i].t.After(us[j].t) })
			for _, x := range us {
				t = append(t, x.n)
			}
		}
		has := func(n string) bool {
			for _, x := range t {
				if x == n {
					return true
				}
			}
			return false
		}
		if claude[dir] && !has("claude") {
			t = append(t, "claude")
		}
		if fi, err := os.Stat(filepath.Join(dir, ".factory")); err == nil && fi.IsDir() && !has("factory") {
			t = append(t, "factory")
		}
		return t
	}

	var out []pickerFolder
	seen := map[string]bool{}
	add := func(d string) {
		d = canonical(d)
		if d == "" || seen[d] {
			return
		}
		if fi, err := os.Stat(d); err != nil || !fi.IsDir() {
			return
		}
		seen[d] = true
		out = append(out, pickerFolder{Dir: d, Tags: tags(d)})
	}
	add(cwd)
	for _, r := range recs {
		add(r.dir)
	}
	for _, d := range findRepos(sourceRoots(), sourceDepth(), 3000, time.Now().Add(400*time.Millisecond)) {
		add(d)
	}
	return out
}

// claudeLastUsed is when Claude last wrote a transcript for dir (zero if
// unknown); used only to order Claude-only folders among roost's.
func claudeLastUsed(dir string) time.Time {
	enc := strings.NewReplacer("/", "-", ".", "-").Replace(dir)
	files, _ := filepath.Glob(filepath.Join(claudeDir(), "projects", enc, "*.jsonl"))
	var t time.Time
	for _, f := range files {
		if fi, err := os.Stat(f); err == nil && fi.ModTime().After(t) {
			t = fi.ModTime()
		}
	}
	return t
}
