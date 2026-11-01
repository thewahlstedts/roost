# roost

A simple home for your Claude Code tabs that **comes back after a restart**.

Open a few Claude tabs in different projects, restart your Mac, type `roost`,
and every tab reopens in the right folder with `claude --resume` on the exact
conversation it had before.

```
 🐓 roost  1 api │ ● 2 web │ 3 notes                         + new  ? help
```

A dot on a tab means: **red** Claude needs you (permission/question),
**yellow** working, **green** finished while you were elsewhere.

## Install

```sh
make install      # builds and copies to ~/.local/bin/roost
roost setup       # adds a small hook to ~/.claude/settings.json (backed up first)
```

Needs macOS, `tmux` and `jq` (both via Homebrew), and Go to build. `roost
setup` also creates an iTerm profile (see below); it's safe to re-run, and
`roost setup --remove` undoes everything.

## Use

```sh
roost             # open (restores everything after a restart)
roost new ~/code/api      # new Claude tab in a folder
roost ls          # what's open / what was saved
roost kill        # save + close everything
```

Inside roost, use the mouse: click tabs, click **+ new** to pick a folder for a
new Claude tab (recent Claude projects first; type to filter), double-click a
tab to rename it. Click **? help** for a cheat sheet — while it's open, `n` and
`t` open tabs directly.

Tabs are named after the folder they're in (for Claude tabs, the folder Claude
is working in) and update as you move around. Rename a tab and your name sticks,
including across restarts.

Other keys are `Ctrl-Space`, let go, then:

| key | does | key | does |
|---|---|---|---|
| `n` | new Claude tab | `t` | new shell tab |
| `1`–`9` | go to tab | `←` `→` | prev / next tab |
| `r` | rename | `x` | close tab |
| `\|` `-` | split | `d` | detach |
| `?` | help | `s` | save now |

## iTerm

`roost setup` adds an iTerm profile named **roost**: your default profile plus
**Cmd+← / Cmd+→ to switch roost tabs**, a 🐓 icon, and it starts roost when
opened. Open it with **Ctrl+Cmd+R** or the *Profiles* menu. Your other profiles
are untouched.

- iTerm's *global* key bindings beat profile ones. If Cmd+←/→ still switches
  iTerm tabs, remove those two rows in Settings → Keys → Key Bindings (setup
  warns you about this). Re-add them per-profile where you want them.
- To make a dedicated hotkey window open roost, give the roost profile the
  hotkey (Profiles → roost → Keys) and turn it off on your old hotkey profile.
- iTerm may offer "Looks like mouse reporting was left on when an ssh session
  ended…". Don't click Yes (roost needs the mouse). To stop it:
  `defaults write com.googlecode.iterm2 NoSyncTurnOffMouseReportingOnHostChange -bool false`

If you set iTerm → Profiles → Keys → *Left Option key* to **Esc+**, you also
get `Opt-1..9`, `Opt-←/→`, and `Opt-n` with no prefix.

Prefer iTerm's own tabs? `roost --native` uses iTerm's tmux integration.

## How it works

- roost runs a **private tmux server** (`tmux -L roost`) with its own config,
  so it never touches your normal tmux setup.
- It **saves your layout** (tabs, splits, folders) to `~/.roost/state.json`
  every few seconds while attached, and whenever tabs change. The last 50
  versions are kept in `~/.roost/history/` — `roost restore --list`.
- The **Claude hook** (`roost hook`) tags each tab with its Claude session id
  (it follows `/clear` and `/resume` too) and sets the status dots. It does
  nothing outside roost. A deliberate quit (`/exit`, Ctrl-D) forgets the
  session; a reboot or crash does not.
- On restore, tabs whose conversation exists on disk run
  `claude --resume <id>`. Plain shell tabs just reopen in their folder.

## Customize

- `~/.roost/custom.conf` is loaded last — any tmux settings/keys go there
  (e.g. `set -g prefix C-a`).
- `ROOST_CLAUDE_CMD` changes how Claude is launched
  (e.g. `export ROOST_CLAUDE_CMD="claude --model opus"`).

## Development

Run `make hooks` once after cloning: every commit then runs a secret scan,
gofmt, `go vet`, `staticcheck` and `govulncheck` (`brew install staticcheck
govulncheck`), and is blocked if any fail. Bypass once with `--no-verify`.
CI (GitHub Actions) runs the same checks plus a TruffleHog secret scan of the
full history on every push and pull request; Dependabot keeps the pinned
actions and Go modules up to date.

`make install` replaces the binary (it never overwrites in place — macOS kills
signed binaries that are overwritten while cached). `ROOST_SOCKET` and
`ROOST_HOME` point roost at a separate tmux server and state folder for testing.

Uninstall the hook and iTerm profile with `roost setup --remove`.
