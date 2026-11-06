package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

const sessionName = "roost"

// socketName is the private tmux server roost uses (overridable for testing).
var socketName = func() string {
	if s := os.Getenv("ROOST_SOCKET"); s != "" {
		return s
	}
	return "roost"
}()

// homeDir is where roost keeps its config and saved state.
func homeDir() string {
	if d := os.Getenv("ROOST_HOME"); d != "" {
		return d
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".roost")
}

func confPath() string { return filepath.Join(homeDir(), "tmux.conf") }

// tmux runs a tmux command against roost's private server.
func tmux(args ...string) (string, error) {
	full := append([]string{"-L", socketName, "-f", confPath()}, args...)
	cmd := exec.Command("tmux", full...)
	cmd.Env = envWithoutTMUX()
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err := cmd.Run()
	if err != nil {
		return out.String(), fmt.Errorf("tmux %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return strings.TrimRight(out.String(), "\n"), nil
}

func envWithoutTMUX() []string {
	var env []string
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "TMUX=") {
			env = append(env, e)
		}
	}
	return env
}

func serverRunning() bool {
	_, err := tmux("has-session", "-t", sessionName)
	return err == nil
}

// insideRoost reports whether the current process runs inside a roost pane.
func insideRoost() bool {
	t := os.Getenv("TMUX")
	if t == "" {
		return false
	}
	return filepath.Base(strings.SplitN(t, ",", 2)[0]) == socketName
}

