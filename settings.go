package main

// settings.conf holds the answers `roost setup` collected. It's managed by
// roost (rewritten as a whole) and loaded before custom.conf, so anything set
// by hand there still wins. A setting counts as "answered" once it's in
// either file; `roost setup` only asks about the rest, so an update that adds
// a new question asks just that one.

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var settingLineRe = regexp.MustCompile(`^\s*set(?:-option)?\s+-g\s+(@roost_[a-z0-9_]+)\s+(.*?)\s*$`)

// unquote reverses tmuxQuote (and accepts single-quoted values, which tmux
// takes literally).
func unquote(v string) string {
	if len(v) >= 2 && v[0] == '\'' && v[len(v)-1] == '\'' {
		return v[1 : len(v)-1]
	}
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		var b strings.Builder
		in := v[1 : len(v)-1]
		for i := 0; i < len(in); i++ {
			if in[i] == '\\' && i+1 < len(in) {
				i++
			}
			b.WriteByte(in[i])
		}
		return b.String()
	}
	return v
}

// readConfSettings returns the @roost_* options a conf file sets (last wins).
func readConfSettings(path string) map[string]string {
	m := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		return m
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if g := settingLineRe.FindStringSubmatch(sc.Text()); g != nil {
			m[g[1]] = unquote(g[2])
		}
	}
	return m
}

func readSettings() map[string]string { return readConfSettings(settingsConfPath()) }

// writeSetting stores key=value in settings.conf, keeping the other answers.
func writeSetting(key, value string) error {
	m := readSettings()
	m[key] = value
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("# Written by `roost setup` (re-ask everything: roost setup --all).\n")
	b.WriteString("# custom.conf is loaded after this file, so settings there win.\n")
	for _, k := range keys {
		fmt.Fprintf(&b, "set -g %s %s\n", k, tmuxQuote(m[k]))
	}
	if err := os.MkdirAll(homeDir(), 0o700); err != nil {
		return err
	}
	return os.WriteFile(settingsConfPath(), []byte(b.String()), 0o600)
}

// tmuxQuote double-quotes a value for a tmux config line.
func tmuxQuote(v string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `\$`)
	return `"` + r.Replace(v) + `"`
}

// answered: set by setup (settings.conf) or by hand (custom.conf).
func answered(key string) bool {
	if _, ok := readSettings()[key]; ok {
		return true
	}
	_, ok := readConfSettings(customConfPath())[key]
	return ok
}

// setupQuestion is one thing `roost setup` asks about.
type setupQuestion struct {
	Key   string // the @roost_ option it sets
	Label string // shown in "new settings: …"
}

// setupQuestions, in the order setup asks them. Add new ones at the end:
// existing users are then asked only the new question after updating.
var setupQuestions = []setupQuestion{
	{"@roost_default_agent", "your CLI"},
	{"@roost_source_dirs", "where your code lives"},
}

// pendingQuestions are the setup questions not answered yet.
func pendingQuestions() []setupQuestion {
	var out []setupQuestion
	for _, q := range setupQuestions {
		if !answered(q.Key) {
			out = append(out, q)
		}
	}
	return out
}

func pendingSummary() string {
	var labels []string
	for _, q := range pendingQuestions() {
		labels = append(labels, q.Label)
	}
	return strings.Join(labels, ", ")
}

// ---- source folders ----

const defaultSourceDepth, maxSourceDepth = 4, 10

// sourceDepth is how many levels below each source folder to look for repos.
func sourceDepth() int {
	if v, ok := tmuxOption("@roost_source_depth"); ok {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 {
			return min(n, maxSourceDepth)
		}
	}
	return defaultSourceDepth
}

// Folders in $HOME that never hold your code (and are slow to walk).
var homeSkip = map[string]bool{
	"Library": true, "Applications": true, "Pictures": true, "Music": true,
	"Movies": true, "Downloads": true, "Public": true, "snap": true,
}

