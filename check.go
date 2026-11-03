package main

// Checks ("sensors"): after a Claude tab finishes a turn, run the project's
// test/lint command in the background and show ✓ / ✗ on the tab.
//
// A command that runs on its own is only ever run after you've approved it
// for that project (like direnv): a cloned repo can't make roost execute
// anything you haven't seen.

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"
)

const checkFile = ".roost-check"

// Check marker states (window option @roost_check).
const (
	checkRunning   = "running"
	checkPass      = "pass"
	checkFail      = "fail"
	checkUntrusted = "untrusted"
)

func checksDir() string       { return filepath.Join(homeDir(), "checks") }
func checksAllowPath() string { return filepath.Join(homeDir(), "checks-allowed.json") }

var paneIDRe = regexp.MustCompile(`^%[0-9]+$`)

func checkLogPath(pane string) string {
	return filepath.Join(checksDir(), strings.TrimPrefix(pane, "%")+".log")
}
func checkPidPath(pane string) string {
	return filepath.Join(checksDir(), strings.TrimPrefix(pane, "%")+".pid")
}

// projectRoot is the git top level of dir, or dir itself.
func projectRoot(dir string) string {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err == nil {
		if root := strings.TrimSpace(string(out)); root != "" {
			return root
		}
	}
	return dir
}

// checkCommand finds the project's check: the first command line of
// .roost-check, else a test command guessed from the project's files.
// It returns "" when there's nothing to run.
func checkCommand(root string) (cmd, source string) {
	if b, err := os.ReadFile(filepath.Join(root, checkFile)); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
				return line, checkFile
			}
		}
		return "", checkFile
	}
	exists := func(name string) bool { _, err := os.Stat(filepath.Join(root, name)); return err == nil }
	if b, err := os.ReadFile(filepath.Join(root, "Makefile")); err == nil &&
		regexp.MustCompile(`(?m)^test\s*:`).Match(b) {
		return "make test", "Makefile"
	}
	if exists("go.mod") {
		return "go test ./...", "go.mod"
	}
	if exists("Cargo.toml") {
		return "cargo test", "Cargo.toml"
	}
	if b, err := os.ReadFile(filepath.Join(root, "package.json")); err == nil {
		var pkg struct {
			Scripts map[string]string `json:"scripts"`
		}
		if json.Unmarshal(b, &pkg) == nil && pkg.Scripts["test"] != "" &&
			!strings.Contains(pkg.Scripts["test"], "no test specified") {
			return "npm test --silent", "package.json"
		}
	}
	if exists("pyproject.toml") || exists("pytest.ini") || exists("setup.cfg") {
		if _, err := exec.LookPath("pytest"); err == nil {
			return "pytest -q", "pytest"
		}
	}
	return "", ""
}

// trustKey ties an approval to this exact folder and command.
func trustKey(root, cmd string) string {
	h := sha256.Sum256([]byte(root + "\x00" + cmd))
	return hex.EncodeToString(h[:])
}

func loadTrust() map[string]string {
	m := map[string]string{}
	if b, err := os.ReadFile(checksAllowPath()); err == nil {
		json.Unmarshal(b, &m)
	}
	return m
}

func saveTrust(m map[string]string) error {
	if err := os.MkdirAll(homeDir(), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	return os.WriteFile(checksAllowPath(), append(b, '\n'), 0o600)
}

func isTrusted(root, cmd string) bool {
	_, ok := loadTrust()[trustKey(root, cmd)]
	return ok
}

func setTrust(root, cmd string, allow bool) error {
	m := loadTrust()
	// Drop any older approval for this folder (its command changed).
	for k, v := range m {
		if strings.HasPrefix(v, root+"\x00") {
			delete(m, k)
		}
	}
	if allow {
		m[trustKey(root, cmd)] = root + "\x00" + cmd
	}
	return saveTrust(m)
}

func checksEnabled() bool {
	v, _ := tmuxOption("@roost_checks")
	return v == "on"
}

func checkTimeout() time.Duration {
	if v, ok := tmuxOption("@roost_check_timeout"); ok {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return time.Duration(n) * time.Second
		}
	}
	return 10 * time.Minute
}

