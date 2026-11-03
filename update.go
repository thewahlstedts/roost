package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// releaseKeys are the ed25519 public keys allowed to sign releases
// (tools/sign). A release whose checksums.txt isn't signed by one of them is
// never installed. To rotate: add the new key here, ship a release signed
// with the old key, then switch the signing secret to the new key.
var releaseKeys = []string{
	"CZJLdLSBeC7EuZuCREEXScolUhtmo/AI/qTZMiSZG48=",
}

const releaseRepo = "thewahlstedts/roost"

// Overridable in tests.
var (
	releaseAPI      = "https://api.github.com/repos/" + releaseRepo + "/releases/latest"
	releaseDownload = "https://github.com/" + releaseRepo + "/releases/download"
	updateTarget    = selfExe // the binary to replace
	allowedURL      = func(u *url.URL) bool {
		h := u.Hostname()
		return u.Scheme == "https" &&
			(h == "github.com" || h == "api.github.com" || strings.HasSuffix(h, ".githubusercontent.com"))
	}
)

const updateEvery = 24 * time.Hour

// isDevBuild: anything not built by the release workflow (which stamps the
// version with -ldflags) is a dev build and never auto-updates.
func isDevBuild() bool { return version == "" || strings.HasPrefix(version, "dev") }

type semver [3]int

// parseVersion accepts "1.2.3" or "v1.2.3". Pre-releases ("1.3.0-rc1") are
// rejected, so they're never installed automatically.
func parseVersion(s string) (semver, bool) {
	parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(s), "v"), ".")
	if len(parts) != 3 {
		return semver{}, false
	}
	var v semver
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return semver{}, false
		}
		v[i] = n
	}
	return v, true
}

func (a semver) newerThan(b semver) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

func httpClient() *http.Client {
	return &http.Client{
		Timeout: 2 * time.Minute,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > 5 || !allowedURL(req.URL) {
				return fmt.Errorf("refusing redirect to %s", req.URL.Redacted())
			}
			return nil
		},
	}
}

func fetch(c *http.Client, rawURL string, max int64) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil || !allowedURL(u) {
		return nil, fmt.Errorf("refusing to download from %s", rawURL)
	}
	req, _ := http.NewRequest("GET", rawURL, nil)
	req.Header.Set("User-Agent", "roost/"+version)
	req.Header.Set("Accept", "application/vnd.github+json, application/octet-stream")
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", rawURL, resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, fmt.Errorf("GET %s: response too large", rawURL)
	}
	return b, nil
}

func latestTag(c *http.Client) (string, error) {
	b, err := fetch(c, releaseAPI, 1<<20)
	if err != nil {
		return "", err
	}
	var rel struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(b, &rel); err != nil || rel.TagName == "" {
		return "", errors.New("could not read the latest release")
	}
	return rel.TagName, nil
}

func verifySignature(msg, sigB64 []byte) error {
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sigB64)))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return errors.New("malformed release signature")
	}
	for _, k := range releaseKeys {
		pub, err := base64.StdEncoding.DecodeString(k)
		if err == nil && len(pub) == ed25519.PublicKeySize && ed25519.Verify(pub, msg, sig) {
			return nil
		}
	}
	return errors.New("release signature doesn't match roost's signing key: not installing")
}

func checksumFor(sums []byte, asset string) (string, error) {
	for _, line := range strings.Split(string(sums), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == asset {
			return strings.ToLower(f[0]), nil
		}
	}
	return "", fmt.Errorf("no checksum for %s", asset)
}

// extractBinary pulls the regular file named "roost" out of a release
// tarball.
func extractBinary(tgz []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(tgz))
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil, errors.New("no roost binary in the release archive")
		}
		if err != nil {
			return nil, err
		}
		if h.Typeflag == tar.TypeReg && filepath.Clean(h.Name) == "roost" {
			const max = 200 << 20
			if h.Size > max {
				return nil, errors.New("roost binary in archive is too large")
			}
			return io.ReadAll(io.LimitReader(tr, max))
		}
	}
}

func selfExe() (string, error) {
	p, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(p)
}

