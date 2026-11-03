# roost

A simple home for your AI coding CLI tabs that **comes back after a restart**.
Built for Claude Code; also runs pi, Codex, Devin, Grok, or any CLI you configure.

Open a few tabs in different projects, restart your computer, type `roost`, and
every tab reopens in the right folder, back in the conversation it had before
(`claude --resume <id>`, `pi --session-id <id>`, …).

```
 🐓 roost v0.3.1  1 api │ ● 2 web │ $ 3 notes                + new  ? help
```

A dot on a tab means:

- 🔴 **red**: Claude needs you (permission/question)
- 🟡 **yellow**: working
- 🟢 **green**: finished while you were elsewhere

Colours are configurable, see [Theme colours](#theme-colours). A `$` marks tabs
that aren't running an AI CLI (see [Tab markers](#tab-markers)).

> roost is an unofficial community tool. It isn't made by or affiliated with
> Anthropic, OpenAI, Cognition, xAI or the pi authors.

## Install

Runs on macOS and Linux. Needs `tmux` 3.3 or newer and `jq` (on macOS the
installer offers to `brew install` them; on Linux use your package manager,
e.g. `sudo apt install tmux jq`).

```sh
curl -fsSL https://raw.githubusercontent.com/thewahlstedts/roost/master/install.sh | sh
roost setup       # asks which CLI you use; Claude hook + iTerm profile
```

Or from a clone:

```sh
git clone https://github.com/thewahlstedts/roost.git && cd roost
./install.sh      # checks deps, builds, installs to ~/.local/bin/roost
```

The installer downloads the latest release for your OS and CPU and checks its
checksum. It's safe to re-run (that's also how you upgrade), never uses sudo, and replaces the binary rather
than overwriting it. Set `ROOST_PREFIX` to install somewhere else. `make
install` works too.

### Updates

roost keeps itself up to date. When you open it, it checks GitHub at most
once a day in the background and installs a newer release, then tells you in
the status bar. Your tabs keep running; open roost again to load everything
new. `roost update` updates right away, and `roost update --check` only says
whether there's a newer version. If a new version has something new to set
up, roost says so: run `roost setup` and it asks only about that.

The first time you open roost after an update, a popup shows what's new since
the version you had (from [CHANGELOG.md](CHANGELOG.md)); `roost update` prints
it too. `roost changes` shows this version's notes again, `roost changes
--all` every release.

```
set -g @roost_auto_update on       # on (default) | notify (just tell me) | off
```

Every release's `checksums.txt` is signed with roost's own ed25519 key, and
the public key is built into roost. An update is only installed if that
signature checks out, the download matches its checksum, and the new binary
runs and reports the expected version. So someone who got into the GitHub
repo still couldn't push an update to you. Builds you made yourself (`make
install`, `go build`) report version `dev` and never auto-update; `roost
update --force` replaces one with the latest release. `ROOST_NO_UPDATE=1`
turns the check off for a shell.

### What setup changes

`roost setup` asks what isn't set up yet: which AI CLI you use (it suggests
the first one it finds installed) and where your code lives (it suggests the
folders where it finds git repos). Re-running it only asks about anything
new, so run it after updating; `roost setup --all` asks everything again, and
each answer has a flag: `--agent NAME`, `--source-dirs "~/Source ~/work"`,
`--source-depth N`. It also:

- saves your answers in `~/.roost/settings.conf` (anything you set in
  `custom.conf` instead counts as answered, and wins).
- if Claude Code is installed, adds one hook command (`roost hook`) to
  `~/.claude/settings.json`, after backing the file up next to it. It's needed
  for exact Claude resume and the status dots, and does nothing outside roost.
- on macOS with iTerm, writes an iTerm profile to
  `~/Library/Application Support/iTerm2/DynamicProfiles/roost.json`.

`roost setup --remove` undoes both. To uninstall completely, also delete
`~/.local/bin/roost` and `~/.roost`.

### Tested with

macOS 27, iTerm 3.7, tmux 3.8, zsh, Claude Code 2.1. On Linux: Ubuntu 24.04
(tmux 3.4) and Debian 12 (tmux 3.3a). Ubuntu 22.04 ships tmux 3.2, which is
too old. roost relies on a few
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
new tab, double-click a tab to rename it. Click **? help** for a cheat sheet.
While it's open, `n` and `t` open tabs directly.

The **+ new** picker lists the current folder, then folders you've used
recently (with any CLI, newest first), then every git repo under your source
folders. Each row shows what's been used there (`claude · factory`, `shell`…).
Type to filter, or type a path. **Tab** switches what the tab opens with: your
CLIs, editors such as `fresh`, or **shell** for a plain tab in that folder.

```
set -g @roost_source_dirs "~/Source ~/work"   # where to look for repos
set -g @roost_source_depth 4                  # levels below them (1-10)
# default folders: whichever of ~/Source ~/src ~/code ~/Projects ~/dev
#                  ~/Developer ~/workspace ~/repos ~/git ~/go/src exist
```

`roost setup` asks for these and suggests both from what it finds.

Scanning big folders can take a few seconds, so the scan runs in the
background and is never cut short: the picker opens instantly with the last
scan's results, the footer shows `scanning repos… N so far` while a fresh scan
runs, and new repos appear as they're found (closing the picker doesn't stop
it). `roost folders` runs the scan now and reports what it searched, how many
repos it found and how long it took; `roost folders --list` lists them.

**Move tabs** by dragging one and dropping it where you want it (on another
tab, past the last tab, or on the 🐓 label for first), with **Cmd+Shift+← / →** (iTerm
profile), or by right-clicking a tab for a menu (move left/right, to start/end,
rename, close). The order is kept across restarts.

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
| `<` `>` | move tab left / right | | |

## Other CLIs

`roost setup` asks which CLI new tabs run; change it any time with
`roost setup --agent pi`. Or set it, and which CLIs the picker offers, in
`~/.roost/custom.conf` (which wins over setup's choice):

```
set -g @roost_default_agent pi        # claude (default) | pi | codex | devin | grok | your own
set -g @roost_agents "claude pi"      # what Tab cycles through in the picker
                                      # (default: your default + installed or
                                      #  previously used presets; shell is always last)
