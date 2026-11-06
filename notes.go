package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Tab notes: a short line about what a tab is for, shown on a line above the
// tabs (several tabs in the same repo otherwise look alike).
//
//   - @roost_note (window option) is the note you set: Ctrl-Space N, the
//     right-click menu, clicking the note line, or `roost note "…"` (which an
//     AI CLI can run too). It's saved and comes back after a restart.
//   - @roost_title (window option) is Claude Code's own title for the
//     conversation, read from its transcript by the hook. It shows when the
//     tab has no note of its own. Other CLIs don't have one.

const noteUsage = `usage:
  roost note              show this tab's note
  roost note TEXT…        set it (an AI CLI can run this too)
  roost note --clear      remove it (Claude tabs fall back to Claude's title)
`

// maxNote caps notes (and titles) so a runaway paste can't flood the bar.
const maxNote = 200

func cmdNote(args []string) error {
	pane := os.Getenv("TMUX_PANE")
	if !insideRoost() || pane == "" {
		return fmt.Errorf("run this inside a roost tab")
	}
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	} else if len(args) == 1 && args[0] == "--clear" {
		if _, err := tmux("set-option", "-wu", "-t", pane, "@roost_note"); err != nil {
			return err
		}
		_, err := save()
		return err
	}
	if len(args) == 0 {
		out, err := tmux("display-message", "-p", "-t", pane, "#{@roost_note}"+sep+"#{@roost_title}")
		if err != nil {
			return err
		}
		f := splitFields(out)
		switch {
		case len(f) == 2 && f[0] != "":
			fmt.Println(f[0])
		case len(f) == 2 && f[1] != "":
			fmt.Println(f[1] + "   (Claude's title; set your own with: roost note \"…\")")
		default:
			fmt.Print("This tab has no note.\n\n" + noteUsage)
		}
		return nil
	}
	note := cleanNote(strings.Join(args, " "))
	if note == "" {
		return fmt.Errorf("empty note (to remove it: roost note --clear)")
	}
	if _, err := tmux("set-option", "-w", "-t", pane, "@roost_note", note); err != nil {
		return err
	}
	_, err := save()
	return err
}

// cleanNote makes text safe and tidy for one line of the status bar: control
// characters (newlines, escapes) become spaces, runs of spaces collapse, and
// it's cut to maxNote characters. "#[" gets a space so the status bar doesn't
// read it as a colour ("#[fg=red]") and swallow the rest of the note.
func cleanNote(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	s = strings.ReplaceAll(s, "#[", "# [")
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > maxNote {
		s = strings.TrimSpace(string(r[:maxNote-1])) + "…"
	}
	return s
}

// transcriptTitle returns the conversation's title from a Claude Code
// transcript: the one you gave it with /rename if any, else the title Claude
// Code generated. Claude Code re-appends these lines as the conversation goes
// on, so the last one wins.
func transcriptTitle(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 64<<10)
	var ai, custom string
	for {
		line, err := r.ReadSlice('\n')
		if err == bufio.ErrBufferFull {
			// A huge line (a big tool result). Title lines are short: skip it.
			for err == bufio.ErrBufferFull {
				_, err = r.ReadSlice('\n')
			}
			if err != nil {
				break
			}
			continue
		}
		if bytes.Contains(line, []byte(`-title"`)) {
			var e struct {
				Type        string `json:"type"`
				AITitle     string `json:"aiTitle"`
				CustomTitle string `json:"customTitle"`
			}
			if json.Unmarshal(line, &e) == nil {
				switch {
				case e.Type == "ai-title" && e.AITitle != "":
					ai = e.AITitle
				case e.Type == "custom-title" && e.CustomTitle != "":
					custom = e.CustomTitle
				}
			}
		}
		if err != nil {
			break
		}
	}
	if custom != "" {
		return custom
	}
	return ai
}

// claudeTranscript finds a session's transcript when the hook didn't say.
func claudeTranscript(session string) string {
	if !validSessionID(session) {
		return ""
	}
	m, _ := filepath.Glob(filepath.Join(claudeDir(), "projects", "*", session+".jsonl"))
	if len(m) == 0 {
		return ""
	}
	return m[0]
}

// updateTitle refreshes the tab's @roost_title from the Claude transcript
// (clearing it for a brand-new conversation that has no title yet).
func updateTitle(pane, transcript, session string) {
	if transcript == "" {
		transcript = claudeTranscript(session)
	}
	title := ""
	if transcript != "" {
		title = cleanNote(transcriptTitle(transcript))
	}
	cur, _ := tmux("display-message", "-p", "-t", pane, "#{@roost_title}")
	if title == cur {
		return
	}
	if title == "" {
		tmux("set-option", "-wu", "-t", pane, "@roost_title")
	} else {
		tmux("set-option", "-w", "-t", pane, "@roost_title", title)
	}
}

// noteAIContext is what Claude is told at the start of a session when
// @roost_note_ai is on, so it keeps the tab's note current by itself.
const noteAIContext = "This session runs in a roost terminal tab. roost shows a one-line note " +
	"above its tabs so the user can tell apart several tabs working in the same repo. " +
	"Keep this tab's note current: once the task is clear, and again whenever it " +
	"changes noticeably, run `roost note \"<3-6 word summary of the current task>\"` " +
	"(for example `roost note \"fix token refresh race\"`). Keep it short and specific; " +
	"don't mention doing this unless asked."

// printNoteAIContext writes the SessionStart hook output that adds
// noteAIContext to Claude's context.
func printNoteAIContext() {
	out := map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName":     "SessionStart",
		"additionalContext": noteAIContext,
	}}
	b, _ := json.Marshal(out)
	fmt.Println(string(b))
}

