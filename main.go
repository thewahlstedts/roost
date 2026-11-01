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

const version = "0.1.0"

const usage = `roost — keep your Claude tabs alive, even across restarts.

  roost                 open roost (restores your tabs after a restart)
  roost new [dir]       new Claude tab in dir (default: current folder)
      --shell             plain shell tab instead of Claude
      --name NAME         fixed tab name (default: follows the folder)
  roost ls              list tabs (running, or last saved)
  roost save            save now (roost also saves automatically)
  roost restore         rebuild tabs from the last save
      --list              show older saves;  roost restore 3  uses one
  roost kill            save, then close everything
  roost setup           install the Claude hook (run once)
      --remove            uninstall it
  roost keys            show keyboard shortcuts

  Add --native to 'roost' to use iTerm's native tabs (tmux -CC).
`

func main() {
	args := os.Args[1:]
	cmd := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
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
		err = setup(hasFlag(args, "--remove"))
		if err == nil {
			err = setupITerm(hasFlag(args, "--remove"))
		}
		if err == nil && serverRunning() {
			// Apply the latest config (e.g. Cmd+arrow keys) to the live roost.
			if err = writeConf(); err == nil {
				_, err = tmux("source-file", confPath())
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
	} else if st, err := loadState(statePath()); err == nil && len(st.Windows) > 0 {
		tabs, resumed, err := restore(st)
		if err != nil {
			return err
		}
		notice = fmt.Sprintf("roost: restored %d tab(s), resumed %d Claude session(s)", tabs, resumed)
	} else {
		cwd, _ := os.Getwd()
		if _, err := newTab(cwd, "", true); err != nil {
			return err
		}
		notice = "roost: welcome! Click '+ new' or press Ctrl-Space n for a Claude tab"
	}
	if !hooksInstalled() {
		notice = "roost: run 'roost setup' once so Claude sessions can be resumed after a restart"
	}
	if notice != "" {
		tmux("run-shell", "-b", "sleep 0.8; tmux -L "+shellQuote(socketName)+" display-message -d 6000 "+shellQuote(notice))
	}
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
		if a == "--name" || a == "--dir" {
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
	if _, err := newTab(dir, flagValue(args, "--name"), hasFlag(args, "--shell")); err != nil {
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
func newTab(dir, name string, shell bool) (string, error) {
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
	if !shell {
		id := uuid()
		tmux("set-option", "-p", "-t", pane, "@roost_session", id)
		tmux("send-keys", "-t", pane, claudeCmd()+" --session-id "+id, "Enter")
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
			kind := "shell "
			if p.Claude != "" {
				kind = "claude"
			}
			head := fmt.Sprintf("%s %d %-18s", mark, i+1, w.Name)
			if j > 0 {
				head = strings.Repeat(" ", len(head))
			}
			fmt.Printf("%s  %s  %s\n", head, kind, tilde(p.Dir))
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

  New tab
    click "+ new" ............. new Claude tab (pick a folder)
    Ctrl-Space, then n ........ new Claude tab
    Ctrl-Space, then t ........ new shell tab in this folder

  "Ctrl-Space, then" = hold Control, tap Space, let go,
  then press the key on its own. More of those:
    1-9  go to tab      r  rename tab      x  close tab
    |    split right    -  split down      z  zoom split
    d    detach         s  save now        ?  this help

  Mouse: double-click a tab to rename · scroll · drag to copy
  Tabs marked $ aren't running Claude (README: Tab markers)
`

// helpMenu waits for a key while the help popup is open, so it doubles as a
// menu: n / t open tabs right away, anything else closes it.
func helpMenu() error {
	fmt.Print("\n  \x1b[1mn\x1b[0m new Claude tab · \x1b[1mt\x1b[0m shell tab · any other key closes")
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
		_, err := newTab(cwd, "", true)
		return err
	}
	return nil
}