```

| CLI | New tab | After a restart | Status dots |
|---|---|---|---|
| `claude` | `claude --session-id <id>` | exact conversation (`--resume <id>`) | yes (hook) |
| `pi` | `pi --session-id <id>` | exact conversation | no |
| `codex` | `codex` | most recent session (`codex resume --last`) | no |
| `devin` | `devin` | most recent session in the tab's folder (`devin -c`) | no |
| `grok` | `grok` | starts fresh | no |
| `factory` | `factory` | runs `factory` again, which continues the run or planning | no |
| `fresh` (editor) | `fresh .` | reopens `fresh .` in the folder | no |

Presets you have installed (including in `~/.cargo/bin`, `~/go/bin`,
`~/.local/bin`…) or have used before show up automatically: in `roost setup`'s
list (AI CLIs only) and when you press **Tab** in the **+ new** picker. `factory` is an agent pipeline
(plan → develop → test → review → merge) rather than a chat; run one per repo,
since it keeps its state in the repo's `.factory/` folder. roost's hook
ignores the non-interactive `claude -p` runs such pipelines make.

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

## Linux

Everything except the iTerm profile works the same in any terminal. Copying a
mouse selection uses `wl-copy` (Wayland) or `xclip`/`xsel` (X11) if installed,
otherwise tmux sends it to your terminal's clipboard (OSC 52), which most
terminals and SSH sessions support. `Ctrl-Space ←/→` switches tabs and
`Ctrl-Space <`/`>` moves them; for one-key shortcuts, bind your terminal to
send `\e[9001~` / `\e[9002~` (switch) and `\e[9003~` / `\e[9004~` (move).

## iTerm (macOS)

`roost setup` adds an iTerm profile named **roost**: your default profile plus
**Cmd+← / Cmd+→ to switch roost tabs** and **Cmd+Shift+← / → to move them**, a 🐓 icon, and it starts roost when
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
get `Opt-1..9`, `Opt-←/→`, `Opt-Shift-←/→` (move tab), and `Opt-n` with no
prefix.

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

### Checks (tests and lint after each turn)

Optional: when a Claude tab finishes a turn, roost can run the project's
check command in the background and mark the tab: **✓** passed, **✗**
failed, **◌** running, **?** needs your approval. When Claude starts working
again the old result is cleared (and a running check stopped). `Ctrl-Space c`
or right-click → *Check output* shows the last run.

```
set -g @roost_checks on            # off by default
set -g @roost_check_timeout 600    # seconds
```

The command comes from a `.roost-check` file in the repo (first non-comment
line, e.g. `make lint test`), or is detected: `make test`, `go test ./...`,
`cargo test`, `npm test`, `pytest`.

Nothing runs until you've approved that project's command, because a cloned
repo shouldn't be able to make roost execute things: run `roost check` in the
project (it shows the command and asks), or `roost check --allow`. If the
command changes, it needs approving again; `roost check --deny` revokes.
`roost check` also runs it on demand in any tab.

### Theme colours

Any tmux colour works: a name (`green`, `brightyellow`), `colourN`, or
`#rrggbb`. Names follow your terminal theme.

```
set -g @roost_accent "#e0473a"          # 🐓 roost label, folder picker, active split
set -g @roost_version_color colour242   # version next to the label
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

Releases: add user-facing notes under `## Unreleased` in `CHANGELOG.md` as
changes land. To release, rename that heading to the version (`## v0.5.0 —
2026-11-04`), merge, then push a `v*` tag (e.g. `git tag -s v0.5.0 && git push
origin v0.5.0`). The version comes from the tag, and the release workflow
refuses a tag with no changelog section (it becomes the release page's notes). The release workflow waits for a
maintainer to approve the `release` environment (which holds the signing key),
then builds macOS and Linux binaries (arm64 + amd64), signs `checksums.txt`
with `tools/sign`, and publishes them. `install.sh` and `roost update` use
them automatically.

`make install` replaces the binary (it never overwrites in place: macOS kills
signed binaries that are overwritten while cached). `ROOST_SOCKET` and
`ROOST_HOME` point roost at a separate tmux server and state folder for testing.

## License

[MIT](LICENSE)
