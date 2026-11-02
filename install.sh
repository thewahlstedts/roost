#!/bin/sh
# roost installer. Safe to re-run.
#
# From a clone:     ./install.sh
# Via pipe:         curl -fsSL https://raw.githubusercontent.com/thewahlstedts/roost/master/install.sh | sh
#
# Environment overrides:
#   ROOST_PREFIX (or PREFIX)  install dir            (default: ~/.local/bin)
#   ROOST_VERSION             release tag, e.g. v0.2.0 (default: latest)
#   ROOST_FROM_SOURCE=1       skip release binaries, always build with Go
#   ROOST_NO_SETUP=1          never offer to run `roost setup`
#
# Works on macOS and Linux. Never uses sudo. Never overwrites the binary in
# place (macOS kills signed binaries whose file is rewritten); it removes the
# old one and moves the new one in.
set -eu

REPO="thewahlstedts/roost"
PREFIX="${ROOST_PREFIX:-${PREFIX:-$HOME/.local/bin}}"
VERSION="${ROOST_VERSION:-latest}"
FROM_SOURCE="${ROOST_FROM_SOURCE:-0}"
NO_SETUP="${ROOST_NO_SETUP:-0}"

tmp=""
cleanup() { if [ -n "$tmp" ] && [ -d "$tmp" ]; then rm -rf "$tmp"; fi; }
trap cleanup EXIT
trap 'cleanup; exit 130' INT TERM

if [ -t 2 ]; then B='\033[1m' Y='\033[33m' R='\033[31m' N='\033[0m'; else B='' Y='' R='' N=''; fi
say()  { printf "${B}==>${N} %s\n" "$*" >&2; }
warn() { printf "${Y}warning:${N} %s\n" "$*" >&2; }
die()  { printf "${R}error:${N} %s\n" "$*" >&2; exit 1; }
have() { command -v "$1" >/dev/null 2>&1; }

# Interactive only when both ends are a terminal. Under `curl | sh` stdin is
# the script itself, so we never prompt there: we just print what to do.
interactive() { [ -t 0 ] && [ -t 1 ]; }

ask() { # ask "question" -> 0 for yes
	interactive || return 1
	printf '%s [y/N] ' "$1"
	read -r reply || return 1
	case "$reply" in [yY] | [yY][eE][sS]) return 0 ;; *) return 1 ;; esac
}

check_platform() {
	case "$(uname -s)" in
	Darwin) OS=darwin ;;
	Linux) OS=linux ;;
	*) die "roost runs on macOS and Linux (found $(uname -s))." ;;
	esac
	case "$(uname -m)" in
	arm64 | aarch64) ARCH=arm64 ;;
	x86_64) ARCH=amd64 ;;
	*) die "unsupported architecture: $(uname -m)" ;;
	esac
}

check_runtime_deps() {
	missing=""
	for dep in tmux jq; do
		have "$dep" || missing="$missing $dep"
	done
	missing="${missing# }"
	if [ -n "$missing" ]; then
		if have brew && ask "roost needs: $missing. Install with Homebrew now?"; then
			# shellcheck disable=SC2086 # word-splitting the list is intended
			brew install $missing
		elif [ "$OS" = linux ]; then
			warn "roost needs $missing at runtime. Install it with your package manager, e.g. sudo apt install $missing"
		else
			warn "roost needs $missing at runtime. Install with: brew install $missing"
		fi
	fi
	if [ "$OS" = darwin ] && [ ! -d "/Applications/iTerm.app" ] && [ ! -d "$HOME/Applications/iTerm.app" ]; then
		warn "iTerm not found. roost works without it, but the iTerm profile and tab markers need it (brew install --cask iterm2)."
	fi
	have claude || warn "Claude Code ('claude') not found on PATH. See https://docs.claude.com/en/docs/claude-code"
}

# Directory holding this script, if it's a roost checkout (not when piped).
local_checkout() {
	case "$0" in
	*/install.sh | install.sh) ;;
	*) return 1 ;;
	esac
	dir=$(cd "$(dirname "$0")" 2>/dev/null && pwd) || return 1
	[ -f "$dir/go.mod" ] && grep -q "^module github.com/$REPO\$" "$dir/go.mod" || return 1
	printf '%s\n' "$dir"
}

# Try a prebuilt release binary. Writes $tmp/roost and returns 0, or returns 1
# (after saying why) so the caller falls back to building from source.
# Plain HTTPS first (the repo is public); gh as a backup.
fetch_release() {
	asset="roost_${OS}_${ARCH}.tar.gz"
	dl="$tmp/release"
	mkdir -p "$dl"
	errlog="$tmp/download.err"

	if download_curl || download_gh; then
		:
	else
		warn "Couldn't download a release binary for ${OS}/${ARCH}:"
		sed 's/^/    /' "$errlog" >&2
		return 1
	fi

	want=$(awk -v f="$asset" '$2 == f || $2 == "*" f { print $1 }' "$dl/checksums.txt")
	[ -n "$want" ] || die "no checksum for $asset in checksums.txt"
	if have sha256sum; then
		got=$(sha256sum "$dl/$asset" | awk '{ print $1 }')
	else
		got=$(shasum -a 256 "$dl/$asset" | awk '{ print $1 }')
	fi
	[ "$want" = "$got" ] || die "checksum mismatch for $asset (want $want, got $got). Not installing."
	say "Checksum OK ($asset)"

	tar -xzf "$dl/$asset" -C "$dl" roost || die "could not extract roost from $asset"
	mv "$dl/roost" "$tmp/roost"
	return 0
}

