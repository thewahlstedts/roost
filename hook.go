package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// runHook is called by Claude Code hooks (see `roost setup`). It tags the
// tmux pane with the Claude session id so restores resume the right
// conversation, and sets the little status dot on the tab.
func runHook() {
	data, _ := io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
	pane := os.Getenv("TMUX_PANE")
	if !insideRoost() || pane == "" {
		return // not running inside roost: do nothing, stay fast
	}
	var ev struct {
		Event   string `json:"hook_event_name"`
		Session string `json:"session_id"`
		Reason  string `json:"reason"`
		Message string `json:"message"`
		Cwd     string `json:"cwd"`
	}
	if json.Unmarshal(data, &ev) != nil {
		return
	}
	setState := func(s string) { tmux("set-option", "-w", "-t", pane, "@roost_state", s) }

	// Name the tab after the folder Claude is working in.
	if ev.Cwd != "" && ev.Event != "SessionEnd" {
		tmux("set-option", "-p", "-t", pane, "@roost_dir", filepath.Base(ev.Cwd))
	}

	switch ev.Event {
	case "SessionStart":
		if validSessionID(ev.Session) {
			tmux("set-option", "-p", "-t", pane, "@roost_session", ev.Session)
		}
		tmux("set-option", "-p", "-t", pane, "@roost_agent", "claude")
		setState("idle")
		save()
	case "UserPromptSubmit", "PostToolUse":
		setState("working")
	case "PermissionRequest":
		setState("waiting")
	case "Notification":
		// The 60s "waiting for your input" nudge just means Claude is done.
		if strings.Contains(strings.ToLower(ev.Message), "waiting for your input") {
			return
		}
		setState("waiting")
	case "Stop":
		// If you're looking at this tab right now, no need for a dot.
		if v, _ := tmux("display-message", "-p", "-t", pane, "#{&&:#{window_active},#{session_attached}}"); v == "1" {
			setState("idle")
		} else {
			setState("done")
		}
	case "SessionEnd":
		// Only forget the session when you deliberately quit Claude. A
		// reboot/kill must NOT clear it, or we couldn't resume it later.
		if ev.Reason == "prompt_input_exit" || ev.Reason == "logout" {
			tmux("set-option", "-pu", "-t", pane, "@roost_session")
			tmux("set-option", "-pu", "-t", pane, "@roost_agent")
			tmux("set-option", "-wu", "-t", pane, "@roost_state")
			tmux("set-option", "-pu", "-t", pane, "@roost_dir")
			save()
		}
	}
}