func setCheckState(pane, state string) {
	if state == "" {
		tmux("set-option", "-wu", "-t", pane, "@roost_check")
		return
	}
	tmux("set-option", "-w", "-t", pane, "@roost_check", state)
}

// stopCheck ends a background check still running for this pane. It only
// signals a process that is actually a roost check (pids get reused).
func stopCheck(pane string) {
	b, err := os.ReadFile(checkPidPath(pane))
	if err != nil {
		return
	}
	os.Remove(checkPidPath(pane))
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 1 {
		return
	}
	out, _ := exec.Command("ps", "-o", "args=", "-p", strconv.Itoa(pid)).Output()
	if !strings.Contains(string(out), "check --run") {
		return
	}
	syscall.Kill(-pid, syscall.SIGTERM) // its process group: the check and its children
}

// startBackgroundCheck is called by the Claude hook when a turn ends.
func startBackgroundCheck(pane, dir string) {
	if !paneIDRe.MatchString(pane) || dir == "" || !checksEnabled() {
		return
	}
	stopCheck(pane)
	root := projectRoot(dir)
	cmd, _ := checkCommand(root)
	if cmd == "" {
		setCheckState(pane, "")
		return
	}
	if !isTrusted(root, cmd) {
		setCheckState(pane, checkUntrusted)
		return
	}
	if err := os.MkdirAll(checksDir(), 0o700); err != nil {
		return
	}
	c := exec.Command(selfPath(), "check", "--run", "--pane", pane, "--dir", root)
	c.Env = os.Environ()
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // own process group, outlives the hook
	if c.Start() == nil {
		os.WriteFile(checkPidPath(pane), []byte(strconv.Itoa(c.Process.Pid)), 0o600)
		c.Process.Release()
	}
}

// runCheck runs the approved command for root, writing output to log, and
// returns whether it passed.
func runCheck(root, cmd string, out io.Writer, timeout time.Duration) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	c := exec.CommandContext(ctx, "/bin/sh", "-c", cmd)
	c.Dir = root
	c.Stdout, c.Stderr = out, out
	c.Env = append(os.Environ(), "ROOST_CHECK=1")
	err := c.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return false, fmt.Errorf("timed out after %s", timeout)
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return false, nil
	}
	return err == nil, err
}

// cmdCheck implements `roost check`:
//
//	roost check            run this project's check now (asks to approve it first)
//	roost check --allow    approve this project's check without running it
//	roost check --deny     revoke the approval
//	roost check --show     show the last check output for this tab
func cmdCheck(args []string) error {
	pane := flagValue(args, "--pane")
	if pane == "" {
		pane = os.Getenv("TMUX_PANE")
	}
	if pane != "" && !paneIDRe.MatchString(pane) {
		return fmt.Errorf("invalid pane %q", pane)
	}

	if hasFlag(args, "--run") { // background worker started by the hook
		return backgroundRun(pane, flagValue(args, "--dir"))
	}
	if hasFlag(args, "--popup") { // Ctrl-Space c / right-click menu
		return checkPopup(pane, flagValue(args, "--client"))
	}
	if hasFlag(args, "--show") {
		return showCheck(pane, hasFlag(args, "--wait"))
	}

	dir := flagValue(args, "--dir")
	if dir == "" {
		dir, _ = os.Getwd()
	}
	root := projectRoot(dir)
	cmd, source := checkCommand(root)
	if cmd == "" {
		if source == checkFile {
			return fmt.Errorf("%s has no command in it", filepath.Join(root, checkFile))
		}
		return fmt.Errorf("no check found for %s: add a %s file with the command to run (e.g. `make lint test`)", root, checkFile)
	}
	switch {
	case hasFlag(args, "--deny"):
		if err := setTrust(root, cmd, false); err != nil {
			return err
		}
		fmt.Printf("roost won't run checks in %s.\n", root)
		return nil
	case hasFlag(args, "--allow"):
		if err := setTrust(root, cmd, true); err != nil {
			return err
		}
		fmt.Printf("Approved: roost may run `%s` in %s.\n", cmd, root)
		return nil
	}

	if !isTrusted(root, cmd) {
		fmt.Printf("Check for %s (from %s):\n\n    %s\n\n", root, source, cmd)
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			return errors.New("not approved yet: run `roost check --allow` here to approve it")
		}
		fmt.Print("Allow roost to run this here, now and automatically after Claude finishes? [y/N] ")
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if a := strings.ToLower(strings.TrimSpace(line)); a != "y" && a != "yes" {
			return errors.New("not approved")
		}
		if err := setTrust(root, cmd, true); err != nil {
			return err
		}
	}

	// Run in the foreground, showing output, and record it for this tab too.
	var log io.Writer = os.Stdout
	var logf *os.File
	if pane != "" && os.MkdirAll(checksDir(), 0o700) == nil {
		stopCheck(pane)
		if f, err := os.OpenFile(checkLogPath(pane), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600); err == nil {
			logf = f
			defer f.Close()
			fmt.Fprintf(f, "$ %s\n", cmd)
			log = io.MultiWriter(os.Stdout, f)
		}
		setCheckState(pane, checkRunning)
	}
	fmt.Printf("$ %s\n", cmd)
	ok, err := runCheck(root, cmd, log, checkTimeout())
	if pane != "" {
		setCheckState(pane, map[bool]string{true: checkPass, false: checkFail}[ok])
	}
	if logf != nil {
		fmt.Fprintf(logf, "\n%s\n", checkVerdict(ok, err))
	}
	fmt.Println(checkVerdict(ok, err))
	if !ok {
		os.Exit(1)
	}
	return nil
}

