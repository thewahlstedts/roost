package main

import (
	"reflect"
	"testing"
)

func TestSplitCmd(t *testing.T) {
	cases := []struct {
		in   []string
		cmd  string
		rest []string
	}{
		{nil, "", nil},
		{[]string{"--version"}, "--version", []string{}},
		{[]string{"-v"}, "-v", []string{}},
		{[]string{"-V"}, "-V", []string{}},
		{[]string{"version"}, "version", []string{}},
		{[]string{"--native"}, "", []string{"--native"}},
		{[]string{"new", "~/code"}, "new", []string{"~/code"}},
	}
	for _, c := range cases {
		cmd, rest := splitCmd(c.in)
		if cmd != c.cmd || !reflect.DeepEqual(rest, c.rest) {
			t.Errorf("splitCmd(%q) = %q %q, want %q %q", c.in, cmd, rest, c.cmd, c.rest)
		}
	}
}
