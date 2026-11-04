package main

import (
	"os"
	"path/filepath"
	"strings"
)

// Tabs are named after the git repo they're in, not just the folder: a tab in
// ~/code/cluster-tooling/bootstrap is called "cluster-tooling", and the status
// bar adds a dim "› bootstrap" so you can see it's in a subfolder.
//
// tmux can't look for a .git folder itself, so roost stores each pane's repo
// root in the pane option @roost_root, and the tmux formats (see tmux.go)
// check that the pane is still inside it. Leaving the repo falls back to the
// folder name at once; entering a new repo picks up its name on the next save
// (every few seconds, from the status bar).

// repoRoot returns the nearest folder at or above dir that holds a .git
// folder or file (worktrees and submodules use a file), or "" if there's none.
// Your home folder and anything above it never count: a dotfiles repo in ~
// would otherwise name every tab that isn't in some other repo.
func repoRoot(dir, home string) string {
	if dir == "" || !filepath.IsAbs(dir) {
		return ""
	}
	dir = filepath.Clean(dir)
	home = filepath.Clean(home)
	for {
		if home != "" && home != "." && (dir == home || !strings.HasPrefix(dir, home+string(filepath.Separator))) {
			return ""
		}
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// setPaneRoot records pane's repo root for the folder it's in, if it changed.
func setPaneRoot(pane, dir, current string) {
	h, _ := os.UserHomeDir()
	root := repoRoot(dir, h)
	if root == current {
		return
	}
	if root == "" {
		tmux("set-option", "-pu", "-t", pane, "@roost_root")
	} else {
		tmux("set-option", "-p", "-t", pane, "@roost_root", root)
	}
}

// refreshRoots updates @roost_root for every pane in roost.
func refreshRoots() {
	f := strings.Join([]string{"#{pane_id}", "#{?#{@roost_cwd},#{@roost_cwd},#{pane_current_path}}", "#{@roost_root}"}, sep)
	out, err := tmux("list-panes", "-s", "-t", sessionName, "-F", f)
	if err != nil {
		return
	}
	for _, line := range strings.Split(out, "\n") {
		if p := splitFields(line); len(p) == 3 {
			setPaneRoot(p[0], p[1], p[2])
		}
	}
}