// replaceBinary writes the new binary next to exe, checks it runs and
// reports the expected version, then renames it over exe. Rename swaps the
// directory entry atomically, so running roost processes keep the old file
// and macOS never sees a signed binary rewritten in place.
func replaceBinary(exe string, bin []byte, want string) error {
	dir := filepath.Dir(exe)
	tmp := filepath.Join(dir, fmt.Sprintf(".roost.new.%d", os.Getpid()))
	os.Remove(tmp)
	if err := os.WriteFile(tmp, bin, 0o755); err != nil {
		return fmt.Errorf("can't write to %s (%v): reinstall roost somewhere you own, e.g. ~/.local/bin", dir, err)
	}
	out, err := exec.Command(tmp, "version").Output()
	if got := strings.TrimSpace(string(out)); err != nil || got != "roost "+want {
		os.Remove(tmp)
		return fmt.Errorf("downloaded binary failed its self-check (got %q): not installing", got)
	}
	if err := os.Rename(tmp, exe); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

type updateCheck struct {
	Latest  string // e.g. "v0.3.0"
	Newer   bool   // Latest is newer than this binary
	Updated bool   // and it was installed
}

// checkUpdate finds the latest release and, if install is set and it's
// newer (or force), downloads, verifies and installs it.
func checkUpdate(install, force bool) (updateCheck, error) {
	c := httpClient()
	tag, err := latestTag(c)
	if err != nil {
		return updateCheck{}, err
	}
	res := updateCheck{Latest: tag}
	lv, ok := parseVersion(tag)
	if !ok {
		return res, fmt.Errorf("latest release has an unexpected version %q", tag)
	}
	cur, curOK := parseVersion(version)
	res.Newer = !curOK || lv.newerThan(cur)
	if !install || (!res.Newer && !force) {
		return res, nil
	}

	asset := fmt.Sprintf("roost_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	base := releaseDownload + "/" + url.PathEscape(tag)
	sums, err := fetch(c, base+"/checksums.txt", 1<<20)
	if err != nil {
		return res, err
	}
	sig, err := fetch(c, base+"/checksums.txt.sig", 4<<10)
	if err != nil {
		return res, fmt.Errorf("release %s isn't signed (%v): not installing", tag, err)
	}
	if err := verifySignature(sums, sig); err != nil {
		return res, err
	}
	want, err := checksumFor(sums, asset)
	if err != nil {
		return res, err
	}
	tgz, err := fetch(c, base+"/"+asset, 200<<20)
	if err != nil {
		return res, err
	}
	if got := sha256.Sum256(tgz); hex.EncodeToString(got[:]) != want {
		return res, fmt.Errorf("checksum mismatch for %s: not installing", asset)
	}
	bin, err := extractBinary(tgz)
	if err != nil {
		return res, err
	}
	exe, err := updateTarget()
	if err != nil {
		return res, err
	}
	if err := replaceBinary(exe, bin, strings.TrimPrefix(tag, "v")); err != nil {
		return res, err
	}
	res.Updated = true
	return res, nil
}

func cmdUpdate(args []string) error {
	if hasFlag(args, "--auto") {
		runAutoUpdate()
		return nil
	}
	force := hasFlag(args, "--force")
	if hasFlag(args, "--check") {
		res, err := checkUpdate(false, false)
		if err != nil {
			return err
		}
		if res.Newer {
			fmt.Printf("roost %s is available (you have %s). Run: roost update\n", res.Latest, version)
		} else {
			fmt.Printf("roost %s is up to date.\n", version)
		}
		return nil
	}
	if isDevBuild() && !force {
		return fmt.Errorf("this is a development build (%s); 'roost update --force' replaces it with the latest release", version)
	}
	res, err := checkUpdate(true, force)
	if err != nil {
		return err
	}
	if res.Updated {
		if serverRunning() {
			tmux("set", "-g", "@roost_version", res.Latest)
		}
		fmt.Printf("Updated roost %s → %s. Open roost again to load everything new.\n", version, res.Latest)
		if notes := newBinaryOutput("changes", "--since", version); notes != "" && !strings.HasPrefix(notes, "No notes") {
			fmt.Printf("\nWhat's new:\n\n%s\n", notes)
			writeSeenVersion(res.Latest) // shown here, so no popup next time
		}
		if p := newSetupQuestions(); p != "" {
			fmt.Println(p)
		}
	} else {
		fmt.Printf("roost %s is up to date.\n", version)
	}
	return nil
}

// newSetupQuestions asks the just-installed binary (which may know setup
// questions this one doesn't) whether anything new needs answering.
func newSetupQuestions() string { return newBinaryOutput("setup", "--check") }

// newBinaryOutput runs the just-installed roost and returns its output.
func newBinaryOutput(args ...string) string {
	exe, err := updateTarget()
	if err != nil {
		return ""
	}
	out, err := exec.Command(exe, args...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func updateStampPath() string { return filepath.Join(homeDir(), "update-check") }
func updateLogPath() string   { return filepath.Join(homeDir(), "update.log") }

// maybeAutoUpdate starts a background update check at most once a day when
// you open roost. It never delays startup and never runs for dev builds.
func maybeAutoUpdate() {
	if isDevBuild() || os.Getenv("ROOST_NO_UPDATE") != "" {
		return
	}
	if fi, err := os.Stat(updateStampPath()); err == nil && time.Since(fi.ModTime()) < updateEvery {
		return
	}
	if err := os.WriteFile(updateStampPath(), []byte(time.Now().Format(time.RFC3339)+"\n"), 0o600); err != nil {
		return
	}
	logf, err := os.OpenFile(updateLogPath(), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer logf.Close()
	cmd := exec.Command(selfPath(), "update", "--auto")
	cmd.Stdout, cmd.Stderr = logf, logf
	cmd.Env = envWithoutTMUX()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // outlive this process
	if cmd.Start() == nil {
		cmd.Process.Release()
	}
}

// runAutoUpdate is `roost update --auto`, run in the background.
// @roost_auto_update: on (default) installs, notify only tells you, off skips.
func runAutoUpdate() {
	mode := "on"
	if v, ok := tmuxOption("@roost_auto_update"); ok && v != "" {
		mode = v
	}
	if mode == "off" || isDevBuild() {
		return
	}
	stamp := time.Now().Format("2006-01-02 15:04:05")
	res, err := checkUpdate(mode == "on", false)
	switch {
	case err != nil:
		fmt.Printf("%s update check failed: %v\n", stamp, err)
	case res.Updated:
		fmt.Printf("%s updated %s -> %s\n", stamp, version, res.Latest)
		tmux("set", "-g", "@roost_version", res.Latest) // status bar shows it now
		msg := "roost: updated to " + res.Latest + " — open roost again to load everything new"
		if newSetupQuestions() != "" {
			msg = "roost: updated to " + res.Latest + " — run 'roost setup' for its new settings"
		}
		tmux("display-message", "-d", "8000", msg)
	case res.Newer:
		fmt.Printf("%s %s available\n", stamp, res.Latest)
		tmux("display-message", "-d", "8000", "roost: "+res.Latest+" is available — run 'roost update'")
	default:
		fmt.Printf("%s up to date (%s)\n", stamp, version)
	}
}
