package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSettingsRoundTrip(t *testing.T) {
	t.Setenv("ROOST_HOME", t.TempDir())
	if err := writeSetting("@roost_default_agent", "claude"); err != nil {
		t.Fatal(err)
	}
	writeSetting("@roost_source_dirs", `~/Source ~/my "odd" $dir`)
	writeSetting("@roost_default_agent", "pi") // overwrite, keep the other
	got := readSettings()
	if got["@roost_default_agent"] != "pi" {
		t.Errorf("agent = %q", got["@roost_default_agent"])
	}
	if got["@roost_source_dirs"] != `~/Source ~/my "odd" $dir` {
		t.Errorf("dirs = %q (escaping must round-trip across rewrites)", got["@roost_source_dirs"])
	}
	if configuredAgent(settingsConfPath()) != "pi" {
		t.Error("configuredAgent can't read the new format")
	}
	if fi, _ := os.Stat(settingsConfPath()); fi.Mode().Perm() != 0o600 {
		t.Errorf("settings.conf mode %v", fi.Mode().Perm())
	}
}

func TestPendingQuestions(t *testing.T) {
	t.Setenv("ROOST_HOME", t.TempDir())
	if len(pendingQuestions()) != len(setupQuestions) {
		t.Fatal("fresh install should have every question pending")
	}
	writeSetting("@roost_default_agent", "claude") // an existing user from before
	if p := pendingSummary(); p != "where your code lives" {
		t.Errorf("pending = %q", p)
	}
	// Set by hand in custom.conf counts as answered.
	os.WriteFile(customConfPath(), []byte("set -g @roost_source_dirs '~/work'\n"), 0o600)
	if p := pendingSummary(); p != "" {
		t.Errorf("custom.conf setting not counted: %q", p)
	}
}

func TestSuggestSourceDirs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	mkdirs(t, home,
		"Source/Github/org/a/.git", "Source/Github/org/b/.git", "Source/Github/me/c/.git",
		"work/x/.git",
		"Library/Caches/thing/.git", // skipped
		"deep/1/2/3/4/5/r/.git",     // 6 levels below ~/deep
	)
	dirs, depth := suggestSourceDirs(2 * time.Second)
	if len(dirs) < 2 || dirs[0] != filepath.Join(home, "Source") {
		t.Fatalf("dirs = %v", dirs)
	}
	for _, d := range dirs {
		if filepath.Base(d) == "Library" {
			t.Error("Library should be skipped")
		}
	}
	if depth != 6 {
		t.Errorf("depth = %d, want 6 (deepest repo)", depth)
	}
}