func checkVerdict(ok bool, err error) string {
	switch {
	case ok:
		return "✓ check passed"
	case err != nil:
		return "✗ check failed: " + err.Error()
	default:
		return "✗ check failed"
	}
}

func backgroundRun(pane, root string) error {
	defer os.Remove(checkPidPath(pane))
	if pane == "" || root == "" {
		return errors.New("--run needs --pane and --dir")
	}
	cmd, _ := checkCommand(root)
	if cmd == "" || !isTrusted(root, cmd) { // re-check: the file may have changed
		setCheckState(pane, checkUntrusted)
		return nil
	}
	f, err := os.OpenFile(checkLogPath(pane), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	fmt.Fprintf(f, "$ %s    (in %s, %s)\n", cmd, root, time.Now().Format("15:04:05"))
	setCheckState(pane, checkRunning)
	ok, runErr := runCheck(root, cmd, f, checkTimeout())
	fmt.Fprintf(f, "\n%s\n", checkVerdict(ok, runErr))
	if ok {
		setCheckState(pane, checkPass)
		return nil
	}
	setCheckState(pane, checkFail)
	tab, _ := tmux("display-message", "-p", "-t", pane, "#W")
	tmux("display-message", "-d", "5000", fmt.Sprintf("roost: checks failed in '%s' — Ctrl-Space c to see why", strings.TrimSpace(tab)))
	return nil
}

var clientNameRe = regexp.MustCompile(`^[A-Za-z0-9/_.-]+$`)

// checkPopup opens the last check output for pane in a scrollable popup.
func checkPopup(pane, client string) error {
	if pane == "" {
		return errors.New("--popup needs --pane")
	}
	args := []string{"display-popup"}
	if clientNameRe.MatchString(client) {
		args = append(args, "-c", client)
	}
	show := shellQuote(selfPath()) + " check --show --pane " + pane
	args = append(args, "-E", "-w", "90%", "-h", "80%", "-T", " Check ", show+" | less -R +G")
	_, err := tmux(args...)
	return err
}

func showCheck(pane string, wait bool) error {
	if pane == "" {
		return errors.New("not in a roost tab")
	}
	b, err := os.ReadFile(checkLogPath(pane))
	if err != nil {
		fmt.Println("No check has run in this tab yet.")
		fmt.Println("Turn checks on with `set -g @roost_checks on` in custom.conf, or run `roost check`.")
	} else {
		os.Stdout.Write(b)
	}
	if wait {
		fmt.Print("\n  press any key to close")
		fd := int(os.Stdin.Fd())
		if old, err := term.MakeRaw(fd); err == nil {
			os.Stdin.Read(make([]byte, 8))
			term.Restore(fd, old)
		}
	}
	return nil
}
