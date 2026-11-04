# Changelog

What's new in each roost release, written for people using roost. roost
shows the entries since your previous version the first time you open it
after an update (`roost changes` shows them again).

Add notes under "Unreleased" as changes land. To release, rename that heading
to the new version (e.g. `## v0.5.0 — 2026-11-04`) and tag it; the release
workflow refuses to publish a version that has no section here.

## v0.7.0 — 2026-11-04

- Tabs are named after the git repo they're in, not the folder. Go into a
  subfolder and the tab still says `cluster-tooling`, with the subfolder after
  it, dimmed: `cluster-tooling › bootstrap`. Tabs you renamed show the
  subfolder too. Turn it off with `set -g @roost_subdir off`.

## v0.6.0 — 2026-11-03

- The **+ new** picker no longer lists every repo twice on Macs where both
  `~/Source` and `~/source` match the same folder (macOS ignores case in
  folder names).
- The **+ new** picker shows which CLI the new tab will run: an "open with"
  line lists every CLI, with the one Enter will run highlighted. Press Tab
  (Shift-Tab to go back) to switch.
- `roost --version` (or `-v`) prints the version instead of opening roost.
- The **+ new** picker finds your repos when your code folder is itself a git
  repo (say, one holding AI instructions for everything under it). roost used
  to stop at that top repo and list only it.

## v0.5.1 — 2026-11-03

- The **+ new** picker no longer misses repos in big or slow code folders.
  The repo scan used to stop after 0.4 seconds; now it runs in the background
  until it's done, the picker opens instantly from the last scan, and the
  footer shows `scanning repos… N so far` while it works.
- `roost folders` scans your code folders now and reports what it found.

## v0.5.0 — 2026-11-03

- `roost setup` now only asks about things you haven't set up yet, so run it
  after an update and it asks just what's new. `roost setup --all` asks
  everything again.
- New setup question: where your code lives. roost suggests the folders where
  it finds git repos, and how deep to look.
- How deep the **+ new** picker looks for repos is configurable:
  `set -g @roost_source_depth N` (default 4).
- After an update, roost says when there's something new to set up.
- This list: the first time you open roost after an update, it shows what's
  new since your previous version. `roost changes` shows it any time.

## v0.4.2 — 2026-11-03

- The **+ new** picker lists every git repo under your code folders (e.g.
  `~/Source`), not just folders Claude has been used in, and shows what's been
  used in each (`claude · factory`, `shell`…).
- **Tab** in the picker cycles your CLIs, editors such as `fresh`, and
  **shell** for a plain tab in that folder.
- New `fresh` editor preset. CLIs installed in `~/.cargo/bin`, `~/go/bin` and
  similar are found even when they aren't on roost's PATH.

## v0.4.1 — 2026-11-03

- `-h` / `--help` always just shows help. Before, `roost kill --help` closed
  every tab.

## v0.4.0 — 2026-11-03

- Checks: with `set -g @roost_checks on`, roost runs the project's tests or
  lint after each Claude turn and marks the tab ✓ / ✗ / ◌. `Ctrl-Space c`
  shows the output. Each project's command must be approved once.
- `factory` (agent pipeline) is a built-in preset.
- roost ignores non-interactive `claude -p` runs, so pipelines in a tab don't
  take it over.

## v0.3.3 — 2026-11-03

- Dragging a tab and dropping it on another tab, past the end, or on the 🐓
  label now moves it there.

## v0.3.2 — 2026-11-02

- The status bar shows roost's version next to the label.

## v0.3.1 — 2026-11-02

- roost updates itself: it checks once a day and installs signed releases.
  `roost update` updates now.
- `roost setup` asks which CLI you use.
- Move tabs: drag them, Cmd+Shift+← / →, or right-click a tab.
- Devin preset.

## v0.2.0 — 2026-11-02

- Linux support. Requires tmux 3.3 or newer.

## v0.1.0 — 2026-11-02

- First release: tabs that come back after a restart, in the right folder and
  conversation.
