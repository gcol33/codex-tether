# codex-tether

*one Codex conversation in the terminal and the desktop app*

codex-tether lets the Codex desktop app and the Codex terminal UI work on the
same conversation at the same time, on Windows, macOS and Linux. You can start
a thread in a terminal over SSH, continue it in the app, and go back to the
terminal, with both sides open.

The app runs Codex through a background process called the app-server, which
loads conversations and runs the agent. codex-tether sits between the app and
the Codex CLI it launches: it starts the app's app-server as a shared server on
a loopback port, and terminal sessions connect to that server instead of
starting their own. Each terminal session reports whether it attached, so you
know before you type whether the thread is shared with the app.

codex-tether supports:

- **Resuming over SSH.** `codex-tether tui resume --last` in an SSH session
  opens the thread that is loaded in the app.
- **Dropped connections.** The conversation stays loaded in the app's server
  when an SSH session drops, and a new terminal session resumes it.
- **The app's own tools.** The shared server receives the app's arguments and
  environment unchanged, including the connection to the app's tools.
- **Matching versions.** Terminal sessions run the app's own copy of the Codex
  CLI, so client and server are the same release.

codex-tether uses options the Codex CLI already provides: the app-server's
`--listen` with token authentication and the terminal UI's `--remote`. The app
and the CLI stay unmodified, and the uninstall step restores the original
setup.

## Installation

