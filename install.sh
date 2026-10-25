#!/bin/sh
# Install codex-tether to ~/.local/bin and point the Codex desktop app at it
# through CODEX_CLI_PATH: launchctl and a login LaunchAgent on macOS,
# environment.d on Linux.
#
#   ./install.sh               install (uses ./codex-tether, or builds it with Go)
#   ./install.sh --uninstall   remove CODEX_CLI_PATH and the installed binary
#
# Restart the Codex app afterwards.
set -eu

here=$(cd "$(dirname "$0")" && pwd)
bin_dir="$HOME/.local/bin"
exe="$bin_dir/codex-tether"
label=com.github.gcol33.codex-tether
plist="$HOME/Library/LaunchAgents/$label.plist"
envd="${XDG_CONFIG_HOME:-$HOME/.config}/environment.d/codex-tether.conf"

case "$(uname -s)" in
    Darwin) os=macos ;;
    Linux)  os=linux ;;
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
    echo "codex-tether removed. Quit the Codex app and start it again."
    exit 0
fi

src="$here/codex-tether"
if [ ! -f "$src" ]; then
    if ! command -v go >/dev/null 2>&1; then
        echo "install.sh: codex-tether not found next to install.sh and Go is not installed to build it." >&2
        exit 1
    fi
    (cd "$here" && go build -ldflags '-s -w' -o codex-tether .)
fi

mkdir -p "$bin_dir"
# Copy then rename, so a codex-tether that is running keeps its old file.
cp "$src" "$exe.new"
chmod 755 "$exe.new"
mv -f "$exe.new" "$exe"

if [ "$os" = macos ]; then
    launchctl setenv CODEX_CLI_PATH "$exe"
    mkdir -p "$(dirname "$plist")"
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
        <string>$exe</string>
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
    *) echo "Add $bin_dir to your PATH to run codex-tether by name." ;;
esac
echo "Quit the Codex app and start it again."
