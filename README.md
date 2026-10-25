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

Each system needs the Codex desktop app, started at least once, plus Git and
[Go](https://go.dev/dl/) 1.22 or newer to build codex-tether. The installer
copies codex-tether to a folder on your PATH and sets `CODEX_CLI_PATH`, the
variable the app reads to find its CLI.

### Windows

1. Install Git and Go if you do not have them:
   `winget install Git.Git GoLang.Go`, then open a new terminal.
2. In a normal PowerShell window (not "Run as administrator"):

   ```powershell
   git clone https://github.com/gcol33/codex-tether
   cd codex-tether
   powershell -ExecutionPolicy Bypass -File .\install.ps1
   ```

   This installs `%LOCALAPPDATA%\codex-tether\codex-tether.exe`, adds that
   folder to your user PATH and sets `CODEX_CLI_PATH` for your user.
   `-ExecutionPolicy Bypass` lets this one script run on systems that block
   PowerShell scripts by default.
3. Quit the Codex app, including its tray icon, then sign out of Windows and
   back in. The app started from the Start menu or taskbar only sees the new
   variable after that.
4. Start the app and run `codex-tether status` in a new terminal. It prints
   `shared server: ws://127.0.0.1:...` when everything works.

To remove it, run `powershell -ExecutionPolicy Bypass -File .\install.ps1 -Uninstall`
in the repository folder and restart the app.

### macOS

1. Install Go if you do not have it: `brew install go`. Git comes with the
   Xcode command line tools (`xcode-select --install`).
2. In Terminal:

   ```sh
   git clone https://github.com/gcol33/codex-tether
   cd codex-tether
   ./install.sh
   ```

   This installs `~/.local/bin/codex-tether`, sets `CODEX_CLI_PATH` with
   `launchctl setenv` for apps you start from now on, and adds a LaunchAgent
   (`~/Library/LaunchAgents/com.github.gcol33.codex-tether.plist`) that sets
   it again at each login.
3. If the installer says `~/.local/bin` is not on your PATH, add this line to
   `~/.zshrc` and open a new terminal:

   ```sh
   export PATH="$HOME/.local/bin:$PATH"
   ```

4. Quit the Codex app with Cmd-Q and start it again.
5. Run `codex-tether status`. It prints `shared server: ws://127.0.0.1:...`
   when everything works.

To remove it, run `./install.sh --uninstall` in the repository folder and
restart the app.

### Linux

1. Install Git and Go if you do not have them, for example
   `sudo apt install git golang-go` on Debian and Ubuntu. Check that
   `go version` prints 1.22 or newer; older distributions ship an older Go,
   in which case install it from [go.dev](https://go.dev/dl/).
2. In a terminal:

   ```sh
   git clone https://github.com/gcol33/codex-tether
   cd codex-tether
   ./install.sh
   ```

   This installs `~/.local/bin/codex-tether` and writes `CODEX_CLI_PATH` to
   `~/.config/environment.d/codex-tether.conf`, which systemd-based desktops
   (GNOME, KDE Plasma) read at login.
3. If the installer says `~/.local/bin` is not on your PATH, add this line to
   `~/.bashrc` (or `~/.zshrc`) and open a new terminal:

   ```sh
   export PATH="$HOME/.local/bin:$PATH"
   ```

4. Quit the Codex app, sign out and back in, and start the app again.
5. Run `codex-tether status`. It prints `shared server: ws://127.0.0.1:...`
   when everything works.

On a desktop that does not read `environment.d`, start the app from a
terminal with `CODEX_CLI_PATH="$HOME/.local/bin/codex-tether"` set in its
environment.

To remove it, run `./install.sh --uninstall` in the repository folder and
restart the app.

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
listens on `127.0.0.1` and accepts a connection only with the token stored in
`.codex/tether/token` in your home directory, which only your user account can
read. Other programs running as your user can read the token too, as they can
everything else under `.codex`.

## Compatibility

codex-tether relies on the app reading `CODEX_CLI_PATH` and on the CLI options
named above. On Windows it was tested with the Codex desktop app
26.1013.4894.0 and its CLI 0.163.0-alpha.10.2 (October 2026). On macOS and
Linux it was tested with a stand-in for the app laid out like the app's
bundle, running Codex CLI 0.164.0; a test with the real macOS and Linux apps
is still open.

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
