# Troubleshooting

Check `%USERPROFILE%\.codex\tether\tether.log` first. Each app launch of the
server logs `shared app-server: ...` and `server pid ... on ws://...`.

## `codex-tether status` says there is no shared server

- **The app was started before the install.** Quit it fully (including the tray
  icon) and start it again.
- **The app was started from the Start menu or taskbar.** It inherits Explorer's
  environment, which does not always pick up a new user variable. Sign out and
  back in once.
- **The log shows `passthrough` for the app-server call.** The app passed an
  explicit `--listen` or `--stdio`, or `CODEX_TETHER_DISABLE=1` is set.

Check what the app actually runs: in Task Manager's Details tab, add the
"Command line" column. The app's `codex-tether.exe ... app-server` should have
a child `codex.exe ... --listen ws://127.0.0.1:...`.

## The terminal still says "open in another app"

The TUI did not attach and runs its own server. The first line of
`codex-tether tui` tells which: `attached to the desktop app server (...)` or
`...; running locally`. When it runs locally, another client holds the thread
until it exits. Close the thread there, or end the stale process.

A session started with plain `codex` instead of `codex-tether tui` never
attaches.

## The daemon refuses to start, or a session runs as administrator

Codex refuses to run its shared daemon elevated. Run `install.ps1` and start
the app from a normal PowerShell or the Start menu. A TUI over OpenSSH often
runs elevated for administrator accounts, which is fine for `codex-tether tui`,
because it only connects as a client.

## The app's tools (browser, computer use) stop working

These need the app's tool connection, which codex-tether passes through to the
server unchanged. If they fail only with codex-tether installed, set
`CODEX_TETHER_DISABLE=1` for your user, restart the app to confirm, and open an
issue with the log.

## After an app update

The app stages its CLI under `%LOCALAPPDATA%\OpenAI\Codex\bin\<hash>\`, and
codex-tether uses the most recently written `codex.exe` there.
`codex-tether status` shows which binary the shared server runs, and
`<that path> --version` shows its version. To use a different binary, set
`CODEX_TETHER_REAL_CLI` to its full path for your user and restart the app.

## Removing it

Run `.\install.ps1 -Uninstall` and restart the app. The `tether` folder under
`%USERPROFILE%\.codex` can be deleted.
