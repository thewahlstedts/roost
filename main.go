package main

import (
	"crypto/rand"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"
)

// version is stamped by the release build (-ldflags -X main.version=…).
// Anything else is a dev build, which never auto-updates.
var version = "dev"

const usage = `roost — keep your AI coding CLI tabs alive, even across restarts.

  roost                 open roost (restores your tabs after a restart)
  roost new [dir]       new tab running your CLI (default: claude) in dir
      --agent NAME        use another CLI: pi, codex, devin, grok, factory, or your own
      --shell             plain shell tab instead
      --name NAME         fixed tab name (default: follows the folder)
  roost ls              list tabs (running, or last saved)
  roost save            save now (roost also saves automatically)
  roost restore         rebuild tabs from the last save
      --list              show older saves;  roost restore 3  uses one
  roost kill            save, then close everything
  roost setup           choose your CLI, install the Claude hook (run once)
      --agent NAME        set the default CLI without asking
      --remove            uninstall it
      --shortcut X        Ctrl+Cmd+X opens roost in iTerm (default R; 'none')
  roost keys            show keyboard shortcuts
  roost check           run this project's check (tests/lint) now
      --allow | --deny    approve or revoke it running automatically
  roost update          install the latest release (roost checks daily)
      --check             only say whether there's a newer one

  Add --native to 'roost' to use iTerm's native tabs (tmux -CC).
`

func main() {
	args := os.Args[1:]
	cmd := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
	}
	// -h/--help anywhere means "show help", never "do it": otherwise
	// `roost kill --help` would close everything and `roost --help` would
	// open roost.
	if wantsHelp(args) && cmd != "hook" {
		fmt.Print(usage)
		return
	}
	var err error
	switch cmd {
	case "", "attach", "open":
		err = cmdOpen(hasFlag(args, "--native"))
	case "new":
		err = cmdNew(args)
	case "pick":
		err = pick()
	case "save":
		var changed bool
		changed, err = save()
		if err == nil && !hasFlag(args, "--quiet") {
			if !serverRunning() {
				fmt.Println("roost isn't running; nothing to save.")
			} else if changed {
				fmt.Println("Saved to", statePath())
			} else {
				fmt.Println("Already up to date.")
			}
		}
	case "restore":
		err = cmdRestore(args)
	case "ls", "list":
		err = cmdList()
	case "kill", "stop":
		err = cmdKill()
	case "setup":
		remove := hasFlag(args, "--remove")
		agent := ""
		if !remove {
			agent, err = setupAgent(flagValue(args, "--agent"))
		}
		if err == nil && (remove || wantsClaudeHook(agent)) {
			err = setup(remove)
		} else if err == nil {
			fmt.Println("Skipping the Claude Code hook (Claude Code isn't installed).")
		}
		if err == nil {
			err = setupITerm(hasFlag(args, "--remove"), flagValue(args, "--shortcut"))
		}
		if err == nil && serverRunning() {
			// Apply the latest config (e.g. Cmd+arrow keys) to the live roost.
			if err = writeConf(); err == nil {
				_, err = tmux("source-file", confPath())
				refreshSplitLabels()
				fmt.Println("Reloaded the running roost.")
			}
		}
	case "hook":
		runHook()
	case "keys":
		fmt.Print(keysHelp)
		if hasFlag(args, "--wait") {
			err = helpMenu()
		}
	case "check":
		err = cmdCheck(args)
	case "tab-drop": // internal: drag-and-drop of a tab on the status bar
		err = cmdTabDrop(args)
	case "update":
		err = cmdUpdate(args)
	case "version", "--version":
		fmt.Println("roost", version)
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "roost: unknown command %q\n\n%s", cmd, usage)
		os.Exit(2)
	}
	if err != nil {
		if hasFlag(args, "--quiet") {
			return
		}
		fmt.Fprintln(os.Stderr, "roost:", err)
		os.Exit(1)
	}
}

func wantsHelp(args []string) bool { return hasFlag(args, "-h") || hasFlag(args, "--help") }

func hasFlag(args []string, f string) bool {
	for _, a := range args {
		if a == f {
			return true
		}
	}
	return false
}

func flagValue(args []string, f string) string {
	for i, a := range args {
		if a == f && i+1 < len(args) {
			return args[i+1]
		} else if strings.HasPrefix(a, f+"=") {
			return a[len(f)+1:]
		}
	}
	return ""
}