func selfPath() string {
	p, err := os.Executable()
	if err != nil {
		return "roost"
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// writeConf (re)generates the tmux config every time roost starts, so
// upgrades to roost take effect without any manual steps.
func writeConf() error {
	if err := checkTmuxVersion(); err != nil {
		return err
	}
	if err := os.MkdirAll(homeDir(), 0o700); err != nil {
		return err
	}
	// Tighten folders created by older versions (they hold paths + session ids).
	os.Chmod(homeDir(), 0o700)
	os.Chmod(historyDir(), 0o700)
	bin := shellQuote(selfPath())
	conf := strings.ReplaceAll(confTemplate, "@BIN@", bin)
	conf = strings.ReplaceAll(conf, "@VERSION@", versionLabel())
	conf = strings.ReplaceAll(conf, "@SETTINGS@", settingsConfPath())
	conf = strings.ReplaceAll(conf, "@CUSTOM@", filepath.Join(homeDir(), "custom.conf"))
	conf = strings.ReplaceAll(conf, "@COPY@", copyAction())
	conf = strings.ReplaceAll(conf, "@CLIPBOARD@", clipboardCmd())
	conf = strings.ReplaceAll(conf, "@NOTELINE@", noteLineConf(defaultTabLine()))
	return os.WriteFile(confPath(), []byte(conf), 0o600)
}

// The oldest tmux roost's config works with (display-popup -T,
// popup-border-lines and allow-passthrough arrived in 3.3).
const minTmuxMajor, minTmuxMinor = 3, 3

func checkTmuxVersion() error {
	out, err := exec.Command("tmux", "-V").Output()
	if err != nil {
		return fmt.Errorf("tmux not found: install tmux %d.%d or newer", minTmuxMajor, minTmuxMinor)
	}
	if v := strings.TrimSpace(string(out)); !tmuxVersionOK(v) {
		return fmt.Errorf("roost needs tmux %d.%d or newer (found %s)", minTmuxMajor, minTmuxMinor, v)
	}
	return nil
}

// tmuxVersionOK parses `tmux -V` output like "tmux 3.4", "tmux 3.3a",
// "tmux next-3.6" or "tmux master". Unknown formats are given the benefit
// of the doubt.
var tmuxVerRe = regexp.MustCompile(`(\d+)\.(\d+)`)

func tmuxVersionOK(v string) bool {
	m := tmuxVerRe.FindStringSubmatch(v)
	if m == nil {
		return true
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	return major > minTmuxMajor || (major == minTmuxMajor && minor >= minTmuxMinor)
}

// cmdTabDrop moves tab FROM to where tab TO is (after it when dragged
// right, before it when dragged left), keeping FROM selected.
func cmdTabDrop(args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: roost tab-drop FROM TO")
	}
	from, err1 := strconv.Atoi(args[0])
	to, err2 := strconv.Atoi(args[1])
	if err1 != nil || err2 != nil || from == to {
		return nil
	}
	where := "-a"
	if to < from {
		where = "-b"
	}
	src := fmt.Sprintf("%s:=%d", sessionName, from)
	dst := fmt.Sprintf("%s:=%d", sessionName, to)
	_, err := tmux("move-window", where, "-s", src, "-t", dst)
	return err
}

// versionLabel is what the status bar shows next to "roost": "v0.3.1", or
// "dev" for local builds.
func versionLabel() string {
	if _, ok := parseVersion(version); ok {
		return "v" + strings.TrimPrefix(version, "v")
	}
	return "dev"
}

// clipboardCmd is the system clipboard tool for this machine ("pbcopy",
// "wl-copy", "xclip -selection clipboard"…), or "" if there's none.
func clipboardCmd() string {
	var candidates [][]string
	if runtime.GOOS == "darwin" {
		candidates = [][]string{{"pbcopy"}}
	} else {
		if os.Getenv("WAYLAND_DISPLAY") != "" {
			candidates = append(candidates, []string{"wl-copy"})
		}
		if os.Getenv("DISPLAY") != "" {
			candidates = append(candidates,
				[]string{"xclip", "-selection", "clipboard"},
				[]string{"xsel", "--clipboard", "--input"})
		}
	}
	for _, c := range candidates {
		if _, err := exec.LookPath(c[0]); err == nil {
			return strings.Join(c, " ")
		}
	}
	return ""
}

// copyAction is the copy-mode action for a mouse selection: pipe it to the
// system clipboard tool if there is one, else let tmux send it to the
// terminal (OSC 52, enabled by set-clipboard on), which most terminals and
// SSH sessions accept.
func copyAction() string {
	if c := clipboardCmd(); c != "" {
		return `copy-pipe-and-cancel "` + c + `"`
	}
	return "copy-selection-and-cancel"
}

const confTemplate = `# Generated by roost on every start. Put your own tweaks in @CUSTOM@
# ---- terminal behaviour (good defaults for Claude Code) ----
set -g default-terminal "tmux-256color"
# Reset first so reloading the config doesn't pile up duplicates.
set -su terminal-features
set -as terminal-features ",xterm*:RGB:extkeys:clipboard:hyperlinks:usstyle"
set -s extended-keys on
set -sq extended-keys-format csi-u
set -s escape-time 0
set -s set-clipboard on
set -g focus-events on
# Don't let programs in panes talk to iTerm directly: it confuses iTerm
# (bogus "mouse reporting left on" / "ssh session ended" prompts).
set -gq allow-passthrough off
# Panes shouldn't claim to be the iTerm session that first started roost.
set-environment -gu ITERM_SESSION_ID
set-environment -gu ITERM_PROFILE
set -g history-limit 100000
set -g mouse on
set -g base-index 1
setw -g pane-base-index 1
set -g renumber-windows on
setw -g automatic-rename on
# Tabs are named after their git repo (@roost_root, which roost fills in), or
# else their folder. Claude tabs use the folder Claude reports (@roost_cwd):
# it can cd without its process moving.
set -g @roost_path "#{?#{@roost_cwd},#{@roost_cwd},#{pane_current_path}}"
set -g @roost_folder "#{?#{@roost_cwd},#{b:@roost_cwd},#{b:pane_current_path}}"
set -g @roost_in_repo "#{&&:#{@roost_root},#{||:#{==:#{@roost_root},#{E:@roost_path}},#{m:#{@roost_root}/*,#{E:@roost_path}}}}"
# When two or more tabs are in the same repo, each name starts with its
# branch ("tab-notes · roost"); roost fills in @roost_branch and @roost_dup.
set -g @roost_branch_prefix on
setw -g automatic-rename-format "#{?#{&&:#{!=:#{@roost_branch_prefix},off},#{&&:#{@roost_dup},#{&&:#{@roost_branch},#{E:@roost_in_repo}}}},#{@roost_branch} · ,}#{?#{E:@roost_in_repo},#{b:@roost_root},#{?#{@roost_dir},#{@roost_dir},#{b:pane_current_path}}}"
set -g allow-rename off
set -g set-titles on
set -g set-titles-string "#W · roost"
set -g display-time 1500
set -g popup-border-lines rounded

# ---- one prefix: Ctrl-Space ----
unbind C-b
set -g prefix C-Space
bind C-Space send-prefix

# Start from a clean slate so only the keys below exist.
unbind -a -T prefix
bind C-Space send-prefix
bind n   display-popup -E -w 80% -h 70% -T " New tab " -d "#{pane_current_path}" "@BIN@ pick"
bind t   new-window -c "#{pane_current_path}"
bind r   command-prompt -I "#W" -p "Rename tab:" "rename-window -- \"%%%\""
bind N   command-prompt -I "#{@roost_note}" -p "Tab note:" "set -w @roost_note \"%%%\""
bind x   confirm-before -p "Close tab '#W'? (y/n)" kill-window
bind d   detach-client
bind s   run -b "@BIN@ save" \; display-message "roost: saved"
# (popups don't expand formats in their command; run-shell does)
bind c   run-shell -b "@BIN@ check --popup --pane #{pane_id} --client #{client_name}"
bind ?   display-popup -E -w 72 -h 29 -T " 🐓 roost " -d "#{pane_current_path}" "@BIN@ keys --wait"
bind Left  previous-window
bind Right next-window
# Move the current tab left/right (stays selected; stops at the ends).
bind <   if -F "#{window_start_flag}" "" "swap-window -d -t :-1"
bind >   if -F "#{window_end_flag}" "" "swap-window -d -t :+1"
bind |   split-window -h -c "#{pane_current_path}"
bind -   split-window -v -c "#{pane_current_path}"
bind o   select-pane -t :.+
bind z   resize-pane -Z
bind [   copy-mode
bind 1 select-window -t :=1
bind 2 select-window -t :=2
bind 3 select-window -t :=3
bind 4 select-window -t :=4
bind 5 select-window -t :=5
bind 6 select-window -t :=6
bind 7 select-window -t :=7
bind 8 select-window -t :=8
bind 9 select-window -t :=9

# Cmd+Left / Cmd+Right: the "roost" iTerm profile sends these codes.
set -s user-keys[0] "\e[9001~"
set -s user-keys[1] "\e[9002~"
set -s user-keys[2] "\e[9003~"
set -s user-keys[3] "\e[9004~"
bind -n User0 previous-window
bind -n User1 next-window
# Cmd+Shift+Left / Cmd+Shift+Right: move the current tab.
bind -n User2 if -F "#{window_start_flag}" "" "swap-window -d -t :-1"
bind -n User3 if -F "#{window_end_flag}" "" "swap-window -d -t :+1"

# No-prefix shortcuts (need iTerm: Profiles > Keys > Left Option key = Esc+)
bind -n M-Left  previous-window
bind -n M-Right next-window
bind -n M-S-Left  if -F "#{window_start_flag}" "" "swap-window -d -t :-1"
bind -n M-S-Right if -F "#{window_end_flag}" "" "swap-window -d -t :+1"
bind -n M-n display-popup -E -w 80% -h 70% -T " New tab " -d "#{pane_current_path}" "@BIN@ pick"
bind -n M-1 select-window -t :=1
bind -n M-2 select-window -t :=2
bind -n M-3 select-window -t :=3
bind -n M-4 select-window -t :=4
bind -n M-5 select-window -t :=5
bind -n M-6 select-window -t :=6
bind -n M-7 select-window -t :=7
bind -n M-8 select-window -t :=8
bind -n M-9 select-window -t :=9

# ---- mouse ----
bind -n MouseDown1Status if -F "#{==:#{mouse_status_range},new}" { display-popup -E -w 80% -h 70% -T " New tab " -d "#{pane_current_path}" "@BIN@ pick" } { if -F "#{==:#{mouse_status_range},help}" { display-popup -E -w 72 -h 29 -T " 🐓 roost " -d "#{pane_current_path}" "@BIN@ keys --wait" } { if -F "#{==:#{mouse_status_range},note}" { command-prompt -I "#{@roost_note}" -p "Tab note:" "set -w @roost_note \"%%%\"" } { select-window -t = } } }
# Drag a tab along the bar and drop it to move it there. tmux reports the
# drag-start tab for every drag update, so act on the drop: pressing already
# selected (made active) the dragged tab, and "=" is the tab under the
# pointer. roost tab-drop does the move with both tabs named explicitly.
bind -n MouseDragEnd1Status run-shell -t = "@BIN@ tab-drop #{active_window_index} #{window_index}"
# Dropped past the last tab: move to the end. On the 🐓 label: to the start.
bind -n MouseDragEnd1StatusDefault move-window -a -t :$
bind -n MouseDragEnd1StatusLeft move-window -b -t :^
# Right-click a tab: a small menu (it also selects the tab, so every item
# acts on the tab you clicked).
bind -n MouseDown3Status select-window -t = \; display-menu -x W -y W -T "#[align=centre] #W " \
  "#{?window_start_flag,-,}Move left"   "<" { swap-window -d -t :-1 } \
  "#{?window_end_flag,-,}Move right"    ">" { swap-window -d -t :+1 } \
  "#{?window_start_flag,-,}Move to start" "^" { move-window -b -t :^ } \
  "#{?window_end_flag,-,}Move to end"   "$" { move-window -a -t :$ } \
  "" \
  "Check output" c { run-shell -b "@BIN@ check --popup --pane #{pane_id} --client #{client_name}" } \
  "Rename"  r { command-prompt -I "#W" -p "Rename tab:" "rename-window -- \"%%%\"" } \
  "Note"    N { command-prompt -I "#{@roost_note}" -p "Tab note:" "set -w @roost_note \"%%%\"" } \
  "Close"   x { confirm-before -p "Close tab '#W'? (y/n)" kill-window }
bind -n DoubleClick1Status command-prompt -I "#W" -p "Rename tab:" "rename-window -t = -- \"%%%\""
# tmux's own double-/triple-click bindings (select a word / line) copy with
# a bare copy-pipe, which uses copy-command: point it at the clipboard too,
# or they'd only reach the clipboard if the terminal allows OSC 52 (iTerm
# doesn't by default).
set -s copy-command "@CLIPBOARD@"
bind -T copy-mode MouseDragEnd1Pane send -X @COPY@
bind -T copy-mode-vi MouseDragEnd1Pane send -X @COPY@

# ---- look ----
# Theme colours (override any in custom.conf). Any tmux colour works:
# a name (green), colourN, or #rrggbb.
set -g @roost_accent "#e0473a"
set -g @roost_waiting_color colour203
set -g @roost_working_color colour221
set -g @roost_done_color colour114
set -g @roost_new_color colour114
set -g @roost_tab_color colour246
set -g @roost_current_tab_color colour255
set -g @roost_current_tab_bg colour238
# CLI for new tabs (claude, pi, codex, grok, or your own; see README "Other CLIs").
# (Not "@roost_agent": that's the per-tab tag, and a global of the same name
# would leak into every tab through tmux's option inheritance.)
set -g @roost_default_agent claude
set -g status-position bottom
set -g status-interval 5
set -g status-justify left
set -g status-style "bg=colour235,fg=colour246"
set -g message-style "bg=colour237,fg=colour223"
# Splits: heavy dividers, the active one in the accent colour, and a label
# with each split's folder (shown only while a tab is actually split).
# Line style is the tmux option pane-border-lines: single|double|heavy|simple|number|spaces.
set -g @roost_split_color colour244
set -g @roost_split_labels on
set -g pane-border-lines heavy
set -g pane-border-style "fg=#{@roost_split_color}"
set -g pane-active-border-style "fg=#{@roost_accent}"
set -g pane-border-format "#{?pane_active,#[fg=#{@roost_accent} bold],#[fg=#{@roost_split_color}]} #{?#{@roost_dir},#{@roost_dir},#{b:pane_current_path}}#{?#{@roost_agent}, · #{@roost_agent},} "
set-hook -g window-layout-changed "if -F '#{&&:#{>:#{window_panes},1},#{!=:#{@roost_split_labels},off}}' 'set -w pane-border-status top' 'set -w pane-border-status off'"
set -g @roost_version "@VERSION@"
set -g @roost_version_color colour242
set -g status-left-length 32
set -g status-left "#[fg=#{@roost_accent},bold] 🐓 roost #[nobold,fg=#{@roost_version_color}]#{@roost_version} #[default]"
# Tab markers: which tabs run Claude (a pane with @roost_session) and which don't.
# Override in custom.conf. @roost_marker: shell (default) | claude | both | none
set -g @roost_marker shell
set -g @roost_shell_marker '$'
set -g @roost_claude_marker '✳'
# Any tmux colour: a name (green), colourN, or #rrggbb.
set -g @roost_shell_marker_color colour242
set -g @roost_claude_marker_color colour242
set -g @roost_tab_marker "#{?#{||:#{@roost_agent},#{@roost_session}},#{?#{||:#{==:#{@roost_marker},claude},#{==:#{@roost_marker},both}},#[fg=#{@roost_claude_marker_color}]#{@roost_claude_marker} ,},#{?#{||:#{==:#{@roost_marker},shell},#{==:#{@roost_marker},both}},#[fg=#{@roost_shell_marker_color}]#{@roost_shell_marker} ,}}"
# Check markers (after Claude finishes a turn, with @roost_checks on).
set -g @roost_checks off
set -g @roost_check_timeout 600
set -g @roost_check_marker "#{?#{==:#{@roost_check},pass},#[fg=#{@roost_done_color}]✓ ,#{?#{==:#{@roost_check},fail},#[fg=#{@roost_waiting_color}]✗ ,#{?#{==:#{@roost_check},running},#[fg=colour242]◌ ,#{?#{==:#{@roost_check},untrusted},#[fg=colour242]? ,}}}}"
# Subfolder: a tab in a repo's subfolder shows it after the name, dimmed
# ("cluster-tooling › bootstrap"), renamed tabs too. @roost_subdir on|off
set -g @roost_subdir on
set -g @roost_subdir_color colour244
set -g @roost_subdir_marker "#{?#{&&:#{!=:#{@roost_subdir},off},#{&&:#{E:@roost_in_repo},#{!=:#{E:@roost_path},#{@roost_root}}}},#[nobold]#[fg=#{@roost_subdir_color}] › #{E:@roost_folder},}"
# Note line: a line above the tabs with the current tab's note (Ctrl-Space N,
# or roost note "…"), else Claude's title for the conversation. @roost_note_line on|off
set -g @roost_note_line on
set -g @roost_note_icon "✎"
set -g @roost_note_icon_color colour242
set -g @roost_note_color colour252
set -g @roost_title_color colour246
set -g @roost_note_hint_color colour240
# The "add a note" hint: seconds a new tab shows it, on (always) or off (never).
set -g @roost_note_hint 30
# Tell Claude (at session start) to keep the note current itself. @roost_note_ai on|off
set -g @roost_note_ai off
set -g window-status-separator "#[fg=colour239]│"
set -g window-status-format " #{?#{==:#{@roost_state},waiting},#[fg=#{@roost_waiting_color}]● ,#{?#{==:#{@roost_state},working},#[fg=#{@roost_working_color}]● ,#{?#{==:#{@roost_state},done},#[fg=#{@roost_done_color}]● ,}}}#{E:@roost_check_marker}#{E:@roost_tab_marker}#[fg=#{@roost_tab_color}]#I #W#{E:@roost_subdir_marker} "
set -g window-status-current-format "#[bg=#{@roost_current_tab_bg},fg=#{@roost_current_tab_color},bold] #{?#{==:#{@roost_state},waiting},#[fg=#{@roost_waiting_color}]● ,#{?#{==:#{@roost_state},working},#[fg=#{@roost_working_color}]● ,}}#{E:@roost_check_marker}#{E:@roost_tab_marker}#[fg=#{@roost_current_tab_color}]#I #W#{E:@roost_subdir_marker} "
set -g status-right-length 60
set -g status-right "#(@BIN@ save --quiet)#[range=user|new]#[fg=#{@roost_new_color},bold] + new #[norange default]#[range=user|help]#[fg=colour242] ? help #[norange default]"

# Seeing a finished tab clears its green dot.
set-hook -g session-window-changed "if -F '#{==:#{@roost_state},done}' 'set -w @roost_state idle'"
set-hook -g window-renamed "run -b '@BIN@ save --quiet'"
set-hook -g after-new-window "run -b '@BIN@ save --quiet'"

source-file -q @SETTINGS@
source-file -q @CUSTOM@

# ---- note line (after your settings, so @roost_note_line off applies) ----
@NOTELINE@`

// refreshSplitLabels applies @roost_split_labels to tabs that are already
// split (the hook only runs when a layout changes).
func refreshSplitLabels() {
	out, err := tmux("list-windows", "-a", "-F", "#{window_id} #{window_panes}")
	if err != nil {
		return
	}
	on := true
	if v, _ := tmux("show-options", "-gqv", "@roost_split_labels"); strings.TrimSpace(v) == "off" {
		on = false
	}
	for _, line := range strings.Split(out, "\n") {
		var id string
		var panes int
		if _, err := fmt.Sscanf(line, "%s %d", &id, &panes); err != nil {
			continue
		}
		pos := "off"
		if on && panes > 1 {
			pos = "top"
		}
		tmux("set-option", "-w", "-t", id, "pane-border-status", pos)
	}
}
