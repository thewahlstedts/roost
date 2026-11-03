package main

// Folders for the "+ new" picker: where you've opened tabs (with any CLI),
// where Claude has run, and every git repo under your source folders.

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"
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
// when the deadline passes.
func findRepos(roots []string, depth, limit int, deadline time.Time) []string {
	out, _ := scanRepos(roots, depth, limit, deadline)
	return out
}

// scanRepos is findRepos that also says whether it finished (false when the
// limit or deadline cut it short). limit <= 0 and a zero deadline mean no
// limit; found, if set, is called for each repo as it's found.
func scanRepos(roots []string, depth, limit int, deadline time.Time, found ...func(string)) (out []string, complete bool) {
	complete = true
	seen := map[string]bool{}
	var walk func(dir string, d int)
	walk = func(dir string, d int) {
		if (limit > 0 && len(out) >= limit) || (!deadline.IsZero() && time.Now().After(deadline)) {
			complete = false
			return
		}
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			if !seen[dir] {
				seen[dir] = true
				out = append(out, dir)
				for _, f := range found {
					f(dir)
				}
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
	return out, complete
}

// Repo scans can take seconds on big or slow disks, so they run in their own
// background process (`roost folders --refresh`) with no time limit, saving
// progress to this cache as they go. The picker shows the cache and follows
// the scan live; closing the picker doesn't stop it.
func repoCachePath() string { return filepath.Join(homeDir(), "repos.json") }

type repoCache struct {
	Roots    []string  `json:"roots"`
	Depth    int       `json:"depth"`
	Repos    []string  `json:"repos"`
	Scanning bool      `json:"scanning"`
	PID      int       `json:"pid,omitempty"`
	Started  time.Time `json:"started"`
	Finished time.Time `json:"finished,omitzero"`
}

func readRepoCache() (repoCache, bool) {
	var c repoCache
	b, err := os.ReadFile(repoCachePath())
	if err != nil || json.Unmarshal(b, &c) != nil {
		return c, false
	}
	return c, true
}

// matches: the cache was made for these settings.
func (c repoCache) matches(roots []string, depth int) bool {
	return c.Depth == depth && strings.Join(c.Roots, "\x00") == strings.Join(roots, "\x00")
}

// running: a scan process is still working on it.
func (c repoCache) running() bool {
	if !c.Scanning || c.PID <= 0 {
		return false
	}
	if syscall.Kill(c.PID, 0) != nil {
		return false // crashed or killed: start a new scan
	}
	out, _ := exec.Command("ps", "-o", "args=", "-p", fmt.Sprint(c.PID)).Output()
	return strings.Contains(string(out), "folders --refresh") // pids get reused
}

func saveRepoCache(c repoCache) {
	if os.MkdirAll(homeDir(), 0o700) != nil {
		return
	}
	b, _ := json.Marshal(c)
	tmp := fmt.Sprintf("%s.%d.tmp", repoCachePath(), os.Getpid())
	if os.WriteFile(tmp, b, 0o600) == nil {
		os.Rename(tmp, repoCachePath())
	}
}

// refreshRepos scans with no limits, saving progress about twice a second.
func refreshRepos(roots []string, depth int, progress func(n int)) repoCache {
	c := repoCache{Roots: roots, Depth: depth, Scanning: true, PID: os.Getpid(), Started: time.Now()}
	if prev, ok := readRepoCache(); ok && prev.matches(roots, depth) {
		c.Repos = prev.Repos // keep showing the last results while rescanning
	}
	saveRepoCache(c)
	var found []string
	last := time.Now()
	repos, _ := scanRepos(roots, depth, 0, time.Time{}, func(d string) {
		found = append(found, d)
		if progress != nil {
			progress(len(found))
		}
		if time.Since(last) > 500*time.Millisecond {
			last = time.Now()
			c.Repos = mergeRepos(c.Repos, found)
			saveRepoCache(c)
		}
	})
	c.Repos, c.Scanning, c.Finished = repos, false, time.Now()
	saveRepoCache(c)
	return c
}

func mergeRepos(a, b []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, l := range [][]string{a, b} {
		for _, d := range l {
			if !seen[d] {
				seen[d] = true
				out = append(out, d)
			}
		}
	}
	sort.Strings(out)
	return out
}

// startRepoScan launches a background scan unless one is already running.
func startRepoScan(roots []string, depth int) {
	if c, ok := readRepoCache(); ok && c.running() && c.matches(roots, depth) {
		return
	}
	cmd := exec.Command(selfPath(), "folders", "--refresh")
	cmd.Env = os.Environ()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // outlives the picker
	if cmd.Start() == nil {
		cmd.Process.Release()
	}
}

// cmdFolders implements `roost folders`: scan now and report.
//
//	roost folders          scan, then print what was searched and found
//	roost folders --list   also list every repo
func cmdFolders(args []string) error {
	roots, depth := sourceRoots(), sourceDepth()
	if hasFlag(args, "--refresh") { // background worker started by the picker
		refreshRepos(roots, depth, nil)
		return nil
	}
	var t []string
	for _, r := range roots {
		t = append(t, tilde(r))
	}
	fmt.Printf("Code folders: %s\nDepth: %d levels\n", strings.Join(t, " "), depth)
	if len(roots) == 0 {
		fmt.Println("No code folders found: set them with roost setup --source-dirs \"~/Source\"")
		return nil
	}
	start := time.Now()
	c := refreshRepos(roots, depth, func(n int) {
		if term.IsTerminal(int(os.Stdout.Fd())) {
			fmt.Printf("\r  scanning… %d repos", n)
		}
	})
	fmt.Printf("\r  found %d repos in %s\n", len(c.Repos), time.Since(start).Round(time.Millisecond))
	if hasFlag(args, "--list") {
		for _, r := range c.Repos {
			fmt.Println("  " + tilde(r))
		}
	}
	return nil
}

// pickerFolder is one row in the picker.
type pickerFolder struct {
	Dir  string
	Tags []string // CLIs used here, most recent first
}

// pickerFolders lists the current folder, then recently used folders (newest
// first), then the rest of repos (from a repo scan).
func pickerFolders(cwd string, repos []string) []pickerFolder {
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
	for _, d := range repos {
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