func cmdOpen(native bool) error {
	if insideRoost() {
		fmt.Println("You're already inside roost. (Ctrl-Space ? for keys)")
		return nil
	}
	if err := writeConf(); err != nil {
		return err
	}
	var notice string
	if serverRunning() {
		tmux("source-file", confPath())
		refreshSplitLabels()
	} else if st, err := loadState(statePath()); err == nil && len(st.Windows) > 0 {
		tabs, resumed, err := restore(st)
		if err != nil {
			return err
		}
		notice = fmt.Sprintf("roost: restored %d tab(s), resumed %d Claude session(s)", tabs, resumed)
	} else {
		cwd, _ := os.Getwd()
		if _, err := newTab(cwd, "", ""); err != nil {
			return err
		}
		notice = "roost: welcome! Click '+ new' or press Ctrl-Space n for a new tab"
	}
	if _, err := os.Stat(settingsConfPath()); err != nil {
		notice = "roost: run 'roost setup' once to choose your CLI and set up restore"
	} else if defaultAgent() == "claude" && !hooksInstalled() {
		notice = "roost: run 'roost setup' so Claude sessions can be resumed after a restart"
	}
	if notice != "" {
		tmux("run-shell", "-b", "sleep 0.8; tmux -L "+shellQuote(socketName)+" display-message -d 6000 "+shellQuote(notice))
	}
	maybeAutoUpdate()
	return attach(native)
}

func attach(native bool) error {
	bin, err := exec.LookPath("tmux")
	if err != nil {
		return err
	}
	argv := []string{"tmux", "-L", socketName, "-f", confPath()}
	if native {
		argv = append(argv, "-CC")
	}
	argv = append(argv, "attach-session", "-t", sessionName)
	return syscall.Exec(bin, argv, envWithoutTMUX())
}

func cmdNew(args []string) error {
	dir := ""
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--name" || a == "--dir" || a == "--agent" {
			if a == "--dir" && i+1 < len(args) {
				dir = args[i+1]
			}
			i++
			continue
		}
		if !strings.HasPrefix(a, "-") && dir == "" {
			dir = a
		}
	}
	if dir == "" {
		dir, _ = os.Getwd()
	}
	dir, _ = filepath.Abs(expandTilde(dir))
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return fmt.Errorf("not a folder: %s", dir)
	}
	started := !serverRunning()
	agent := ""
	if !hasFlag(args, "--shell") {
		agent = flagValue(args, "--agent")
		if agent == "" {
			agent = useDefaultAgent
		}
	}
	if _, err := newTab(dir, flagValue(args, "--name"), agent); err != nil {
		return err
	}
	if !insideRoost() {
		if started {
			return attach(false)
		}
		fmt.Println("Opened a new tab in roost. Run 'roost' to see it.")
	}
	return nil
}

// newTab opens a tab (starting roost if needed) and, unless shell is set,
// launches Claude with a known session id so it can be resumed later.
// useDefaultAgent asks newTab to use the configured default CLI.
const useDefaultAgent = "\x00default"

// newTab opens a tab (starting roost if needed) and runs agent in it ("" for
// a plain shell). The agent is looked up after the tab exists, so roost's
// config (custom.conf) is loaded and custom CLIs resolve.
func newTab(dir, name, agent string) (string, error) {
	var nameArg []string
	if name != "" {
		nameArg = []string{"-n", name} // pinned; otherwise it follows the folder
	}
	var pane string
	var err error
	if serverRunning() {
		pane, err = tmux(append(append([]string{"new-window", "-t", sessionName + ":"}, nameArg...), "-c", dir, "-P", "-F", "#{pane_id}")...)
	} else {
		if err := writeConf(); err != nil {
			return "", err
		}
		pane, err = tmux(append(append([]string{"new-session", "-d", "-s", sessionName}, nameArg...), "-c", dir, "-x", "200", "-y", "50", "-P", "-F", "#{pane_id}")...)
	}
	if err != nil {
		return "", err
	}
	if agent == useDefaultAgent {
		agent = defaultAgent()
	}
	if agent != "" {
		ag, err := lookupAgent(agent)
		if err != nil {
			tmux("send-keys", "-t", pane, "# roost: "+err.Error(), "Enter")
			return pane, err
		}
		id := ""
		if ag.UsesID() {
			id = uuid()
			tmux("set-option", "-p", "-t", pane, "@roost_session", id)
		}
		markAgent(pane, ag.Name)
		tmux("send-keys", "-t", pane, ag.StartCmd(id), "Enter")
	}
	save()
	return pane, nil
}

