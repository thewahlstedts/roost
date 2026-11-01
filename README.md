# roost

A simple home for your Claude Code tabs that **comes back after a restart**.

Open a few Claude tabs in different projects, restart your Mac, type `roost`,
and every tab reopens in the right folder with `claude --resume` on the exact
conversation it had before.

```
 roost   1 api  ● 2 web  3 notes                          + new  ^Space ? help
```

A dot on a tab means: **red** Claude needs you (permission/question),
**yellow** working, **green** finished while you were elsewhere.

## Install

```sh
make install      # builds and copies to ~/.local/bin/roost
roost setup       # adds a small hook to ~/.claude/settings.json (backed up first)
```

Needs `tmux` and `jq` (both via Homebrew) and Go to build.

## Use

```sh
roost             # open (restores everything after a restart)
roost new ~/code/api      # new Claude tab in a folder
roost ls          # what's open / what was saved
roost kill        # save + close everything
```

Inside roost, use the mouse: click tabs, click **+ new** to pick a folder for a
new Claude tab, double-click a tab to rename it. Keys are `Ctrl-Space`, then:

| key | does | key | does |
|---|---|---|---|
| `n` | new Claude tab | `t` | new shell tab |
| `1`–`9` | go to tab | `←` `→` | prev / next tab |
| `r` | rename | `x` | close tab |
| `\|` `-` | split | `d` | detach |
| `?` | help | `s` | save now |

**Cmd+← / Cmd+→ switch tabs**, just like iTerm. `roost setup` adds an iTerm
profile named **roost** (your default profile plus those two keys, and it
starts roost when opened). Open it from iTerm's *Profiles* menu, or make it
your default. Your other profiles keep normal iTerm tab switching.

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

Uninstall the hook and iTerm profile with `roost setup --remove`.
