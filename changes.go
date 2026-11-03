package main

// "What's new": CHANGELOG.md is built into the binary. The first time you
// open roost after it updated, a popup shows the notes for every version
// since the one you had; `roost changes` shows them any time.

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/term"
)

//go:embed CHANGELOG.md
var changelogMD string

type changeSection struct {
	Heading    string // "v0.4.2 — 2026-11-03" or "Unreleased"
	Body       string
	Ver        semver
	Unreleased bool
}

var changeHeadingRe = regexp.MustCompile(`^## (v\d+\.\d+\.\d+)\b`)

func parseChangelog(md string) []changeSection {
	var out []changeSection
	var cur *changeSection
	for _, line := range strings.Split(md, "\n") {
		if strings.HasPrefix(line, "## ") {
			if cur != nil {
				out = append(out, *cur)
			}
			cur = &changeSection{Heading: strings.TrimSpace(line[3:])}
			if m := changeHeadingRe.FindStringSubmatch(line); m != nil {
				cur.Ver, _ = parseVersion(m[1])
			} else if strings.EqualFold(cur.Heading, "Unreleased") {
				cur.Unreleased = true
			} else {
				cur = nil // not a version section
			}
			continue
		}
		if cur != nil {
			cur.Body += line + "\n"
		}
	}
	if cur != nil {
		out = append(out, *cur)
	}
	for i := range out {
		out[i].Body = strings.TrimSpace(out[i].Body)
	}
	return out
}

// changesBetween returns the sections newer than from, up to and including
// to (newest first). A dev build (to not a version) also gets Unreleased.
func changesBetween(md, from, to string) []changeSection {
	fv, fok := parseVersion(from)
	tv, tok := parseVersion(to)
	var out []changeSection
	for _, s := range parseChangelog(md) {
		if s.Unreleased {
			if !tok {
				out = append(out, s)
			}
			continue
		}
		if fok && !s.Ver.newerThan(fv) {
			continue
		}
		if !tok || !s.Ver.newerThan(tv) {
			out = append(out, s)
		}
	}
	return out
}

func formatChanges(secs []changeSection) string {
	var b strings.Builder
	for i, s := range secs {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "\x1b[1m%s\x1b[0m\n\n%s\n", s.Heading, s.Body)
	}
	return b.String()
}

func seenVersionPath() string { return filepath.Join(homeDir(), "seen-version") }

func readSeenVersion() string {
	b, _ := os.ReadFile(seenVersionPath())
	return strings.TrimSpace(string(b))
}

func writeSeenVersion(v string) {
	if os.MkdirAll(homeDir(), 0o700) == nil {
		os.WriteFile(seenVersionPath(), []byte(strings.TrimPrefix(v, "v")+"\n"), 0o600)
	}
}

// existingInstall: roost has been used here before (vs. a fresh install).
func existingInstall() bool {
	for _, p := range []string{statePath(), settingsConfPath(), recentPath()} {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

// previousRelease is the newest changelog version older than v ("" if none).
func previousRelease(md, v string) string {
	cur, ok := parseVersion(v)
	if !ok {
		return ""
	}
	var best semver
	found := false
	for _, s := range parseChangelog(md) {
		if !s.Unreleased && cur.newerThan(s.Ver) && (!found || s.Ver.newerThan(best)) {
			best, found = s.Ver, true
		}
	}
	if !found {
		return ""
	}
	return fmt.Sprintf("%d.%d.%d", best[0], best[1], best[2])
}

// whatsNewHook is the client-attached hook slot used for the one-time popup.
const whatsNewHook = "client-attached[77]"

// changesToShow decides, before roost opens (and creates any state of its
// own), whether to show "what's new", returning the version to show changes
// since ("" for none). A first install just records the version.
func changesToShow() string {
	if isDevBuild() {
		return ""
	}
	seen := readSeenVersion()
	cur, ok := parseVersion(version)
	if !ok {
		return ""
	}
	sv, sok := parseVersion(seen)
	if !sok && existingInstall() {
		// Updated from a roost that didn't track this yet: show this
		// version's notes (everything after the previous release).
		seen = previousRelease(changelogMD, version)
		sv, sok = parseVersion(seen)
	}
	writeSeenVersion(version)
	if !sok || !cur.newerThan(sv) || len(changesBetween(changelogMD, seen, version)) == 0 {
		return ""
	}
	return seen
}

// armChangesPopup shows "what's new since" in a popup on the next attach.
func armChangesPopup(since string) {
	if _, ok := parseVersion(since); !ok {
		return
	}
	inner := shellQuote(selfPath()) + " changes --popup --since " + since + " --client '#{client_name}'"
	tmux("set-hook", "-g", whatsNewHook, "run-shell -b "+tmuxQuote(inner))
}

// cmdChanges implements `roost changes`:
//
//	roost changes            what's new in this version
//	roost changes --since V  everything newer than V
//	roost changes --all      the whole changelog
func cmdChanges(args []string) error {
	if hasFlag(args, "--popup") { // the one-time hook: show it, then remove the hook
		tmux("set-hook", "-gu", whatsNewHook)
		client := flagValue(args, "--client")
		since := flagValue(args, "--since")
		if _, ok := parseVersion(since); !ok {
			return errors.New("--popup needs --since VERSION")
		}
		a := []string{"display-popup"}
		if clientNameRe.MatchString(client) {
			a = append(a, "-c", client)
		}
		show := shellQuote(selfPath()) + " changes --color --since " + since + " | less -R -P'  q to close, arrows to scroll'"
		a = append(a, "-E", "-w", "80", "-h", "70%", "-T", " What's new in roost "+versionLabel()+" ", show)
		_, err := tmux(a...)
		return err
	}
	var secs []changeSection
	switch {
	case hasFlag(args, "--all"):
		secs = parseChangelog(changelogMD)
	case flagValue(args, "--since") != "":
		secs = changesBetween(changelogMD, flagValue(args, "--since"), version)
	default:
		for _, s := range parseChangelog(changelogMD) {
			if v, ok := parseVersion(version); (ok && s.Ver == v) || (!ok && s.Unreleased) {
				secs = []changeSection{s}
			}
		}
	}
	if len(secs) == 0 {
		fmt.Println("No notes for this version. `roost changes --all` shows every release.")
	} else {
		out := formatChanges(secs)
		if !hasFlag(args, "--color") && !term.IsTerminal(int(os.Stdout.Fd())) {
			out = regexp.MustCompile("\x1b\\[[0-9;]*m").ReplaceAllString(out, "")
		}
		fmt.Print(out)
	}
	if hasFlag(args, "--wait") {
		fmt.Print("\n\x1b[38;5;242m  roost changes --all shows every release · press any key to close\x1b[0m")
		fd := int(os.Stdin.Fd())
		if old, err := term.MakeRaw(fd); err == nil {
			os.Stdin.Read(make([]byte, 8))
			term.Restore(fd, old)
		}
	}
	return nil
}
