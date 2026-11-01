package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var hookEvents = []string{
	"SessionStart", "SessionEnd", "UserPromptSubmit", "PostToolUse",
	"PermissionRequest", "Notification", "Stop",
}

var hookRe = regexp.MustCompile(`roost'?\s+hook"`)

func settingsPath() string { return filepath.Join(claudeDir(), "settings.json") }

func hooksInstalled() bool {
	b, err := os.ReadFile(settingsPath())
	return err == nil && hookRe.Match(b)
}

// setup adds (or removes) roost's hook to ~/.claude/settings.json. It uses jq
// so your existing settings keep their order and formatting.
func setup(remove bool) error {
	if _, err := exec.LookPath("jq"); err != nil {
		return fmt.Errorf("jq is needed to edit %s safely (brew install jq)", settingsPath())
	}
	path := settingsPath()
	orig, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		orig = []byte("{}")
	} else if err != nil {
		return err
	}
	events, _ := json.Marshal(hookEvents)
	cmd := shellQuote(selfPath()) + " hook"

	// Always strip old roost entries first so re-running setup (e.g. after
	// moving the binary) is idempotent.
	filter := `
	  def strip: map(.hooks |= map(select((.command // "") | test("roost'?\\s+hook$") | not)))
	             | map(select((.hooks | length) > 0));
	  if .hooks then .hooks |= with_entries(.value |= strip) | .hooks |= with_entries(select((.value | length) > 0)) else . end`
	if !remove {
		filter += ` | .hooks //= {} | reduce $events[] as $e (.; .hooks[$e] = ((.hooks[$e] // []) + [{"hooks":[{"type":"command","command":$cmd}]}]))`
	}
	j := exec.Command("jq", "--indent", "2", "--arg", "cmd", cmd, "--argjson", "events", string(events), filter)
	j.Stdin = bytes.NewReader(orig)
	var out, errb bytes.Buffer
	j.Stdout, j.Stderr = &out, &errb
	if err := j.Run(); err != nil {
		return fmt.Errorf("could not update %s: %s", path, strings.TrimSpace(errb.String()))
	}
	if bytes.Equal(bytes.TrimSpace(out.Bytes()), bytes.TrimSpace(orig)) {
		fmt.Println("Nothing to change in", path)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if len(orig) > 2 {
		backup := path + ".roost-backup-" + time.Now().Format("20060102-150405")
		if err := os.WriteFile(backup, orig, 0o600); err != nil {
			return err
		}
		fmt.Println("Backed up your settings to", backup)
	}
	mode := os.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	if err := os.WriteFile(path, out.Bytes(), mode); err != nil {
		return err
	}
	if remove {
		fmt.Println("Removed roost hooks from", path)
	} else {
		fmt.Println("Installed roost hooks in", path)
		fmt.Println("Claude sessions started from now on will be remembered across restarts.")
	}
	return nil
}
