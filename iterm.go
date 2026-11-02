package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// Escape sequences iTerm sends for Cmd+Left / Cmd+Right in the roost
// profile. tmux maps them back via user-keys (see tmux.go).
const (
	seqPrevTab = "[9001~"
	seqNextTab = "[9002~"
)

//go:embed assets/roost.png
var iconPNG []byte

const itermProfileGUID = "roost-0b6f3c1e-7a52-4d2b-9a61-roost-profile"

func itermProfilePath() string {
	h, _ := os.UserHomeDir()
	return filepath.Join(h, "Library", "Application Support", "iTerm2", "DynamicProfiles", "roost.json")
}

func itermPlist() string {
	h, _ := os.UserHomeDir()
	return filepath.Join(h, "Library", "Preferences", "com.googlecode.iterm2.plist")
}

func plutilExtract(key, format string) (string, error) {
	out, err := exec.Command("plutil", "-extract", key, format, "-o", "-", itermPlist()).Output()
	return strings.TrimSpace(string(out)), err
}

// defaultProfile returns the name and key mappings of your default iTerm
// profile, so the roost profile can inherit them.
func defaultProfile() (name string, keymap map[string]any) {
	keymap = map[string]any{}
	guid, err := exec.Command("defaults", "read", "com.googlecode.iterm2", "Default Bookmark Guid").Output()
	if err != nil {
		return "", keymap
	}
	want := strings.TrimSpace(string(guid))
	for i := 0; i < 200; i++ {
		g, err := plutilExtract("New Bookmarks."+strconv.Itoa(i)+".Guid", "raw")
		if err != nil {
			break
		}
		if g != want {
			continue
		}
		name, _ = plutilExtract("New Bookmarks."+strconv.Itoa(i)+".Name", "raw")
		if km, err := plutilExtract("New Bookmarks."+strconv.Itoa(i)+".Keyboard Map", "json"); err == nil {
			json.Unmarshal([]byte(km), &keymap)
		}
		break
	}
	return name, keymap
}

// setupITerm writes an iTerm "Dynamic Profile" named roost: it is your
// default profile plus Cmd+Left/Right for switching roost tabs, and it runs
// roost when a window opens. Your other profiles are untouched.
func setupITerm(remove bool, shortcut string) error {
	path := itermProfilePath()
	if remove {
		if err := os.Remove(path); err == nil {
			fmt.Println("Removed the roost iTerm profile.")
		}
		return nil
	}
	if runtime.GOOS != "darwin" {
		return nil // iTerm is macOS-only
	}
	if _, err := os.Stat("/Applications/iTerm.app"); err != nil {
		return nil // no iTerm, nothing to do
	}
	parent, keymap := defaultProfile()
	key := func(seq string) map[string]any {
		return map[string]any{"Version": 2, "Apply Mode": 0, "Action": 10, "Text": seq, "Escaping": 2}
	}
	// keycode-modifiers: 0xf702/0xf703 = arrow, 0x300000 = Cmd (+arrow flag).
	keymap["0xf702-0x300000-0x7b-0x1"] = key(seqPrevTab)
	keymap["0xf703-0x300000-0x7c-0x1"] = key(seqNextTab)

	profile := map[string]any{
		"Name":         "roost",
		"Guid":         itermProfileGUID,
		"Initial Text": shellQuote(selfPath()),
		"Keyboard Map": keymap,
	}
	// Ctrl+Cmd+<letter> opens a roost window: --shortcut wins, then whatever
	// the profile had before (so re-running setup keeps it), then R.
	switch sc := strings.ToUpper(strings.TrimSpace(shortcut)); {
	case sc == "NONE":
		profile["Shortcut"] = "" // empty (not missing) so re-running setup keeps "none"
	case len(sc) == 1 && sc[0] >= 'A' && sc[0] <= 'Z':
		profile["Shortcut"] = sc
	case sc != "":
		return fmt.Errorf("--shortcut must be a single letter A-Z or 'none'")
	default:
		if old, ok := existingProfileValue(path, "Shortcut"); ok {
			profile["Shortcut"] = old
		} else {
			profile["Shortcut"] = "R"
		}
	}
	icon := filepath.Join(homeDir(), "roost.png")
	if os.MkdirAll(homeDir(), 0o755) == nil && os.WriteFile(icon, iconPNG, 0o644) == nil {
		profile["Icon"] = 2 // custom icon instead of tmux's "T"
		profile["Custom Icon Path"] = icon
	}
	if parent != "" {
		profile["Dynamic Profile Parent Name"] = parent
	}
	// Never inherit the parent's dedicated-window hotkey (that would open two
	// windows on one keypress), but keep a hotkey you've given roost itself.
	profile["Has Hotkey"] = false
	for k, v := range existingHotkey(path) {
		profile[k] = v
	}
	b, _ := json.MarshalIndent(map[string]any{"Profiles": []any{profile}}, "", "  ")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return err
	}
	fmt.Println("Added an iTerm profile named \"roost\" (Cmd+←/→ switches roost tabs).")
	if sc, ok := profile["Shortcut"].(string); ok && sc != "" {
		fmt.Printf("  Ctrl+Cmd+%s opens a roost window (change with: roost setup --shortcut X | none).\n", sc)
	}
	fmt.Println("  Open it from iTerm's Profiles menu, or make it your default profile.")
	// iTerm's global key bindings beat profile ones, and its defaults use
	// Cmd+arrows for tab switching — which would swallow our keys.
	for _, k := range []string{"0xf702-0x300000-0x7b-0x1", "0xf703-0x300000-0x7c-0x1"} {
		if _, err := plutilExtract("GlobalKeyMap."+k, "json"); err == nil {
			fmt.Println("\n  ! iTerm's global key bindings use Cmd+←/→ for tab switching, which wins")
			fmt.Println("    over the roost profile. Remove those two rows in iTerm → Settings →")
			fmt.Println("    Keys → Key Bindings to let Cmd+←/→ reach roost.")
			break
		}
	}
	return nil
}

// existingHotkey returns the hotkey settings from a previously written roost
// profile, so re-running setup doesn't drop them.
func existingHotkey(path string) map[string]any {
	out := map[string]any{}
	b, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	var f struct {
		Profiles []map[string]any `json:"Profiles"`
	}
	if json.Unmarshal(b, &f) != nil || len(f.Profiles) == 0 {
		return out
	}
	for k, v := range f.Profiles[0] {
		if strings.HasPrefix(k, "HotKey") || k == "Has Hotkey" {
			out[k] = v
		}
	}
	return out
}

// existingProfileValue reads one key from a previously written roost profile.
func existingProfileValue(path, key string) (any, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var f struct {
		Profiles []map[string]any `json:"Profiles"`
	}
	if json.Unmarshal(b, &f) != nil || len(f.Profiles) == 0 {
		return nil, false
	}
	v, ok := f.Profiles[0][key]
	return v, ok
}