// ---- the note line ----

// The note line's contents: the tab's note, else Claude's title (dimmer),
// else a hint while @roost_hint is set (see refreshHints). Clicking it edits
// the note (range=user|note).
const noteLineFormat = `#[align=left range=user|note]#[fg=#{@roost_note_icon_color}] #{@roost_note_icon} ` +
	`#{?#{@roost_note},#[fg=#{@roost_note_color}]#{@roost_note},` +
	`#{?#{@roost_title},#[fg=#{@roost_title_color}]#{@roost_title},` +
	`#{?#{@roost_hint},#[fg=#{@roost_note_hint_color}]no note · click or Ctrl-Space N to add one,}}} #[norange default]`

// hintSeconds reads @roost_note_hint: how long a new tab shows the "add a
// note" hint. A number of seconds, "on" (always) or "off" (never). -1 means
// always.
func hintSeconds(v string) int64 {
	switch v = strings.TrimSpace(v); v {
	case "on", "always":
		return -1
	case "off", "never", "":
		return 0
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < 0 {
		return 30
	}
	return n
}

// showHint decides whether a tab first seen at born (unix seconds; 0 = a tab
// restored after a restart) still shows the hint at now.
func showHint(secs, born, now int64) bool {
	switch {
	case secs < 0:
		return true
	case secs == 0 || born == 0:
		return false
	}
	return now-born < secs
}

// refreshHints stamps new tabs with when roost first saw them (@roost_born)
// and sets @roost_hint on the ones still young enough to show the hint.
// Runs with every save (every few seconds, from the status bar).
func refreshHints() {
	secs := int64(30)
	if v, ok := tmuxOption("@roost_note_hint"); ok {
		secs = hintSeconds(v)
	}
	out, err := tmux("list-windows", "-t", sessionName, "-F", "#{window_id}"+sep+"#{@roost_born}"+sep+"#{@roost_hint}")
	if err != nil {
		return
	}
	now := time.Now().Unix()
	for _, line := range strings.Split(out, "\n") {
		p := splitFields(line)
		if len(p) != 3 {
			continue
		}
		born, err := strconv.ParseInt(p[1], 10, 64)
		if err != nil {
			born = now
			tmux("set-option", "-w", "-t", p[0], "@roost_born", strconv.FormatInt(now, 10))
		}
		show := showHint(secs, born, now)
		if show && p[2] != "1" {
			tmux("set-option", "-w", "-t", p[0], "@roost_hint", "1")
		} else if !show && p[2] != "" {
			tmux("set-option", "-wu", "-t", p[0], "@roost_hint")
		}
	}
}

// noteLineConf is the tmux config for the note line. tabLine is tmux's own
// default status line (the tabs); the note line takes its place on top and
// the tabs move to the line below. If tmux's default couldn't be read, the
// note line goes below the tabs instead.
func noteLineConf(tabLine string) string {
	var b strings.Builder
	b.WriteString("if -F \"#{!=:#{@roost_note_line},off}\" {\n  set -g status 2\n")
	if tabLine != "" && !strings.ContainsAny(tabLine, "'\n") {
		fmt.Fprintf(&b, "  set -g status-format[0] \"%s\"\n  set -g status-format[1] '%s'\n", noteLineFormat, tabLine)
	} else {
		b.WriteString("  set -gu status-format\n")
		fmt.Fprintf(&b, "  set -g status-format[1] \"%s\"\n", noteLineFormat)
	}
	b.WriteString("} {\n  set -g status on\n  set -gu status-format\n}\n")
	return b.String()
}

// defaultTabLine asks a throwaway tmux server for tmux's built-in status line
// format, so it can move down a line without copying it into roost (it
// differs between tmux versions).
func defaultTabLine() string {
	sock := "roost-probe-" + strconv.Itoa(os.Getpid())
	cmd := exec.Command("tmux", "-L", sock, "-f", "/dev/null", "start-server", ";", "show-options", "-gv", "status-format[0]")
	cmd.Env = envWithoutTMUX()
	out, err := cmd.Output()
	// start-server with no sessions exits on its own; make sure of it.
	kill := exec.Command("tmux", "-L", sock, "kill-server")
	kill.Env = envWithoutTMUX()
	_ = kill.Run()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// ---- branch prefix ----

// gitBranch returns the branch checked out at the repo root (or a short
// commit id when detached), read straight from .git so it costs no process.
// Worktrees and submodules have a .git file pointing at the real git dir.
func gitBranch(root string) string {
	if root == "" {
		return ""
	}
	gitPath := filepath.Join(root, ".git")
	fi, err := os.Stat(gitPath)
	if err != nil {
		return ""
	}
	dir := gitPath
	if !fi.IsDir() {
		b, err := os.ReadFile(gitPath)
		if err != nil {
			return ""
		}
		line := strings.TrimSpace(string(b))
		if !strings.HasPrefix(line, "gitdir:") {
			return ""
		}
		dir = strings.TrimSpace(strings.TrimPrefix(line, "gitdir:"))
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(root, dir)
		}
	}
	head, err := os.ReadFile(filepath.Join(dir, "HEAD"))
	if err != nil {
		return ""
	}
	h := strings.TrimSpace(string(head))
	if strings.HasPrefix(h, "ref:") {
		ref := strings.TrimSpace(strings.TrimPrefix(h, "ref:"))
		return strings.TrimPrefix(ref, "refs/heads/")
	}
	if len(h) >= 7 && strings.Trim(h, "0123456789abcdef") == "" {
		return h[:7]
	}
	return ""
}
