package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRepoRoot(t *testing.T) {
	home := t.TempDir()
	mk := func(p string) string {
		full := filepath.Join(home, p)
		if err := os.MkdirAll(full, 0o755); err != nil {
			t.Fatal(err)
		}
		return full
	}
	repo := mk("code/cluster-tooling")
	mk("code/cluster-tooling/.git")
	deep := mk("code/cluster-tooling/bootstrap/terraform")
	wt := mk("code/worktree/sub")
	if err := os.WriteFile(filepath.Join(home, "code/worktree/.git"), []byte("gitdir: x"), 0o644); err != nil {
		t.Fatal(err)
	}
	plain := mk("code/notes")
	mk(".git") // a dotfiles repo in home must not count

	cases := []struct{ dir, want string }{
		{repo, repo},
		{deep, repo},
		{wt, filepath.Join(home, "code/worktree")},
		{plain, ""},
		{home, ""},
		{filepath.Dir(home), ""},
		{"relative/path", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := repoRoot(c.dir, home); got != c.want {
			t.Errorf("repoRoot(%q) = %q, want %q", c.dir, got, c.want)
		}
	}
}
