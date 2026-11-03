package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func mkdirs(t *testing.T, root string, dirs ...string) {
	t.Helper()
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFindRepos(t *testing.T) {
	root := t.TempDir()
	mkdirs(t, root,
		"Github/org/api/.git",           // repo at depth 3
		"Github/org/api/sub/inner/.git", // nested: not descended into
		"Github/org/web/.git",
		"personal/notes/.git",
		"Github/org/web/node_modules/x/.git",
		"deep/a/b/c/d/far/.git", // deeper than the limit
		".hidden/secret/.git",
		"plain/folder",
	)
	got := findRepos([]string{root}, 4, 100, time.Now().Add(time.Second))
	want := []string{
		filepath.Join(root, "Github/org/api"),
		filepath.Join(root, "Github/org/web"),
		filepath.Join(root, "personal/notes"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v\nwant %v", got, want)
	}
	if n := len(findRepos([]string{root}, 4, 1, time.Now().Add(time.Second))); n != 1 {
		t.Errorf("limit not applied: %d", n)
	}
}

func TestRecordRecent(t *testing.T) {
	t.Setenv("ROOST_HOME", t.TempDir())
	recordRecent("/work/api", "claude")
	time.Sleep(10 * time.Millisecond)
	recordRecent("/work/api", "")
	recordRecent("/work/web", "factory")
	m := loadRecent()
	if len(m) != 2 || m["/work/api"].Used["claude"].IsZero() || m["/work/api"].Used["shell"].IsZero() {
		t.Fatalf("unexpected recent: %+v", m)
	}
	if fi, _ := os.Stat(recentPath()); fi.Mode().Perm() != 0o600 {
		t.Errorf("recent.json should be 0600, is %v", fi.Mode().Perm())
	}
}

func TestPickerChoicesAndEditors(t *testing.T) {
	c := pickerChoices()
	if c[len(c)-1] != shellChoice {
		t.Errorf("shell should be the last choice: %v", c)
	}
	var fresh Agent
	for _, a := range builtinAgents {
		if a.Name == "fresh" {
			fresh = a
		}
	}
	if !fresh.Editor || fresh.StartCmd("") != "fresh ." || fresh.ResumeCmd("") != "fresh ." {
		t.Errorf("fresh preset wrong: %+v", fresh)
	}
}

func TestScanUnlimitedAndCache(t *testing.T) {
	t.Setenv("ROOST_HOME", t.TempDir())
	root := t.TempDir()
	mkdirs(t, root, "a/.git", "b/c/.git", "b/d/e/.git")
	var seen []string
	repos, complete := scanRepos([]string{root}, 10, 0, time.Time{}, func(d string) { seen = append(seen, d) })
	if !complete || len(repos) != 3 || len(seen) != 3 {
		t.Fatalf("unlimited scan: %v %v %v", repos, complete, seen)
	}
	if _, complete := scanRepos([]string{root}, 10, 1, time.Time{}); complete {
		t.Error("a limited scan that hit its limit must report incomplete")
	}

	c := refreshRepos([]string{root}, 10, nil)
	got, ok := readRepoCache()
	if !ok || got.Scanning || got.Finished.IsZero() || len(got.Repos) != 3 || len(c.Repos) != 3 {
		t.Fatalf("cache after scan: %+v", got)
	}
	if !got.matches([]string{root}, 10) || got.matches([]string{root}, 4) || got.matches([]string{"/x"}, 10) {
		t.Error("cache must only match the settings it was made with")
	}
	if got.running() {
		t.Error("a finished scan isn't running")
	}
	stuck := repoCache{Scanning: true, PID: 999999} // crashed scan
	if stuck.running() {
		t.Error("a dead scan pid must not count as running")
	}
	if fi, _ := os.Stat(repoCachePath()); fi.Mode().Perm() != 0o600 {
		t.Errorf("repos.json mode %v", fi.Mode().Perm())
	}
}

// caseVariant returns dir with its last element upper-cased if the file system
// treats that as the same folder (macOS default), else skips the test.
func caseVariant(t *testing.T, dir string) string {
	t.Helper()
	v := filepath.Join(filepath.Dir(dir), strings.ToUpper(filepath.Base(dir)))
	if v == dir {
		t.Fatalf("%s has no lower-case letters", dir)
	}
	if fi, err := os.Stat(v); err != nil || !fi.IsDir() {
		t.Skip("case-sensitive file system")
	}
	return v
}

func TestExistingRootsSameFolderOnce(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "source")
	mkdirs(t, base, "source", "other")
	other := filepath.Join(base, "other")
	link := filepath.Join(base, "link")
	if err := os.Symlink(src, link); err != nil {
		t.Fatal(err)
	}
	want := existingRoots([]string{src})
	if got := existingRoots([]string{src, link, other, filepath.Join(base, "missing")}); !reflect.DeepEqual(got, append(want, canonical(other))) {
		t.Errorf("symlink/missing: got %v", got)
	}

	upper := caseVariant(t, src)
	got := existingRoots([]string{upper, src, other})
	if len(got) != 2 || dirKey(got[0]) != dirKey(src) {
		t.Fatalf("~/Source and ~/source must count once: got %v", got)
	}
	if !strings.HasSuffix(got[0], "SOURCE") {
		t.Errorf("first spelling should win: got %v", got)
	}
}

func TestCaseVariantsListedOnce(t *testing.T) {
	t.Setenv("ROOST_HOME", t.TempDir())
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	base := t.TempDir()
	mkdirs(t, base, "source/api/.git", "source/web/.git")
	src := canonical(filepath.Join(base, "source"))
	upper := caseVariant(t, src)

	repos := findRepos([]string{src, upper}, 3, 0, time.Time{})
	if len(repos) != 2 {
		t.Fatalf("each repo once across case variants: got %v", repos)
	}

	recordRecent(filepath.Join(upper, "api"), "claude")
	var dirs []string
	for _, f := range pickerFolders(filepath.Join(upper, "web"), repos) {
		dirs = append(dirs, f.Dir)
	}
	if len(dirs) != 2 {
		t.Errorf("picker must list each folder once: got %v", dirs)
	}
}
