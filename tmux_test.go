package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The generated config, including the 🐓 roost menu, must load in tmux.
func TestConfLoadsInTmux(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("no tmux")
	}
	dir := t.TempDir()
	t.Setenv("ROOST_HOME", dir)
	if err := writeConf(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(confPath())
	if strings.Contains(string(b), "@ROOSTMENU@") || strings.Contains(string(b), "@BIN@") {
		t.Fatal("unsubstituted placeholder in config")
	}
	sock := "roost-test-" + filepath.Base(dir)
	out, err := exec.Command("tmux", "-L", sock, "-f", confPath(), "new-session", "-d", "-x", "100", "-y", "30").CombinedOutput()
	defer exec.Command("tmux", "-L", sock, "kill-server").Run()
	if err != nil {
		t.Fatalf("tmux: %v: %s", err, out)
	}
	// Config errors don't fail new-session; source it again to see them.
	if out, err := exec.Command("tmux", "-L", sock, "source-file", confPath()).CombinedOutput(); err != nil || len(out) > 0 {
		t.Fatalf("config errors: %v: %s", err, out)
	}
	keys, _ := exec.Command("tmux", "-L", sock, "list-keys", "-T", "root", "MouseDown1StatusLeft").CombinedOutput()
	if !strings.Contains(string(keys), "Detach") {
		t.Fatalf("menu not bound: %s", keys)
	}
}