// suggestSourceDirs scans $HOME (bounded) and suggests the top-level folders
// that hold git repos, most repos first, plus the depth needed to reach them.
func suggestSourceDirs(budget time.Duration) (dirs []string, depth int) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, defaultSourceDepth
	}
	entries, err := os.ReadDir(home)
	if err != nil {
		return nil, defaultSourceDepth
	}
	deadline := time.Now().Add(budget)
	count := map[string]int{}
	deepest := 1
	for _, e := range entries {
		n := e.Name()
		if !e.IsDir() || strings.HasPrefix(n, ".") || homeSkip[n] {
			continue
		}
		root := filepath.Join(home, n)
		for _, r := range findRepos([]string{root}, 6, 2000, deadline) {
			count[root]++
			rel, _ := filepath.Rel(root, r)
			if d := strings.Count(rel, string(filepath.Separator)) + 1; d > deepest {
				deepest = d
			}
		}
	}
	for d := range count {
		dirs = append(dirs, d)
	}
	sort.Slice(dirs, func(i, j int) bool {
		if count[dirs[i]] != count[dirs[j]] {
			return count[dirs[i]] > count[dirs[j]]
		}
		return dirs[i] < dirs[j]
	})
	if len(dirs) > 3 {
		dirs = dirs[:3]
	}
	return dirs, max(deepest, defaultSourceDepth)
}

// askSourceDirs asks where code lives and how deep to look.
func askSourceDirs(curDirs string, curDepth int, in *bufio.Reader) (dirs string, depth int) {
	fmt.Println()
	fmt.Println("Where do you keep your code? The '+ new' picker lists every git repo there.")
	sugg, suggDepth := suggestSourceDirs(1500 * time.Millisecond)
	def := curDirs
	if def == "" {
		var t []string
		for _, d := range sugg {
			t = append(t, tilde(d))
		}
		def = strings.Join(t, " ")
	}
	if len(sugg) > 0 {
		var t []string
		for _, d := range sugg {
			t = append(t, tilde(d))
		}
		fmt.Printf("  Found repos under: %s\n", strings.Join(t, ", "))
	}
	for {
		fmt.Printf("Folders, separated by spaces [%s]: ", def)
		line, err := in.ReadString('\n')
		line = strings.TrimSpace(line)
		if line == "" {
			line = def
		}
		var bad []string
		for _, f := range strings.Fields(line) {
			if fi, err := os.Stat(expandTilde(f)); err != nil || !fi.IsDir() {
				bad = append(bad, f)
			}
		}
		if len(bad) == 0 || err != nil {
			dirs = line
			break
		}
		fmt.Printf("  Not a folder: %s\n", strings.Join(bad, ", "))
	}
	if curDepth > 0 {
		suggDepth = curDepth
	}
	for {
		fmt.Printf("How many levels below those to look for repos? [%d]: ", suggDepth)
		line, err := in.ReadString('\n')
		line = strings.TrimSpace(line)
		if line == "" {
			return dirs, suggDepth
		}
		if n, e := strconv.Atoi(line); e == nil && n >= 1 && n <= maxSourceDepth {
			return dirs, n
		}
		if err != nil {
			return dirs, suggDepth
		}
		fmt.Printf("  Enter a number from 1 to %d.\n", maxSourceDepth)
	}
}

// setupSourceDirs handles the source-folder question during `roost setup`.
func setupSourceDirs(dirsFlag, depthFlag string, reask, interactive bool, in *bufio.Reader) error {
	if depthFlag != "" {
		n, err := strconv.Atoi(depthFlag)
		if err != nil || n < 1 || n > maxSourceDepth {
			return fmt.Errorf("--source-depth must be 1-%d", maxSourceDepth)
		}
		if err := writeSetting("@roost_source_depth", strconv.Itoa(n)); err != nil {
			return err
		}
	}
	if dirsFlag != "" {
		if err := writeSetting("@roost_source_dirs", dirsFlag); err != nil {
			return err
		}
	}
	cur := readSettings()
	if dirsFlag == "" && (reask || !answered("@roost_source_dirs")) && interactive {
		curDepth, _ := strconv.Atoi(cur["@roost_source_depth"])
		dirs, depth := askSourceDirs(cur["@roost_source_dirs"], curDepth, in)
		if err := writeSetting("@roost_source_dirs", dirs); err != nil {
			return err
		}
		if err := writeSetting("@roost_source_depth", strconv.Itoa(depth)); err != nil {
			return err
		}
		cur = readSettings()
	}
	if v, ok := cur["@roost_source_dirs"]; ok {
		depth := cur["@roost_source_depth"]
		if depth == "" {
			depth = strconv.Itoa(defaultSourceDepth)
		}
		fmt.Printf("Code folders: %s (%s levels deep; change with: roost setup --source-dirs \"…\" --source-depth N)\n", v, depth)
	}
	return nil
}
