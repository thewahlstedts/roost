package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestParseVersion(t *testing.T) {
	a, ok := parseVersion("v0.10.0")
	b, ok2 := parseVersion("0.9.12")
	if !ok || !ok2 || !a.newerThan(b) || b.newerThan(a) || a.newerThan(a) {
		t.Fatal("version comparison wrong")
	}
	for _, bad := range []string{"dev", "", "1.2", "v1.3.0-rc1", "1.x.0"} {
		if _, ok := parseVersion(bad); ok {
			t.Errorf("parseVersion(%q) should fail", bad)
		}
	}
}

// fakeRelease serves a GitHub-like release of roost "9.9.9" whose binary is a
// shell script, signed with a fresh key that the test installs as trusted.
type fakeRelease struct {
	sums, sig, tgz []byte
	srv            *httptest.Server
}

func newFakeRelease(t *testing.T, binVersion string) *fakeRelease {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	oldKeys, oldAPI, oldDL, oldAllowed, oldTarget := releaseKeys, releaseAPI, releaseDownload, allowedURL, updateTarget
	t.Cleanup(func() {
		releaseKeys, releaseAPI, releaseDownload, allowedURL, updateTarget = oldKeys, oldAPI, oldDL, oldAllowed, oldTarget
	})
	releaseKeys = []string{base64.StdEncoding.EncodeToString(pub)}
	allowedURL = func(*url.URL) bool { return true }

	script := []byte("#!/bin/sh\necho roost " + binVersion + "\n")
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	tw.WriteHeader(&tar.Header{Name: "README.md", Mode: 0o644, Size: 2, Typeflag: tar.TypeReg})
	tw.Write([]byte("hi"))
	tw.WriteHeader(&tar.Header{Name: "roost", Mode: 0o755, Size: int64(len(script)), Typeflag: tar.TypeReg})
	tw.Write(script)
	tw.Close()
	gz.Close()

	f := &fakeRelease{tgz: buf.Bytes()}
	asset := fmt.Sprintf("roost_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	h := sha256.Sum256(f.tgz)
	f.sums = []byte(hex.EncodeToString(h[:]) + "  " + asset + "\n")
	f.sig = []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(priv, f.sums)))

	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/latest":
			fmt.Fprint(w, `{"tag_name":"v9.9.9"}`)
		case strings.HasSuffix(r.URL.Path, "/checksums.txt"):
			w.Write(f.sums)
		case strings.HasSuffix(r.URL.Path, "/checksums.txt.sig"):
			if f.sig == nil {
				http.NotFound(w, r)
				return
			}
			w.Write(f.sig)
		case strings.HasSuffix(r.URL.Path, "/"+asset):
			w.Write(f.tgz)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.srv.Close)
	releaseAPI = f.srv.URL + "/latest"
	releaseDownload = f.srv.URL + "/download"
	return f
}

func targetFile(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "roost")
	if err := os.WriteFile(p, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	updateTarget = func() (string, error) { return p, nil }
	return p
}

func withVersion(t *testing.T, v string) {
	old := version
	version = v
	t.Cleanup(func() { version = old })
}

func TestUpdateInstallsSignedRelease(t *testing.T) {
	newFakeRelease(t, "9.9.9")
	p := targetFile(t)
	withVersion(t, "0.2.0")
	res, err := checkUpdate(true, false)
	if err != nil || !res.Updated || res.Latest != "v9.9.9" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if b, _ := os.ReadFile(p); !bytes.Contains(b, []byte("roost 9.9.9")) {
		t.Fatalf("binary not replaced: %q", b)
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(p), ".roost.new.*")); len(left) > 0 {
		t.Errorf("temp files left: %v", left)
	}
}

func TestUpdateRefuses(t *testing.T) {
	cases := map[string]func(f *fakeRelease){
		"bad signature": func(f *fakeRelease) { f.sig = []byte(base64.StdEncoding.EncodeToString(make([]byte, 64))) },
		"unsigned":      func(f *fakeRelease) { f.sig = nil },
		"tampered sums": func(f *fakeRelease) { f.sums = append(f.sums, []byte("# extra\n")...) },
		"tampered tgz":  func(f *fakeRelease) { f.tgz = append(f.tgz, 0) },
	}
	for name, breakIt := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFakeRelease(t, "9.9.9")
			breakIt(f)
			p := targetFile(t)
			withVersion(t, "0.2.0")
			if _, err := checkUpdate(true, false); err == nil {
				t.Fatal("expected refusal")
			}
			if b, _ := os.ReadFile(p); string(b) != "old" {
				t.Fatal("binary was replaced")
			}
		})
	}
	t.Run("self-check fails", func(t *testing.T) {
		newFakeRelease(t, "6.6.6") // archive claims to be a different version
		p := targetFile(t)
		withVersion(t, "0.2.0")
		if _, err := checkUpdate(true, false); err == nil {
			t.Fatal("expected refusal")
		}
		if b, _ := os.ReadFile(p); string(b) != "old" {
			t.Fatal("binary was replaced")
		}
	})
}

func TestUpdateNoopWhenCurrent(t *testing.T) {
	newFakeRelease(t, "9.9.9")
	p := targetFile(t)
	withVersion(t, "9.9.9")
	res, err := checkUpdate(true, false)
	if err != nil || res.Newer || res.Updated {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if b, _ := os.ReadFile(p); string(b) != "old" {
		t.Fatal("binary was replaced")
	}
}

func TestDefaultAllowedURL(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://github.com/x":                    true,
		"https://api.github.com/x":                true,
		"https://objects.githubusercontent.com/x": true,
		"http://github.com/x":                     false,
		"https://github.com.evil.example/x":       false,
		"https://evilgithubusercontent.com/x":     false,
	} {
		u, _ := url.Parse(raw)
		if got := allowedURL(u); got != want {
			t.Errorf("allowedURL(%s) = %v", raw, got)
		}
	}
}
