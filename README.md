# roost

A simple home for your AI coding CLI tabs that **comes back after a restart**.
Built for Claude Code; also runs pi, Codex, Grok, or any CLI you configure.

Open a few tabs in different projects, restart your Mac, type `roost`, and
every tab reopens in the right folder, back in the conversation it had before
(`claude --resume <id>`, `pi --session-id <id>`, …).

```
 🐓 roost  1 api │ ● 2 web │ $ 3 notes                       + new  ? help
```

A dot on a tab means:

- 🔴 **red**: Claude needs you (permission/question)
- 🟡 **yellow**: working
- 🟢 **green**: finished while you were elsewhere

Colours are configurable, see [Theme colours](#theme-colours). A `$` marks tabs
that aren't running an AI CLI (see [Tab markers](#tab-markers)).

> roost is an unofficial community tool. It isn't made by or affiliated with
> Anthropic, OpenAI, xAI or the pi authors.

## Install

Needs macOS, [Homebrew](https://brew.sh) `tmux` and `jq`, Go (to build), and
the GitHub CLI logged in to an account with access to this repo.

```sh
gh repo clone thewahlstedts/roost && cd roost
./install.sh      # checks deps, builds, installs to ~/.local/bin/roost
roost setup       # Claude hook + iTerm profile (see "What setup changes")
```

Or in one line, without keeping a clone:

```sh
gh api -H 'Accept: application/vnd.github.raw' repos/thewahlstedts/roost/contents/install.sh | sh
```

The installer is safe to re-run (that's also how you upgrade). It offers to
`brew install` missing tmux/jq, never uses sudo, and replaces the binary rather
than overwriting it. Set `ROOST_PREFIX` to install somewhere else. `make
install` works too.

### What setup changes

`roost setup` is optional but needed for exact Claude resume and the status
dots. It:

- adds one hook command (`roost hook`) to `~/.claude/settings.json`, after
  backing the file up next to it. The hook does nothing outside roost.
- writes an iTerm profile to
  `~/Library/Application Support/iTerm2/DynamicProfiles/roost.json`.

`roost setup --remove` undoes both. To uninstall completely, also delete
`~/.local/bin/roost` and `~/.roost`.

### Tested with

macOS 27, iTerm 3.7, tmux 3.8, zsh, Claude Code 2.1. roost relies on a few
undocumented details (how Claude Code stores sessions, iTerm's profile format),
so other versions may need tweaks. Issues welcome.

## Use

```sh
roost                       # open (restores everything after a restart)
roost new ~/code/api        # new tab running your CLI (default: claude)
roost new ~/code/api --agent pi
roost new . --shell         # plain shell tab
roost ls                    # what's open / what was saved
roost kill                  # save + close everything
```

Inside roost, use the mouse: click tabs, click **+ new** to pick a folder for a
new tab (recent Claude projects first; type to filter; **Tab** switches which
CLI it opens), double-click a tab to rename it. Click **? help** for a cheat
sheet. While it's open, `n` and `t` open tabs directly.

Tabs are named after the folder they're in (for Claude tabs, the folder Claude
is working in) and update as you move around. Rename a tab and your name sticks,
including across restarts.

Other keys are `Ctrl-Space`, let go, then:

| key | does | key | does |
|---|---|---|---|
| `n` | new CLI tab | `t` | new shell tab |
| `1`–`9` | go to tab | `←` `→` | prev / next tab |
| `r` | rename | `x` | close tab |
| `\|` `-` | split | `d` | detach |
| `?` | help | `s` | save now |

## Other CLIs

Pick the CLI for new tabs in `~/.roost/custom.conf`:

```
set -g @roost_default_agent pi        # claude (default) | pi | codex | grok | your own
set -g @roost_agents "claude pi"      # what Tab cycles through in the picker
                                      # (default: claude + installed presets)
```

| CLI | New tab | After a restart | Status dots |
|---|---|---|---|
| `claude` | `claude --session-id <id>` | exact conversation (`--resume <id>`) | yes (hook) |
| `pi` | `pi --session-id <id>` | exact conversation | no |
| `codex` | `codex` | most recent session (`codex resume --last`) | no |
| `grok` | `grok` | starts fresh | no |

Change a preset or add any CLI. `{id}` is a UUID roost generates per tab, so
use it when the CLI can be pinned to a session:

```
set -g @roost_agent_claude_cmd "claude --model opus"   # program + flags you always want
set -g @roost_agent_mycli_cmd "mycli"                   # a new CLI called "mycli"
set -g @roost_agent_mycli_start ""                      # extra args for a new tab
set -g @roost_agent_mycli_resume "--continue"           # args after a restart ("" = start fresh)
```

Only Claude reports when you quit it (through its hook). For other CLIs roost
treats a tab that's back at a shell prompt as "CLI exited", so it reopens as a
plain shell after a restart.

## iTerm

`roost setup` adds an iTerm profile named **roost**: your default profile plus
**Cmd+← / Cmd+→ to switch roost tabs**, a 🐓 icon, and it starts roost when
opened. Open it with **Ctrl+Cmd+R** (change with `roost setup --shortcut K`, or
`--shortcut none`) or the *Profiles* menu. Your other profiles are untouched.

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
- It **saves your layout** (tabs, splits, folders, which CLI each tab runs) to
  `~/.roost/state.json` every few seconds while attached, and whenever tabs
  change. The last 50 versions are kept in `~/.roost/history/`:
  `roost restore --list`.
- The **Claude hook** (`roost hook`) tags each tab with its Claude session id
  (it follows `/clear` and `/resume` too) and sets the status dots. It does
  nothing outside roost. A deliberate quit (`/exit`, Ctrl-D) forgets the
  session; a reboot or crash does not.
- On restore, each tab reruns its CLI with its resume command. Plain shell tabs
  just reopen in their folder.

## Customize

- `~/.roost/custom.conf` is loaded last. Any tmux settings/keys go there
  (e.g. `set -g prefix C-a`). Run `roost setup` (or restart roost) to apply.
- Bar background and split borders are plain tmux options: `status-style`,
  `pane-border-style`.

### Theme colours

Any tmux colour works: a name (`green`, `brightyellow`), `colourN`, or
`#rrggbb`. Names follow your terminal theme.

```
set -g @roost_accent "#e0473a"          # 🐓 roost label, folder picker, active split
set -g @roost_waiting_color colour203   # dot: Claude needs you
set -g @roost_working_color colour221   # dot: working
set -g @roost_done_color colour114      # dot: finished while you were away
set -g @roost_new_color colour114       # "+ new" button
set -g @roost_tab_color colour246       # tab text
set -g @roost_current_tab_color colour255
set -g @roost_current_tab_bg colour238
```

### Splits

Split tabs (`Ctrl-Space |` / `-`) get heavy divider lines, the active split
outlined in the accent colour, and a label with each split's folder (and CLI)
while the tab is split.

```
set -g pane-border-lines heavy        # single | double | heavy | simple | number | spaces
set -g @roost_split_color colour244   # inactive dividers and labels
set -g @roost_split_labels on         # off hides the folder labels
```

The active split uses `@roost_accent`.

### Tab markers

Tabs not running an AI CLI (shells, servers, other programs) get a dim `$`
before their number; CLI tabs stay clean. ("claude" below means any CLI tab.)

```
set -g @roost_marker both          # shell (default) | claude | both | none
set -g @roost_shell_marker '$'
set -g @roost_claude_marker '✳'
set -g @roost_shell_marker_color green       # any tmux colour: green, colour114, #7ec699
set -g @roost_claude_marker_color colour242  # default for both is dim grey
```

Tip: plain names like `green` use your terminal theme's colours, so they match
other tools (e.g. a Claude status line that prints with `\033[32m`).

## Development

Run `make hooks` once after cloning: every commit then runs a secret scan,
gofmt, `go vet`, `staticcheck` and `govulncheck` (`brew install staticcheck
govulncheck`), and is blocked if any fail. Bypass once with `--no-verify`.
CI (GitHub Actions) runs the same checks, the tests, and a TruffleHog secret
scan of the full history on every push and pull request; Dependabot keeps the
pinned actions and Go modules up to date.

Releases: bump `version` in `main.go`, then push a `v*` tag. The release
workflow builds macOS binaries (arm64 + amd64) with checksums, and
`install.sh` uses them automatically so users don't need Go.

`make install` replaces the binary (it never overwrites in place: macOS kills
signed binaries that are overwritten while cached). `ROOST_SOCKET` and
`ROOST_HOME` point roost at a separate tmux server and state folder for testing.

## License

[MIT](LICENSE)
