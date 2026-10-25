# Troubleshooting

Most answers are in `%USERPROFILE%\.codex\tether\tether.log`. Each time the app
starts its server, the log gets a `shared app-server:` line followed by
`server pid <pid> on ws://127.0.0.1:<port>`.

## Why does `codex-tether status` say there is no shared server?

The app started its server without codex-tether, usually for one of these
reasons:

- **The app was already running during the install.** Quit it, including the
  tray icon, and start it again.
- **The app was started from the Start menu or the taskbar.** It inherits
  Explorer's environment, which may not contain `CODEX_CLI_PATH` yet. Sign out
  and back in once.
- **The log shows `passthrough:` for the app-server call.** The app passed
  `--listen` or `--stdio` itself, or `CODEX_TETHER_DISABLE` is set to `1`.

Task Manager shows what the app actually runs. In the Details tab, add the
Command line column: `codex-tether.exe ... app-server` should have a child
`codex.exe ... --listen ws://127.0.0.1:...`.

## Why does the terminal still say "This conversation is open in another app"?

The terminal session did not attach and started its own server. The first
line of `codex-tether tui` says which happened: `attached to the desktop app
server (...)`, or the reason followed by `running locally`. A local session
competes with the app for the conversation, so close the conversation on the
other side, or end the process that still has it open.

A session started with plain `codex` never attaches. The profile function in
the README makes `codex` call `codex-tether tui`.

## Why do the app's tools (browser, computer use) stop working?

These tools reach the server through a connection the app sets up, which
codex-tether passes on unchanged. If they fail only with codex-tether
installed, set `CODEX_TETHER_DISABLE` to `1` for your user and restart the app
to confirm. Then open an issue with the log.

## Which CLI does the shared server run after an app update?

The app stages its CLI under `%LOCALAPPDATA%\OpenAI\Codex\bin\<hash>\`, and
codex-tether uses the most recently written `codex.exe` there.
`codex-tether status` prints the binary the server runs, and running that
binary with `--version` prints its version. To use a different binary, set
`CODEX_TETHER_REAL_CLI` to its full path for your user and restart the app.

## How do I remove codex-tether?

Run `.\install.ps1 -Uninstall` from the repository folder and restart the app.
The script removes `CODEX_CLI_PATH`, the PATH entry and the installed binary.
The `tether` folder under `%USERPROFILE%\.codex` can be deleted afterwards.