download_curl() {
	have curl || { echo "curl: not installed" >>"$errlog"; return 1; }
	if [ "$VERSION" = "latest" ]; then
		base="https://github.com/$REPO/releases/latest/download"
	else
		base="https://github.com/$REPO/releases/download/$VERSION"
	fi
	for f in "$asset" checksums.txt; do
		if ! curl -fsSL --retry 2 -o "$dl/$f" "$base/$f" 2>>"$errlog"; then
			echo "curl: could not fetch $base/$f" >>"$errlog"
			return 1
		fi
	done
}

download_gh() {
	have gh || return 1
	gh auth status >/dev/null 2>&1 || { echo "gh: not logged in" >>"$errlog"; return 1; }
	tag="$VERSION"
	if [ "$tag" = "latest" ]; then # older gh can't download "latest" without a tag
		tag=$(gh api "repos/$REPO/releases/latest" --jq .tag_name 2>>"$errlog") || return 1
	fi
	rm -f "$dl/$asset" "$dl/checksums.txt"
	gh release download "$tag" --repo "$REPO" --dir "$dl" \
		--pattern "$asset" --pattern checksums.txt 2>>"$errlog" >/dev/null
}

get_source() { # prints the source dir
	if src=$(local_checkout); then
		printf '%s\n' "$src"
		return 0
	fi
	src="$tmp/src"
	say "Fetching source..."
	if GIT_TERMINAL_PROMPT=0 git clone --quiet --depth 1 "https://github.com/$REPO.git" "$src" >/dev/null 2>&1; then
		printf '%s\n' "$src"
		return 0
	fi
	rm -rf "$src"
	if have gh && gh auth status >/dev/null 2>&1 &&
		gh repo clone "$REPO" "$src" -- --quiet --depth 1 >/dev/null 2>&1; then
		printf '%s\n' "$src"
		return 0
	fi
	die "could not clone $REPO. Check your network, or try:
    git clone https://github.com/$REPO.git && cd roost && ./install.sh"
}

build_from_source() {
	have go || die "Go is needed to build roost from source: https://go.dev/dl (or brew install go)"
	src=$(get_source)
	say "Building from $src"
	(cd "$src" && CGO_ENABLED=0 go build -trimpath -o "$tmp/roost" .) || die "go build failed"
}

install_binary() {
	dest="$PREFIX/roost"
	mkdir -p "$PREFIX"
	# Stage next to the target so the final step is a same-filesystem rename,
	# then remove the old binary first: never rewrite it in place.
	staged="$PREFIX/.roost.new.$$"
	rm -f "$staged"
	cp "$tmp/roost" "$staged"
	chmod 755 "$staged"
	if [ "$OS" = darwin ]; then xattr -d com.apple.quarantine "$staged" 2>/dev/null || true; fi
	rm -f "$dest"
	mv "$staged" "$dest"
	say "Installed $("$dest" version 2>/dev/null || echo roost) to $dest"
}

check_path() {
	found=$(command -v roost 2>/dev/null || true)
	if [ -n "$found" ] && [ "$found" != "$PREFIX/roost" ]; then
		warn "'roost' on your PATH is $found ($("$found" version 2>/dev/null || echo "unknown version")), not the one just installed at $PREFIX/roost. Remove the other copy or put $PREFIX first on your PATH."
	fi
	case ":$PATH:" in
	*":$PREFIX:"*) ;;
	*)
		warn "$PREFIX is not on your PATH. Add this to your shell's rc file (~/.zshrc, ~/.bashrc):"
		# shellcheck disable=SC2016 # print a literal $PATH for the user to paste
		printf '    export PATH="%s:$PATH"\n' "$PREFIX" >&2
		;;
	esac
}

offer_setup() {
	if [ "$NO_SETUP" != "1" ] && ask "Run 'roost setup' now (choose your CLI; adds a Claude Code hook, and an iTerm profile on macOS)?"; then
		"$PREFIX/roost" setup
	else
		say "Next: run 'roost setup' (asks which CLI you use; adds a hook to ~/.claude/settings.json, plus an iTerm profile on macOS; undo with 'roost setup --remove')."
	fi
}

main() {
	check_platform
	check_runtime_deps
	tmpbase="${TMPDIR:-/tmp}"
	tmp=$(mktemp -d "${tmpbase%/}/roost-install.XXXXXX")

	if [ "$FROM_SOURCE" != "1" ] && ! local_checkout >/dev/null && fetch_release; then
		:
	else
		[ "$FROM_SOURCE" = "1" ] || local_checkout >/dev/null ||
			warn "Building from source instead. Source builds report version 'dev' and don't auto-update; re-run this installer once the download works to get the release."
		build_from_source
	fi

	install_binary
	check_path
	offer_setup
}

# Everything runs from here, so a truncated `curl | sh` download does nothing.
main "$@"
