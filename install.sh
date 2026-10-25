#!/bin/sh
# Install codex-tether to ~/.local/bin and point the Codex desktop app at it
# through CODEX_CLI_PATH: launchctl and a login LaunchAgent on macOS,
# environment.d on Linux.
#
#   curl -fsSL https://raw.githubusercontent.com/gcol33/codex-tether/main/install.sh | sh
#       install the latest release
#   ./install.sh               from a checkout: use ./codex-tether, or build it
#                              with Go, or download the release
#   ./install.sh --uninstall   remove CODEX_CLI_PATH and the installed binary
#
# Restart the Codex app afterwards.
set -eu

repo=gcol33/codex-tether
bin_dir="$HOME/.local/bin"
exe="$bin_dir/codex-tether"
label=com.github.gcol33.codex-tether
plist="$HOME/Library/LaunchAgents/$label.plist"
envd="${XDG_CONFIG_HOME:-$HOME/.config}/environment.d/codex-tether.conf"

case "$(uname -s)" in
    Darwin) os=macos; goos=darwin ;;
    Linux)  os=linux; goos=linux ;;
    *) echo "install.sh: unsupported system $(uname -s)" >&2; exit 1 ;;
esac

if [ "${1:-}" = "--uninstall" ]; then
    if [ "$os" = macos ]; then
        launchctl unsetenv CODEX_CLI_PATH
        rm -f "$plist"
    else
        rm -f "$envd"
        if command -v systemctl >/dev/null 2>&1; then
            systemctl --user unset-environment CODEX_CLI_PATH 2>/dev/null || true
        fi
    fi
    rm -f "$exe"
    if [ "$os" = macos ]; then
        echo "codex-tether removed. Quit the Codex app and start it again."
    else
        echo "codex-tether removed. Sign out and back in before starting the Codex app again."
    fi
    exit 0
fi

fetch() {
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL "$1" -o "$2"
    elif command -v wget >/dev/null 2>&1; then
        wget -q "$1" -O "$2"
    else
        echo "install.sh: curl or wget is needed to download codex-tether" >&2
        exit 1
    fi
}

sha256() {
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$1" | cut -d' ' -f1
    elif command -v shasum >/dev/null 2>&1; then
        shasum -a 256 "$1" | cut -d' ' -f1
    else
        echo "install.sh: sha256sum or shasum is needed to check the download" >&2
        rm -f "$1"
        exit 1
    fi
}

# Download the latest release binary for this machine into $1 and check it
# against the release's SHA256SUMS.
download() {
    case "$(uname -m)" in
        x86_64|amd64) goarch=amd64 ;;
        arm64|aarch64) goarch=arm64 ;;
        *) echo "install.sh: unsupported processor $(uname -m)" >&2; exit 1 ;;
    esac
    name="codex-tether-$goos-$goarch"
    base="https://github.com/$repo/releases/latest/download"
    fetch "$base/$name" "$1"
    fetch "$base/SHA256SUMS" "$1.sums"
    want=$(awk -v n="$name" '$2 == n || $2 == "*" n { print $1 }' "$1.sums")
    rm -f "$1.sums"
    if [ -z "$want" ]; then
        echo "install.sh: SHA256SUMS has no entry for $name" >&2
        rm -f "$1"
        exit 1
    fi
    got=$(sha256 "$1")
    if [ "$got" != "$want" ]; then
        echo "install.sh: checksum mismatch for $name" >&2
        rm -f "$1"
        exit 1
    fi
}

mkdir -p "$bin_dir"
# Piped into sh, $0 names the shell rather than this file, and the release is
# downloaded; run as a file next to the sources, the checkout is used.
here=
case "$0" in
    */install.sh|install.sh) here=$(cd "$(dirname "$0")" && pwd) ;;
esac
if [ -n "$here" ] && [ -f "$here/go.mod" ]; then
    src="$here/codex-tether"
    if [ ! -f "$src" ]; then
        if command -v go >/dev/null 2>&1; then
            (cd "$here" && go build -trimpath -ldflags '-s -w' -o codex-tether .)
        else
            src=
        fi
    fi
else
    src=
fi

# Copy then rename, so a codex-tether that is running keeps its old file.
if [ -n "$src" ]; then
    cp "$src" "$exe.new"
else
    download "$exe.new"
fi
chmod 755 "$exe.new"
mv -f "$exe.new" "$exe"

if [ "$os" = macos ]; then
    launchctl setenv CODEX_CLI_PATH "$exe"
    mkdir -p "$(dirname "$plist")"
    exe_xml=$(printf '%s' "$exe" | sed 's/&/\&amp;/g; s/</\&lt;/g; s/>/\&gt;/g')
    cat >"$plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>$label</string>
    <key>ProgramArguments</key>
    <array>
        <string>/bin/launchctl</string>
        <string>setenv</string>
        <string>CODEX_CLI_PATH</string>
        <string>$exe_xml</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
</dict>
</plist>
EOF
else
    mkdir -p "$(dirname "$envd")"
    printf 'CODEX_CLI_PATH=%s\n' "$exe" >"$envd"
    if command -v systemctl >/dev/null 2>&1; then
        systemctl --user set-environment CODEX_CLI_PATH="$exe" 2>/dev/null || true
    fi
fi

echo "Installed $exe and set CODEX_CLI_PATH."
case ":$PATH:" in
    *":$bin_dir:"*) ;;
    *) echo "Add $bin_dir to your PATH to run codex-tether by name, for example: export PATH=\"\$HOME/.local/bin:\$PATH\"" ;;
esac
echo "Quit the Codex app and start it again."
