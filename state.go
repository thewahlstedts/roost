package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Pane struct {
	Dir     string `json:"dir"`
	Agent   string `json:"agent,omitempty"`   // CLI running in this pane (claude, pi, …); "" = shell
	Session string `json:"session,omitempty"` // that CLI's session id, when it supports one
	Claude  string `json:"claude,omitempty"`  // legacy: Claude session id (pre-agents state files)
	Active  bool   `json:"active,omitempty"`
}

type Window struct {
	Name   string `json:"name"`
	Custom bool   `json:"custom,omitempty"` // renamed by hand; otherwise follows the folder
	Layout string `json:"layout,omitempty"`
	Active bool   `json:"active,omitempty"`
	Panes  []Pane `json:"panes"`
}

type State struct {
	SavedAt time.Time `json:"saved_at"`
	Windows []Window  `json:"windows"`
}

func statePath() string  { return filepath.Join(homeDir(), "state.json") }
func historyDir() string { return filepath.Join(homeDir(), "history") }

const sep = "\x1f"

// snapshot reads the live layout from the running tmux server.
func snapshot() (*State, error) {
	f := strings.Join([]string{
		"#{window_index}", "#{window_name}", "#{window_active}", "#{window_layout}",
		"#{pane_current_path}", "#{pane_active}", "#{@roost_session}", "#{automatic-rename}",
		"#{@roost_agent}", "#{pane_current_command}", "#{pane_id}", "#{@roost_started}",
	}, sep)
	out, err := tmux("list-panes", "-s", "-t", sessionName, "-F", f)
	if err != nil {
		return nil, err
	}
	st := &State{SavedAt: time.Now()}
	byIdx := map[int]*Window{}
	var order []int
	for _, line := range strings.Split(out, "\n") {
		p := strings.Split(line, sep)
		if len(p) != 12 {
			continue
		}
		idx, _ := strconv.Atoi(p[0])
		w, ok := byIdx[idx]
		if !ok {
			w = &Window{Name: p[1], Active: p[2] == "1", Layout: p[3], Custom: p[7] == "0"}
			byIdx[idx] = w
			order = append(order, idx)
		}
		pane := Pane{Dir: p[4], Active: p[5] == "1", Agent: p[8], Session: p[6]}
		if pane.Agent == "" && pane.Session != "" {
			pane.Agent = "claude" // tagged by the Claude hook / older roost
		}
		// Only Claude tells us (via its hook) when you quit it. For other CLIs,
		// a pane that's back at a shell prompt means the CLI has exited.
		// (Ignore the first seconds after launch, before the CLI has started.)
		started, _ := strconv.ParseInt(p[11], 10, 64)
		if pane.Agent != "" && pane.Agent != "claude" && isShell(p[9]) && time.Now().Unix()-started > agentGrace {
			tmux("set-option", "-pu", "-t", p[10], "@roost_agent")
			tmux("set-option", "-pu", "-t", p[10], "@roost_session")
			tmux("set-option", "-pu", "-t", p[10], "@roost_started")
			pane.Agent, pane.Session = "", ""
		}
		w.Panes = append(w.Panes, pane)
	}
	sort.Ints(order)
	for _, i := range order {
		st.Windows = append(st.Windows, *byIdx[i])
	}
	return st, nil
}

func loadState(path string) (*State, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var st State
	if err := json.Unmarshal(b, &st); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	for wi := range st.Windows {
		for pi := range st.Windows[wi].Panes {
			p := &st.Windows[wi].Panes[pi]
			if p.Claude != "" && p.Session == "" {
				p.Agent, p.Session = "claude", p.Claude
			}
			p.Claude = ""
		}
	}
	return &st, nil
}

// save writes the snapshot if something changed, and keeps a history copy.
func save() (bool, error) {
	if !serverRunning() {
		return false, nil
	}
	if v, _ := tmux("show-options", "-gqv", "@roost_restoring"); v == "1" {
		return false, nil
	}
	st, err := snapshot()
	if err != nil || len(st.Windows) == 0 {
		return false, err
	}
	if old, err := loadState(statePath()); err == nil && reflect.DeepEqual(old.Windows, st.Windows) {
		return false, nil
	}
	b, _ := json.MarshalIndent(st, "", "  ")
	if err := os.MkdirAll(historyDir(), 0o700); err != nil {
		return false, err
	}
	tmp := statePath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return false, err
	}
	if err := os.Rename(tmp, statePath()); err != nil {
		return false, err
	}
	hist := filepath.Join(historyDir(), st.SavedAt.Format("20060102-150405")+".json")
	_ = os.WriteFile(hist, b, 0o600)
	pruneHistory(50)
	return true, nil
}