Each system needs the Codex desktop app, started at least once. The installer
downloads codex-tether for your system from the latest
[release](https://github.com/gcol33/codex-tether/releases), checks it against
the release's SHA-256 checksums, puts it in a folder on your PATH and sets
`CODEX_CLI_PATH`, the variable the app reads to find its CLI.

### Windows

1. In a normal PowerShell window (not "Run as administrator"):

   ```powershell
   irm https://raw.githubusercontent.com/gcol33/codex-tether/main/install.ps1 | iex
   ```

   This installs `%LOCALAPPDATA%\codex-tether\codex-tether.exe`, adds that
   folder to your user PATH and sets `CODEX_CLI_PATH` for your user.
2. Quit the Codex app, including its tray icon, then sign out of Windows and
   back in. The app started from the Start menu or taskbar only sees the new
   variable after that.
3. Start the app and run `codex-tether status` in a new terminal. It prints
   `shared server: ws://127.0.0.1:...` when everything works.

To remove it, quit the app, run the following, then sign out and back in
before starting the app again:

```powershell
& ([scriptblock]::Create((irm https://raw.githubusercontent.com/gcol33/codex-tether/main/install.ps1))) -Uninstall
```

### macOS

1. In Terminal:

   ```sh
   curl -fsSL https://raw.githubusercontent.com/gcol33/codex-tether/main/install.sh | sh
   ```

   This installs `~/.local/bin/codex-tether`, sets `CODEX_CLI_PATH` with
   `launchctl setenv` for apps you start from now on, and adds a LaunchAgent
   (`~/Library/LaunchAgents/com.github.gcol33.codex-tether.plist`) that sets
   it again at each login.
2. If the installer says `~/.local/bin` is not on your PATH, add this line to
   `~/.zshrc` and open a new terminal:

   ```sh
   export PATH="$HOME/.local/bin:$PATH"
   ```

3. Quit the Codex app with Cmd-Q and start it again.
4. Run `codex-tether status`. It prints `shared server: ws://127.0.0.1:...`
   when everything works.

### Linux

1. In a terminal:

   ```sh
   curl -fsSL https://raw.githubusercontent.com/gcol33/codex-tether/main/install.sh | sh
   ```

   This installs `~/.local/bin/codex-tether` and writes `CODEX_CLI_PATH` to
   `~/.config/environment.d/codex-tether.conf`, which systemd-based desktops
   (GNOME, KDE Plasma) read at login.
2. If the installer says `~/.local/bin` is not on your PATH, add this line to
   `~/.bashrc` (or `~/.zshrc`) and open a new terminal:

   ```sh
   export PATH="$HOME/.local/bin:$PATH"
   ```

3. Quit the Codex app, sign out and back in, and start the app again.
4. Run `codex-tether status`. It prints `shared server: ws://127.0.0.1:...`
   when everything works.

On a desktop that does not read `environment.d`, start the app from a
terminal with `CODEX_CLI_PATH="$HOME/.local/bin/codex-tether"` set in its
environment.

To remove codex-tether on macOS or Linux, quit the app and run the following.
On macOS, start the app again; on Linux, sign out and back in first.

```sh
curl -fsSL https://raw.githubusercontent.com/gcol33/codex-tether/main/install.sh | sh -s -- --uninstall
```

### Building from source

With Git and [Go](https://go.dev/dl/) 1.22 or newer, clone the repository and
run the installer from the checkout. It builds codex-tether instead of
downloading it:

```sh
git clone https://github.com/gcol33/codex-tether
cd codex-tether
./install.sh                                            # macOS, Linux
powershell -ExecutionPolicy Bypass -File .\install.ps1  # Windows
```

## Usage

1. Start the Codex desktop app.
2. In a new terminal, local or over SSH, run `codex-tether tui` with the
   arguments you would give `codex`:

   ```sh
   codex-tether tui resume --last
   ```

3. Check the first line it prints:

   ```
   codex-tether: attached to the desktop app server (ws://127.0.0.1:60802)
   ```

   The thread is now open in both places, and messages from either side
   appear in the other.

When the app is closed, `codex-tether tui` prints the reason and runs Codex
locally, as plain `codex` would.

Other ways to call it:

```sh
codex-tether tui                    # new conversation
codex-tether tui resume             # pick a conversation
codex-tether tui --model gpt-5.5    # any other codex option
```

### Making it your `codex` command

An alias in your shell's startup file sends every `codex` call through
codex-tether. For bash or zsh (`~/.bashrc`, `~/.zshrc`):

```sh
alias codex='codex-tether tui'
```

For PowerShell, in the file that `$PROFILE` names:

```powershell
function codex { codex-tether tui @args }
```

With `CODEX_TETHER_DISABLE` set to `1`, `codex-tether tui` runs Codex locally
even while the app is running.

### Other commands

| Command | What it does |
| --- | --- |
| `codex-tether status` | Prints the shared server's address, CLI path, process ID and start time. |
| `codex-tether attach` | Relays JSON-RPC lines between stdin/stdout and the shared server, for scripts. |

## Security

The shared server runs a full Codex agent with the app's permissions. It
listens on `127.0.0.1` and accepts a connection only with a random token that
codex-tether creates for each server start and stores in `.codex/tether` in
your home directory, readable by your user account only. Other programs
running as your user can read the token too, as they can everything else
under `.codex`.

## Compatibility

codex-tether relies on the app reading `CODEX_CLI_PATH` and on the CLI options
named above. On Windows it was tested with the Codex desktop app
26.1013.4894.0 and its CLI 0.163.0-alpha.10.2 (October 2026). On macOS and
Linux it was tested with a stand-in for the app laid out like the app's
bundle, running Codex CLI 0.164.0; a test with the real macOS and Linux apps
is still open. When a CLI version does not start the shared server,
codex-tether gives the app an ordinary unshared server, so the app keeps
working and `codex-tether tui` runs locally.

On macOS, a server whose codex-tether was killed outright keeps running until
the app next starts its server, when codex-tether stops it. Windows and Linux
stop it at once.

codex-tether is an independent project, not affiliated with OpenAI.

## Documentation

Start with [How codex-tether works](docs/how-it-works.md): why the app and the
terminal conflict, and what codex-tether runs in between.

- [Troubleshooting](docs/troubleshooting.md): what to check when a terminal
  session does not attach or the app misbehaves.

## Getting help

Report bugs on [GitHub issues](https://github.com/gcol33/codex-tether/issues),
with your system, the output of `codex-tether status` and the last lines of
`.codex/tether/tether.log` in your home directory.

## Support

> "Software is like sex: it's better when it's free." — Linus Torvalds

I'm a PhD student who builds tools in my free time because I believe good tools should be free and open. I started these projects for my own work and figured others might find them useful too.

If this tool saved you some time, buying me a coffee is a nice way to say thanks. It helps with my coffee addiction.

[![Buy Me A Coffee](https://img.shields.io/badge/-Buy%20me%20a%20coffee-FFDD00?logo=buymeacoffee&logoColor=black)](https://buymeacoffee.com/gcol33)

## License

MIT