func uuid() string {
	b := make([]byte, 16)
	rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

func cmdRestore(args []string) error {
	files := historyFiles()
	if hasFlag(args, "--list") {
		if len(files) == 0 {
			fmt.Println("No saves yet.")
		}
		for i, f := range files {
			st, err := loadState(f)
			if err != nil {
				continue
			}
			var names []string
			for _, w := range st.Windows {
				names = append(names, w.Name)
			}
			fmt.Printf("%3d  %s  %s\n", i, st.SavedAt.Format("Jan 2 15:04:05"), strings.Join(names, ", "))
		}
		return nil
	}
	if serverRunning() {
		return fmt.Errorf("roost is already running. Run 'roost kill' first, then 'roost restore'")
	}
	path := statePath()
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		n, err := strconv.Atoi(args[0])
		if err != nil || n < 0 || n >= len(files) {
			return fmt.Errorf("no save #%s (see 'roost restore --list')", args[0])
		}
		path = files[n]
	}
	st, err := loadState(path)
	if err != nil {
		return fmt.Errorf("nothing to restore: %v", err)
	}
	tabs, resumed, err := restore(st)
	if err != nil {
		return err
	}
	fmt.Printf("Restored %d tab(s), resumed %d Claude session(s). Run 'roost' to open.\n", tabs, resumed)
	return nil
}

func cmdList() error {
	var st *State
	var err error
	label := "running"
	if serverRunning() {
		st, err = snapshot()
	} else {
		st, err = loadState(statePath())
		label = "saved"
	}
	if err != nil {
		fmt.Println("No tabs yet. Run 'roost' to start.")
		return nil
	}
	if label == "saved" {
		label += " " + st.SavedAt.Format("Jan 2 15:04") + " (not running — 'roost' brings these back)"
	}
	fmt.Printf("roost tabs (%s):\n", label)
	for i, w := range st.Windows {
		mark := " "
		if w.Active {
			mark = "*"
		}
		for j, p := range w.Panes {
			kind := "shell"
			if p.Agent != "" {
				kind = p.Agent
			}
			head := fmt.Sprintf("%s %d %-18s", mark, i+1, w.Name)
			if j > 0 {
				head = strings.Repeat(" ", len(head))
			}
			fmt.Printf("%s  %-6s  %s\n", head, kind, tilde(p.Dir))
		}
	}
	return nil
}

func cmdKill() error {
	if !serverRunning() {
		fmt.Println("roost isn't running.")
		return nil
	}
	if _, err := save(); err != nil {
		return err
	}
	// Mark as restoring so nothing overwrites the save while panes close.
	tmux("set-option", "-g", "@roost_restoring", "1")
	time.Sleep(100 * time.Millisecond)
	tmux("kill-server")
	fmt.Println("Saved and closed. Run 'roost' to bring everything back.")
	return nil
}

const keysHelp = `
  Switch tabs
    Cmd-← / Cmd-→ ............. previous / next tab
    click a tab ............... go to it

  Move tabs
    Cmd-Shift-← / → ........... move this tab left / right
    drag a tab, drop it ....... move it there (past the end = last)
    right-click a tab ......... move / rename / close menu

  New tab
    click "+ new" ............. new tab (pick a folder; Tab = CLI)
    Ctrl-Space, then n ........ new tab running your CLI
    Ctrl-Space, then t ........ new shell tab in this folder

  "Ctrl-Space, then" = hold Control, tap Space, let go,
  then press the key on its own. More of those:
    1-9  go to tab      r  rename tab      x  close tab
    |    split right    -  split down      z  zoom split
    d    detach         s  save now        ?  this help
    c    check output (✓ passed  ✗ failed  ◌ running  ? approve)
    <    move left      >  move right

  Mouse: double-click a tab to rename · scroll · drag to copy
  Tabs marked $ aren't running an AI CLI (README: Tab markers)
`

// helpMenu waits for a key while the help popup is open, so it doubles as a
// menu: n / t open tabs right away, anything else closes it.
func helpMenu() error {
	fmt.Print("\n  \x1b[1mn\x1b[0m new tab · \x1b[1mt\x1b[0m shell tab · any other key closes")
	fd := int(os.Stdin.Fd())
	old, err := term.MakeRaw(fd)
	if err != nil {
		return err
	}
	b := make([]byte, 8)
	n, _ := os.Stdin.Read(b)
	term.Restore(fd, old)
	if n != 1 {
		return nil
	}
	switch b[0] {
	case 'n':
		return pick()
	case 't':
		cwd, _ := os.Getwd()
		_, err := newTab(cwd, "", "")
		return err
	}
	return nil
}