func historyFiles() []string {
	files, _ := filepath.Glob(filepath.Join(historyDir(), "*.json"))
	sort.Sort(sort.Reverse(sort.StringSlice(files))) // newest first
	return files
}

func pruneHistory(keep int) {
	files := historyFiles()
	for i := keep; i < len(files); i++ {
		os.Remove(files[i])
	}
}

// claudeSessionExists checks that a transcript for the session id is on disk,
// so we never try to resume something Claude can't find.
func claudeSessionExists(id string) bool {
	if !validSessionID(id) {
		return false
	}
	m, _ := filepath.Glob(filepath.Join(claudeDir(), "projects", "*", id+".jsonl"))
	return len(m) > 0
}

// Session ids get typed into a shell on restore, so only accept real UUIDs.
var sessionIDRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func validSessionID(id string) bool { return sessionIDRe.MatchString(id) }

func claudeDir() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".claude")
}

func existingDir(d string) string {
	if fi, err := os.Stat(d); err == nil && fi.IsDir() {
		return d
	}
	h, _ := os.UserHomeDir()
	return h
}

// restore rebuilds every tab from a saved state and resumes Claude sessions.
func restore(st *State) (int, int, error) {
	if len(st.Windows) == 0 {
		return 0, 0, fmt.Errorf("saved state has no tabs")
	}
	if err := writeConf(); err != nil {
		return 0, 0, err
	}
	resumed := 0
	activeWin := ""
	for wi, w := range st.Windows {
		dir0 := existingDir(w.Panes[0].Dir)
		var name []string
		if w.Custom {
			name = []string{"-n", w.Name}
		}
		var paneID string
		var err error
		if wi == 0 {
			paneID, err = tmux(append(append([]string{"new-session", "-d", "-s", sessionName}, name...), "-c", dir0, "-x", "200", "-y", "50", "-P", "-F", "#{pane_id}")...)
			if err == nil {
				tmux("set-option", "-g", "@roost_restoring", "1")
			}
		} else {
			paneID, err = tmux(append(append([]string{"new-window", "-d", "-t", sessionName + ":"}, name...), "-c", dir0, "-P", "-F", "#{pane_id}")...)
		}
		if err != nil {
			return 0, 0, err
		}
		winID, _ := tmux("display-message", "-p", "-t", paneID, "#{window_id}")
		if w.Active {
			activeWin = winID
		}
		ids := []string{paneID}
		for _, p := range w.Panes[1:] {
			id, err := tmux("split-window", "-d", "-t", winID, "-c", existingDir(p.Dir), "-P", "-F", "#{pane_id}")
			if err != nil {
				break
			}
			ids = append(ids, id)
		}
		if len(ids) > 1 && w.Layout != "" {
			tmux("select-layout", "-t", winID, w.Layout)
		}
		for i, p := range w.Panes {
			if i >= len(ids) {
				break
			}
			if p.Active {
				tmux("select-pane", "-t", ids[i])
			}
			if p.Agent == "" {
				continue
			}
			if resumeAgent(ids[i], p) {
				resumed++
			}
		}
	}
	if activeWin != "" {
		tmux("select-window", "-t", activeWin)
	}
	tmux("set-option", "-gu", "@roost_restoring")
	return len(st.Windows), resumed, nil
}

// resumeAgent restarts the pane's CLI after a restart. It returns true when it
// picked up a previous conversation (rather than starting fresh).
func resumeAgent(pane string, p Pane) bool {
	a, err := lookupAgent(p.Agent)
	if err != nil {
		tmux("send-keys", "-t", pane, "# roost: "+err.Error(), "Enter")
		return false
	}
	id := p.Session
	resumable := strings.TrimSpace(a.Resume) != ""
	if strings.Contains(a.Resume, "{id}") {
		resumable = validSessionID(id)
		// Claude only writes a transcript after the first message; if there's
		// none, resuming would fail, so start that tab fresh instead.
		if a.Name == "claude" && !claudeSessionExists(id) {
			resumable = false
		}
	}
	cmd := ""
	if resumable {
		cmd = a.ResumeCmd(id)
	} else {
		if a.UsesID() {
			id = uuid()
		} else {
			id = ""
		}
		cmd = a.StartCmd(id)
	}
	markAgent(pane, a.Name)
	if id != "" {
		tmux("set-option", "-p", "-t", pane, "@roost_session", id)
	}
	tmux("send-keys", "-t", pane, cmd, "Enter")
	return resumable
}

// agentGrace is how long after launching a CLI roost waits before treating a
// shell prompt in that pane as "the CLI has exited".
const agentGrace = 15

func markAgent(pane, name string) {
	tmux("set-option", "-p", "-t", pane, "@roost_agent", name)
	tmux("set-option", "-p", "-t", pane, "@roost_started", strconv.FormatInt(time.Now().Unix(), 10))
}
