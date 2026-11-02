package main

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

// An Agent is a coding CLI roost can run in a tab (Claude Code, pi, Codex…).
//
// The command that gets typed is Cmd + " " + Start (new tab) or
// Cmd + " " + Resume (after a restart), with {id} replaced by the tab's
// session id. If Start contains {id}, roost generates a UUID for each new tab
// so the exact conversation can be resumed later.
type Agent struct {
	Name   string
	Cmd    string
	Start  string
	Resume string
}

// Built-in presets. Everything can be overridden in ~/.roost/custom.conf:
//
//	set -g @roost_agent_<name>_cmd    "…"   # the program (and any flags you always want)
//	set -g @roost_agent_<name>_start  "…"   # extra args for a new tab
//	set -g @roost_agent_<name>_resume "…"   # args to resume after a restart ("" = start fresh)
var builtinAgents = []Agent{
	{Name: "claude", Cmd: "claude", Start: "--session-id {id}", Resume: "--resume {id}"},
	{Name: "pi", Cmd: "pi", Start: "--session-id {id}", Resume: "--session-id {id}"},
	{Name: "codex", Cmd: "codex", Start: "", Resume: "resume --last"},
	// Devin can't be given a session id up front; -c resumes the most recent
	// session in the current folder, which is the tab's folder on restore.
	{Name: "devin", Cmd: "devin", Start: "", Resume: "-c"},
	{Name: "grok", Cmd: "grok", Start: "", Resume: ""},
}

var agentNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

func validAgentName(n string) bool { return agentNameRe.MatchString(n) }

func tmuxOption(name string) (string, bool) {
	v, err := tmux("show-options", "-gqv", name)
	if err != nil {
		return "", false
	}
	// show-options -q prints nothing for unset options; distinguish "set to
	// empty" by asking tmux whether it exists.
	if v == "" {
		if out, _ := tmux("show-options", "-gq", name); strings.TrimSpace(out) == "" {
			return "", false
		}
	}
	return strings.TrimSpace(v), true
}

// lookupAgent returns the agent definition (preset + custom.conf overrides).
func lookupAgent(name string) (Agent, error) {
	if !validAgentName(name) {
		return Agent{}, fmt.Errorf("invalid agent name %q", name)
	}
	a := Agent{Name: name}
	known := false
	for _, b := range builtinAgents {
		if b.Name == name {
			a, known = b, true
		}
	}
	pre := "@roost_agent_" + name + "_"
	if v, ok := tmuxOption(pre + "cmd"); ok && v != "" {
		a.Cmd, known = v, true
	}
	if v, ok := tmuxOption(pre + "start"); ok {
		a.Start = v
	}
	if v, ok := tmuxOption(pre + "resume"); ok {
		a.Resume = v
	}
	if name == "claude" {
		if c := os.Getenv("ROOST_CLAUDE_CMD"); c != "" { // testing / one-off override
			a.Cmd = c
		}
	}
	if !known {
		return Agent{}, fmt.Errorf("unknown agent %q: add `set -g @roost_agent_%s_cmd \"…\"` to %s", name, name, customConfPath())
	}
	return a, nil
}

func customConfPath() string { return homeDir() + "/custom.conf" }

// UsesID reports whether this agent can be pinned to an exact session.
func (a Agent) UsesID() bool { return strings.Contains(a.Start, "{id}") }

func (a Agent) build(args, id string) string {
	cmd := a.Cmd
	if args = strings.TrimSpace(strings.ReplaceAll(args, "{id}", id)); args != "" {
		cmd += " " + args
	}
	return cmd
}

// StartCmd is what to type in a new tab. id must be "" or a roost-generated
// UUID (callers pass uuid()).
func (a Agent) StartCmd(id string) string { return a.build(a.Start, id) }

// ResumeCmd is what to type after a restart. It returns "" when this agent
// can't resume (no Resume args) or the template needs an id and id isn't a
// valid UUID: ids get typed into a shell, so a bad one is never used.
func (a Agent) ResumeCmd(id string) string {
	if strings.TrimSpace(a.Resume) == "" || (strings.Contains(a.Resume, "{id}") && !validSessionID(id)) {
		return ""
	}
	return a.build(a.Resume, id)
}

// defaultAgent is the CLI new tabs use: @roost_default_agent, else claude.
func defaultAgent() string {
	if v, ok := tmuxOption("@roost_default_agent"); ok && validAgentName(v) {
		return v
	}
	return "claude"
}

// pickerAgents lists the CLIs Tab cycles through in the folder picker:
// @roost_agents if set, otherwise the default plus any installed presets.
func pickerAgents() []string {
	if v, ok := tmuxOption("@roost_agents"); ok && v != "" {
		var out []string
		for _, n := range strings.Fields(v) {
			if validAgentName(n) {
				out = append(out, n)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	def := defaultAgent()
	out := []string{def}
	for _, b := range builtinAgents {
		if b.Name == def {
			continue
		}
		if _, err := exec.LookPath(b.Cmd); err == nil {
			out = append(out, b.Name)
		}
	}
	return out
}

var shells = map[string]bool{"zsh": true, "bash": true, "fish": true, "sh": true, "dash": true, "ksh": true, "tcsh": true, "csh": true, "nu": true}

// isShell reports whether a pane's foreground command is a plain shell.
func isShell(cmd string) bool { return shells[strings.TrimPrefix(cmd, "-")] }
