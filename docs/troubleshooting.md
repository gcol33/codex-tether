# Troubleshooting

Most answers are in `.codex/tether/tether.log` in your home directory
(`%USERPROFILE%\.codex\tether\tether.log` on Windows). Each time the app starts
its server, the log gets a `shared app-server:` line followed by
`server pid <pid> on ws://127.0.0.1:<port>`.

## Why does `codex-tether status` say there is no shared server?

The app started its server without codex-tether, usually for one of these
reasons:

- **The app was already running during the install.** Quit it, including the
  tray or menu bar icon, and start it again.
- **The app did not get `CODEX_CLI_PATH`.** An app started from the Windows
  Start menu or taskbar inherits Explorer's environment, and an app started
  from a Linux desktop inherits the session's environment; either may predate
  the install. Sign out and back in once. On macOS, `launchctl getenv
  CODEX_CLI_PATH` should print the installed path.
- **The log shows `passthrough:` for the app-server call.** The app passed
  `--listen` or `--stdio` itself, or `CODEX_TETHER_DISABLE` is set to `1`.

The process list shows what the app actually runs: the app's codex-tether
`app-server` process should have a child running the real CLI with
`--listen ws://127.0.0.1:...`. On Windows, add the Command line column in Task
Manager's Details tab; on macOS and Linux, `ps -ef | grep codex` shows both.

## Why does the terminal still say "This conversation is open in another app"?

The terminal session did not attach and started its own server. The first
line of `codex-tether tui` says which happened: `attached to the desktop app
server (...)`, or the reason followed by `running locally`. A local session
competes with the app for the conversation, so close the conversation on the
other side, or end the process that still has it open.

A session started with plain `codex` never attaches. The alias in the README
makes `codex` call `codex-tether tui`.

## Why does `codex-tether tui` say it cannot find the Codex CLI?

With the app closed, a terminal session uses the CLI the app last ran, then
`codex` on your PATH. Start the app once with codex-tether installed, install
the Codex CLI, or set `CODEX_TETHER_REAL_CLI` to the full path of a `codex`
binary.

## Why do the app's tools (browser, computer use) stop working?

These tools reach the server through a connection the app sets up, which
codex-tether passes on unchanged. If they fail only with codex-tether
installed, set `CODEX_TETHER_DISABLE` to `1` in the app's environment and
restart the app to confirm. Then open an issue with the log.

## Which CLI does the shared server run after an app update?

The one the updated app ships: codex-tether looks it up again each time the
app starts it. `codex-tether status` prints the binary the server runs, and
running that binary with `--version` prints its version. To use a different
binary, set `CODEX_TETHER_REAL_CLI` to its full path in the app's environment
and restart the app.

## How do I remove codex-tether?

Run the uninstall command from the repository folder (`.\install.ps1
-Uninstall` on Windows, `./install.sh --uninstall` on macOS and Linux) and
restart the app. The `tether` folder under `.codex` can be deleted afterwards.
