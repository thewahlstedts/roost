package main

import "testing"

func TestColourSGR(t *testing.T) {
	const fb = "FALLBACK"
	cases := map[string]string{
		"#e0473a":     "38;2;224;71;58",
		"#E0473A":     "38;2;224;71;58",
		"colour114":   "38;5;114",
		"color7":      "38;5;7",
		"green":       "32",
		"brightgreen": "92",
		"colour999":   fb,
		"#zzz":        fb,
		"chartreuse":  fb,
		"":            fb,
	}
	for in, want := range cases {
		if got := colourSGR(in, fb); got != want {
			t.Errorf("colourSGR(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidSessionID(t *testing.T) {
	good := "a87ff3d8-7d4f-4eb4-b1b9-e7ad7df2fad9"
	if !validSessionID(good) {
		t.Errorf("%s should be valid", good)
	}
	for _, bad := range []string{"", "x; rm -rf ~", good + " ", "../" + good, "a87ff3d8-7d4f-4eb4-b1b9"} {
		if validSessionID(bad) {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

func TestSplitFields(t *testing.T) {
	for _, line := range []string{"1" + sep + "api" + sep + "/tmp"} {
		if got := splitFields(line); len(got) != 3 || got[1] != "api" || got[2] != "/tmp" {
			t.Errorf("splitFields(%q) = %q", line, got)
		}
	}
}

func TestTmuxVersionOK(t *testing.T) {
	cases := map[string]bool{
		"tmux 3.8": true, "tmux 3.3a": true, "tmux 3.4": true, "tmux next-3.6": true,
		"tmux 4.0": true, "tmux 3.10": true, "tmux master": true, "tmux 3.2a": false, "tmux 2.9": false,
	}
	for in, want := range cases {
		if got := tmuxVersionOK(in); got != want {
			t.Errorf("tmuxVersionOK(%q) = %v, want %v", in, got, want)
		}
	}
}
