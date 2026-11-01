package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/term"
)

// recentDirs lists folders you've used Claude in, most recent first, based
// on the transcripts in ~/.claude/projects.
func recentDirs() []string {
	projects, _ := filepath.Glob(filepath.Join(claudeDir(), "projects", "*"))
	type item struct {
		dir string
		t   time.Time
	}
	seen := map[string]bool{}
	var items []item
	for _, p := range projects {
		files, _ := filepath.Glob(filepath.Join(p, "*.jsonl"))
		var newest string
		var nt time.Time
		for _, f := range files {
			if fi, err := os.Stat(f); err == nil && fi.ModTime().After(nt) {
				newest, nt = f, fi.ModTime()
			}
		}
		if newest == "" {
			continue
		}
		d := cwdFromTranscript(newest)
		if d == "" || seen[d] {
			continue
		}
		if fi, err := os.Stat(d); err != nil || !fi.IsDir() {
			continue
		}
		seen[d] = true
		items = append(items, item{d, nt})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].t.After(items[j].t) })
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.dir
	}
	return out
}

func cwdFromTranscript(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for i := 0; i < 50 && sc.Scan(); i++ {
		line := sc.Bytes()
		if !strings.Contains(string(line), `"cwd"`) {
			continue
		}
		var v struct {
			Cwd string `json:"cwd"`
		}
		if json.Unmarshal(line, &v) == nil && v.Cwd != "" {
			return v.Cwd
		}
	}
	return ""
}

func tilde(p string) string {
	h, _ := os.UserHomeDir()
	if h != "" && (p == h || strings.HasPrefix(p, h+"/")) {
		return "~" + p[len(h):]
	}
	return p
}

func expandTilde(p string) string {
	if strings.HasPrefix(p, "~") {
		h, _ := os.UserHomeDir()
		return h + p[1:]
	}
	return p
}

// fuzzy reports whether all chars of q appear in s in order.
func fuzzy(s, q string) bool {
	s, q = strings.ToLower(s), strings.ToLower(q)
	i := 0
	for _, c := range s {
		if i < len(q) && rune(q[i]) == c {
			i++
		}
	}
	return i == len(q)
}

// pick shows a tiny folder picker and opens a Claude tab in the choice.
func pick() error {
	cwd, _ := os.Getwd()
	all := []string{cwd}
	for _, d := range recentDirs() {
		if d != cwd {
			all = append(all, d)
		}
	}

	fd := int(os.Stdin.Fd())
	old, err := term.MakeRaw(fd)
	if err != nil {
		return err
	}
	defer term.Restore(fd, old)
	w := os.Stdout
	fmt.Fprint(w, "\x1b[?25l")
	defer fmt.Fprint(w, "\x1b[?25h\x1b[2J\x1b[H")

	query, sel, top := "", 0, 0
	buf := make([]byte, 16)
	for {
		// Build the visible list.
		var list []string
		q := strings.TrimSpace(query)
		if q != "" && (strings.HasPrefix(q, "/") || strings.HasPrefix(q, "~") || strings.HasPrefix(q, ".")) {
			if fi, err := os.Stat(expandTilde(q)); err == nil && fi.IsDir() {
				p, _ := filepath.Abs(expandTilde(q))
				list = append(list, p)
			}
		}
		list = append(list, filterDirs(all, q)...)
		width, h, _ := term.GetSize(fd)
		rows := h - 4
		if rows < 3 {
			rows = 3
		}
		if sel >= len(list) {
			sel = len(list) - 1
		}
		if sel < 0 {
			sel = 0
		}
		if sel < top {
			top = sel
		}
		if sel >= top+rows {
			top = sel - rows + 1
		}

		var b strings.Builder
		b.WriteString("\x1b[2J\x1b[H")
		fmt.Fprintf(&b, " \x1b[1;38;2;224;71;58m›\x1b[0m %s\x1b[38;5;242m▏\x1b[0m\r\n\r\n", query)
		for i := top; i < len(list) && i < top+rows; i++ {
			extra := ""
			if i == 0 && list[i] == cwd && q == "" {
				extra = "(current)"
			}
			fmt.Fprint(&b, row(list[i], extra, i == sel, width))
		}
		if len(list) == 0 {
			b.WriteString("    \x1b[38;5;242mno matches — type a path like ~/code/thing\x1b[0m\r\n")
		}
		fmt.Fprintf(&b, "\x1b[%d;1H \x1b[38;5;242mtype to filter · ↑↓ choose · enter open Claude · esc cancel\x1b[0m", h)
		fmt.Fprint(w, b.String())

		n, err := os.Stdin.Read(buf)
		if err != nil {
			return err
		}
		k := buf[:n]
		switch {
		case n == 1 && (k[0] == 27 || k[0] == 3): // esc, ctrl-c
			return nil
		case n == 1 && k[0] == 13: // enter
			if len(list) == 0 {
				continue
			}
			term.Restore(fd, old)
			_, err := newTab(list[sel], "", false)
			return err
		case n == 1 && (k[0] == 127 || k[0] == 8):
			if r := []rune(query); len(r) > 0 {
				query = string(r[:len(r)-1])
			}
			sel = 0
		case n == 1 && k[0] == 21: // ctrl-u
			query, sel = "", 0
		case n == 1 && (k[0] == 16 || k[0] == 11): // ctrl-p / ctrl-k
			sel--
		case n == 1 && (k[0] == 14 || k[0] == 10): // ctrl-n / ctrl-j
			sel++
		case n >= 3 && k[0] == 27 && k[1] == '[' && k[2] == 'A':
			sel--
		case n >= 3 && k[0] == 27 && k[1] == '[' && k[2] == 'B':
			sel++
		case k[0] >= 32 && k[0] != 127:
			query += string(k)
			sel = 0
		}
	}
}

// filterDirs ranks folder-name matches first, then path substring matches,
// and only falls back to loose fuzzy matching when nothing else hits.
func filterDirs(all []string, q string) []string {
	if q == "" {
		return all
	}
	lq := strings.ToLower(q)
	var base, sub, fz []string
	for _, d := range all {
		t := strings.ToLower(tilde(d))
		switch {
		case strings.Contains(strings.ToLower(filepath.Base(d)), lq):
			base = append(base, d)
		case strings.Contains(t, lq):
			sub = append(sub, d)
		case fuzzy(filepath.Base(d), q):
			fz = append(fz, d)
		}
	}
	out := append(base, sub...)
	if len(out) == 0 {
		out = fz
	}
	return out
}

// row renders "name  ~/parent/path" on one line, trimming the path from
// the left so it never wraps.
func row(dir, extra string, selected bool, width int) string {
	name := filepath.Base(dir)
	parent := tilde(filepath.Dir(dir))
	if extra != "" {
		parent += "  " + extra
	}
	room := width - 8 - len([]rune(name))
	if room < 0 {
		room = 0
	}
	if r := []rune(parent); len(r) > room {
		if room > 1 {
			parent = "…" + string(r[len(r)-room+1:])
		} else {
			parent = ""
		}
	}
	if selected {
		return fmt.Sprintf(" \x1b[48;5;238;38;5;255;1m ▸ %s \x1b[22;38;5;246m%s \x1b[0m\r\n", name, parent)
	}
	return fmt.Sprintf("    \x1b[38;5;252m%s \x1b[38;5;242m%s\x1b[0m\r\n", name, parent)
}
