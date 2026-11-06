package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCleanNote(t *testing.T) {
	cases := []struct{ in, want string }{
		{"  fix   token\nrefresh\t race ", "fix token refresh race"},
		{"issue #42", "issue #42"},
		{"a #[fg=red]b", "a # [fg=red]b"},
		{"\x1b[31mred", "[31mred"},
		{"", ""},
	}
	for _, c := range cases {
		if got := cleanNote(c.in); got != c.want {
			t.Errorf("cleanNote(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	long := cleanNote(strings.Repeat("é", maxNote+50))
	if n := len([]rune(long)); n != maxNote || !strings.HasSuffix(long, "…") {
		t.Errorf("long note: %d runes, suffix %q", n, string([]rune(long)[n-1:]))
	}
}

func TestTranscriptTitle(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, lines ...string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	huge := `{"type":"user","message":"` + strings.Repeat("x", 200<<10) + `-title\""}`
	ai := write("ai.jsonl",
		`{"type":"user","message":"talk about \"type\":\"ai-title\" here"}`,
		`{"type":"ai-title","aiTitle":"Old title","sessionId":"s"}`,
		huge,
		`{"type":"ai-title","aiTitle":"League selector toggle","sessionId":"s"}`,
	)
	if got := transcriptTitle(ai); got != "League selector toggle" {
		t.Errorf("ai title = %q", got)
	}
	custom := write("custom.jsonl",
		`{"type":"custom-title","customTitle":"my name","sessionId":"s"}`,
		`{"type":"ai-title","aiTitle":"generated","sessionId":"s"}`,
	)
	if got := transcriptTitle(custom); got != "my name" {
		t.Errorf("custom title = %q, want it to beat the generated one", got)
	}
	none := write("none.jsonl", `{"type":"user","message":"hi"}`)
	if got := transcriptTitle(none); got != "" {
		t.Errorf("no title = %q", got)
	}
	if got := transcriptTitle(filepath.Join(dir, "missing.jsonl")); got != "" {
		t.Errorf("missing file = %q", got)
	}
}

func TestGitBranch(t *testing.T) {
	dir := t.TempDir()
	mk := func(p, head string) string {
		root := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, ".git", "HEAD"), []byte(head), 0o644); err != nil {
			t.Fatal(err)
		}
		return root
	}
	main := mk("main", "ref: refs/heads/feature/tab-notes\n")
	detached := mk("detached", "0b1baef1234567890abcdef1234567890abcdef1\n")

	// A worktree: .git is a file pointing at the real git dir (relative here).
	wt := filepath.Join(dir, "wt")
	gitdir := filepath.Join(dir, "main", ".git", "worktrees", "wt")
	if err := os.MkdirAll(gitdir, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(gitdir, "HEAD"), []byte("ref: refs/heads/fix-it\n"), 0o644)
	os.MkdirAll(wt, 0o755)
	os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: ../main/.git/worktrees/wt\n"), 0o644)

	cases := []struct{ root, want string }{
		{main, "feature/tab-notes"},
		{detached, "0b1baef"},
		{wt, "fix-it"},
		{filepath.Join(dir, "nope"), ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := gitBranch(c.root); got != c.want {
			t.Errorf("gitBranch(%q) = %q, want %q", c.root, got, c.want)
		}
	}
}

func TestNoteLineConf(t *testing.T) {
	top := noteLineConf("#[align=left]#{W:#{T:window-status-format}}")
	if !strings.Contains(top, `status-format[0] "`+noteLineFormat) || !strings.Contains(top, `status-format[1] '#[align=left]`) {
		t.Errorf("note line should go on top, tabs below:\n%s", top)
	}
	// Without tmux's default (or one we can't quote), the note goes below.
	for _, tab := range []string{"", "it's"} {
		c := noteLineConf(tab)
		if !strings.Contains(c, `status-format[1] "`+noteLineFormat) || strings.Contains(c, "status-format[0]") {
			t.Errorf("noteLineConf(%q): note line should go below the tabs:\n%s", tab, c)
		}
	}
	if !strings.Contains(top, "set -g status on") {
		t.Error("@roost_note_line off should go back to one line")
	}
}

func TestHint(t *testing.T) {
	for v, want := range map[string]int64{"on": -1, "off": 0, "": 0, "45": 45, "0": 0, "junk": 30, "-3": 30} {
		if got := hintSeconds(v); got != want {
			t.Errorf("hintSeconds(%q) = %d, want %d", v, got, want)
		}
	}
	cases := []struct {
		secs, born, now int64
		want            bool
	}{
		{30, 1000, 1010, true},
		{30, 1000, 1030, false},
		{-1, 0, 5000, true}, // always, even restored tabs
		{30, 0, 5, false},   // restored after a restart
		{0, 1000, 1000, false},
	}
	for _, c := range cases {
		if got := showHint(c.secs, c.born, c.now); got != c.want {
			t.Errorf("showHint(%d, %d, %d) = %v, want %v", c.secs, c.born, c.now, got, c.want)
		}
	}
}
