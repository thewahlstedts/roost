package main

import "testing"

func TestAgentCommands(t *testing.T) {
	id := "a87ff3d8-7d4f-4eb4-b1b9-e7ad7df2fad9"
	byName := map[string]Agent{}
	for _, a := range builtinAgents {
		byName[a.Name] = a
	}
	cases := []struct {
		agent, start, resume string
		usesID               bool
	}{
		{"claude", "claude --session-id " + id, "claude --resume " + id, true},
		{"pi", "pi --session-id " + id, "pi --session-id " + id, true},
		{"codex", "codex", "codex resume --last", false},
		{"devin", "devin", "devin -c", false},
		{"grok", "grok", "", false}, // no resume: roost starts it fresh
	}
	for _, c := range cases {
		a := byName[c.agent]
		sid := ""
		if a.UsesID() {
			sid = id
		}
		if a.UsesID() != c.usesID {
			t.Errorf("%s UsesID = %v", c.agent, a.UsesID())
		}
		if got := a.StartCmd(sid); got != c.start {
			t.Errorf("%s start = %q, want %q", c.agent, got, c.start)
		}
		if got := a.ResumeCmd(sid); got != c.resume {
			t.Errorf("%s resume = %q, want %q", c.agent, got, c.resume)
		}
	}
	// A resume template needing an id never types a bad one.
	for _, bad := range []string{"", "x; rm -rf ~", id + "; echo pwned"} {
		if got := byName["claude"].ResumeCmd(bad); got != "" {
			t.Errorf("ResumeCmd(%q) = %q, want refusal", bad, got)
		}
	}
	custom := Agent{Name: "mine", Cmd: "mycli --fast", Start: "", Resume: "--continue"}
	if got := custom.ResumeCmd(""); got != "mycli --fast --continue" {
		t.Errorf("custom resume = %q", got)
	}
}

func TestAgentNames(t *testing.T) {
	for _, ok := range []string{"claude", "pi", "my-cli", "x_2"} {
		if !validAgentName(ok) {
			t.Errorf("%q should be valid", ok)
		}
	}
	for _, bad := range []string{"", "Claude", "a b", "a;b", "-x", "../x"} {
		if validAgentName(bad) {
			t.Errorf("%q should be invalid", bad)
		}
	}
}
