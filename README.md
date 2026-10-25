# codex-tether

*one Codex conversation in the terminal and the Windows desktop app*

codex-tether lets the Codex desktop app on Windows and the Codex terminal UI
work on the same conversation at the same time. You can start a thread in a
terminal over SSH, continue it in the app, and go back to the terminal, with
both sides open.

The app runs Codex through a background process called the app-server, which
loads conversations and runs the agent. codex-tether sits between the app and
the Codex CLI it launches: it starts the app's app-server as a shared server on
a loopback port, and terminal sessions connect to that server instead of
starting their own. Each terminal session reports whether it attached, so you
know before you type whether the thread is shared with the app.

codex-tether supports:

- **Resuming over SSH.** `codex-tether tui resume --last` in a Termius or
  OpenSSH session opens the thread that is loaded in the app.
- **Dropped connections.** The conversation stays loaded in the app's server
  when an SSH session drops, and a new terminal session resumes it.
- **The app's own tools.** The shared server receives the app's arguments and
  environment unchanged, including the connection to the app's tools.
- **Matching versions.** Terminal sessions run the app's own copy of the Codex
  CLI, so client and server are the same release.

codex-tether uses options the Codex CLI already provides: the app-server's
`--listen` with token authentication and the terminal UI's `--remote`. The app
and the CLI stay unmodified, and `install.ps1 -Uninstall` restores the original
setup.

## Installation

You need Windows, the Codex desktop app (started at least once) and
[Go](https://go.dev/dl/) 1.22 or newer to build codex-tether. In a normal,
non-elevated PowerShell:

```powershell
git clone https://github.com/gcol33/codex-tether
cd codex-tether
.\install.ps1
```

The script builds `codex-tether.exe`, copies it to
`%LOCALAPPDATA%\codex-tether`, adds that folder to your PATH and sets
`CODEX_CLI_PATH` so the app launches it. Quit the Codex app, including its tray
icon, and start it again.

If you start the app from the Start menu or the taskbar, it inherits
Explorer's environment, which may not have the new variable yet. Sign out and
back in once.

## Usage

1. Start the Codex desktop app.
2. In a new terminal, local or over SSH, run `codex-tether tui` with the
   arguments you would give `codex`:

   ```powershell
   codex-tether tui resume --last
   ```

3. Check the first line it prints:

   ```
   codex-tether: attached to the desktop app server (ws://127.0.0.1:60802)
   ```

   The thread is now open in both places, and messages from either side
   appear in the other.

When the app is closed, `codex-tether tui` prints the reason and runs
Codex locally, as plain `codex` would.

Other ways to call it:

```powershell
codex-tether tui                    # new conversation
codex-tether tui resume             # pick a conversation
codex-tether tui --model gpt-5.5    # any other codex option
```

### Making it your `codex` command

A function in your PowerShell profile (the file that `$PROFILE` names) sends
every `codex` call through codex-tether:

```powershell
function codex { codex-tether tui @args }
```

In Git Bash, the same line is `alias codex='codex-tether tui'` in
`~/.bashrc`. With `CODEX_TETHER_DISABLE` set to `1`, `codex-tether tui` runs
Codex locally even while the app is running.

### Other commands

| Command | What it does |
| --- | --- |
| `codex-tether status` | Prints the shared server's address, CLI path, process ID and start time. |
| `codex-tether attach` | Relays JSON-RPC lines between stdin/stdout and the shared server, for scripts. |

## Security

The shared server runs a full Codex agent with the app's permissions. It
listens on `127.0.0.1` and accepts a connection only with the token stored in
`%USERPROFILE%\.codex\tether\token`, which only your user account can read.
Other programs running as your user can read the token too, as they can
everything else under `.codex`.

## Compatibility

codex-tether relies on the app reading `CODEX_CLI_PATH` and on the CLI options
named above. It was tested with Codex desktop app 26.1013.4894.0 and its CLI
0.163.0-alpha.10.2 (October 2026). codex-tether is an independent project,
not affiliated with OpenAI.

## Documentation

Start with [How codex-tether works](docs/how-it-works.md): why the app and the
terminal conflict, and what codex-tether runs in between.

- [Troubleshooting](docs/troubleshooting.md): what to check when a terminal
  session does not attach or the app misbehaves.

## Getting help

Report bugs on [GitHub issues](https://github.com/gcol33/codex-tether/issues),
with the output of `codex-tether status` and the last lines of
`%USERPROFILE%\.codex\tether\tether.log`.

## Support

> "Software is like sex: it's better when it's free." — Linus Torvalds

I'm a PhD student who builds R packages in my free time because I believe good tools should be free and open. I started these projects for my own work and figured others might find them useful too.

If this package saved you some time, buying me a coffee is a nice way to say thanks. It helps with my coffee addiction.

[![Buy Me A Coffee](https://img.shields.io/badge/-Buy%20me%20a%20coffee-FFDD00?logo=buymeacoffee&logoColor=black)](https://buymeacoffee.com/gcol33)

## License

MIT
