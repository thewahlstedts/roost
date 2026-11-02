package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/term"
)

// settings.conf holds what `roost setup` asked you (today: the default CLI).
// It's loaded before custom.conf, so anything you set there still wins.
func settingsConfPath() string { return filepath.Join(homeDir(), "settings.conf") }

var agentLabels = map[string]string{
	"claude": "Claude Code",
	"pi":     "pi",
	"codex":  "OpenAI Codex",
	"devin":  "Devin",
	"grok":   "Grok",
}

var defaultAgentLineRe = regexp.MustCompile(`(?m)^\s*set(-option)?\s+-g\s+@roost_default_agent\s+['"]?([a-z0-9_-]+)`)

// configuredAgent returns the default CLI set in a conf file, if any.
func configuredAgent(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	if m := defaultAgentLineRe.FindAllSubmatch(b, -1); len(m) > 0 {
		return string(m[len(m)-1][2])
	}
	return ""
}

func installed(cmd string) bool {
	_, err := exec.LookPath(cmd)
	return err == nil
}

// suggestedAgent is the first installed preset (claude first), else claude.
func suggestedAgent() string {
	for _, a := range builtinAgents {
		if installed(a.Cmd) {
			return a.Name
		}
	}
	return "claude"
}

// setupAgent decides the default CLI during `roost setup`:
// --agent NAME sets it; otherwise ask once (interactive) or pick the first
// installed one (non-interactive). Returns the chosen name.
func setupAgent(flag string) (string, error) {
	custom := configuredAgent(customConfPath())
	if flag != "" {
		if !validAgentName(flag) {
			return "", fmt.Errorf("invalid CLI name %q", flag)
		}
		if err := writeSettingsAgent(flag); err != nil {
			return "", err
		}
		fmt.Printf("Default CLI for new tabs: %s\n", flag)
		if !isBuiltinAgent(flag) && configuredAgentCmd(flag) == "" {
			fmt.Printf("  %s isn't a built-in preset: define it in %s with\n    set -g @roost_agent_%s_cmd \"…\"\n", flag, customConfPath(), flag)
		}
		warnCustomOverride(custom, flag)
		return flag, nil
	}
	if cur := configuredAgent(settingsConfPath()); cur != "" {
		eff := cur
		if custom != "" {
			eff = custom
		}
		fmt.Printf("Default CLI for new tabs: %s (change with: roost setup --agent NAME)\n", eff)
		return eff, nil
	}
	if custom != "" { // set by hand before setup ever asked
		fmt.Printf("Default CLI for new tabs: %s (from %s)\n", custom, customConfPath())
		return custom, writeSettingsAgent(custom)
	}

	choice := suggestedAgent()
	if term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd())) {
		choice = askAgent(choice)
	}
	if err := writeSettingsAgent(choice); err != nil {
		return "", err
	}
	fmt.Printf("Default CLI for new tabs: %s (change with: roost setup --agent NAME)\n", choice)
	return choice, nil
}

func askAgent(suggest string) string {
	fmt.Println("Which AI coding CLI do you use? New tabs will run it.")
	fmt.Println("(You can still pick another per tab: press Tab in the '+ new' folder picker.)")
	def := 1
	for i, a := range builtinAgents {
		mark := ""
		if installed(a.Cmd) {
			mark = "  ✓ installed"
		}
		if a.Name == suggest {
			def = i + 1
		}
		fmt.Printf("  %d) %-7s %-13s%s\n", i+1, a.Name, agentLabels[a.Name], mark)
	}
	fmt.Println("  or type the name of another CLI you've defined in custom.conf")
	in := bufio.NewReader(os.Stdin)
	for {
		fmt.Printf("Choose [%d]: ", def)
		line, err := in.ReadString('\n')
		line = strings.TrimSpace(strings.ToLower(line))
		if line == "" {
			return builtinAgents[def-1].Name
		}
		if n, err := strconv.Atoi(line); err == nil && n >= 1 && n <= len(builtinAgents) {
			return builtinAgents[n-1].Name
		}
		if validAgentName(line) {
			return line
		}
		if err != nil { // input closed mid-answer: don't loop forever
			return builtinAgents[def-1].Name
		}
		fmt.Println("  Enter a number from the list or a CLI name.")
	}
}

func isBuiltinAgent(name string) bool {
	for _, a := range builtinAgents {
		if a.Name == name {
			return true
		}
	}
	return false
}

// configuredAgentCmd finds `set -g @roost_agent_<name>_cmd` in custom.conf
// (setup may run while roost's tmux server isn't up to ask).
func configuredAgentCmd(name string) string {
	b, err := os.ReadFile(customConfPath())
	if err != nil {
		return ""
	}
	re := regexp.MustCompile(`(?m)^\s*set(-option)?\s+-g\s+@roost_agent_` + regexp.QuoteMeta(name) + `_cmd\s+(.+)$`)
	if m := re.FindSubmatch(b); m != nil {
		return strings.TrimSpace(string(m[2]))
	}
	return ""
}

func warnCustomOverride(custom, chosen string) {
	if custom != "" && custom != chosen {
		fmt.Printf("  Note: %s sets @roost_default_agent %s, which wins. Remove that line to use %s.\n", customConfPath(), custom, chosen)
	}
}

func writeSettingsAgent(name string) error {
	if err := os.MkdirAll(homeDir(), 0o700); err != nil {
		return err
	}
	body := "# Written by `roost setup`. Change with: roost setup --agent NAME\n" +
		"# (custom.conf is loaded after this file, so settings there win.)\n" +
		"set -g @roost_default_agent " + name + "\n"
	return os.WriteFile(settingsConfPath(), []byte(body), 0o600)
}

// wantsClaudeHook: install the Claude hook when Claude Code is around, even
// if it isn't the default (you can still open Claude tabs).
func wantsClaudeHook(agent string) bool {
	if agent == "claude" || installed("claude") {
		return true
	}
	_, err := os.Stat(claudeDir())
	return err == nil
}
