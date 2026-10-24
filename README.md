# codex-tether

Start a Codex conversation in a terminal over SSH, pick it up in the Codex
desktop app, and switch back, on Windows. The app and the terminal attach to
one server, so the same thread is live in both.

```powershell
codex-tether tui resume --last
```

```
codex-tether: attached to the desktop app server (ws://127.0.0.1:65384)
```

Without it, whichever client opens a thread first holds its writer lock, and
the other one shows "This conversation is open in another app" until that
process exits. A dropped SSH session keeps the lock until someone kills it.

## Installation

Requires Windows and the Codex desktop app (Microsoft Store), which must have
started at least once.

1. Download `codex-tether.exe` and `install.ps1` from the
   [latest release](../../releases/latest) into one folder, or clone this
   repository and have Go 1.22 or newer installed.
2. In a normal (non-elevated) PowerShell, run `.\install.ps1`.
3. Quit the Codex app and start it again.

`codex-tether status` then prints the shared server. If it reports no server,
sign out and back in once, so Explorer passes the new `CODEX_CLI_PATH` to the
app.

To remove it, run `.\install.ps1 -Uninstall` and restart the app.

## Usage

`codex-tether tui` takes the same arguments as `codex`:

```powershell
codex-tether tui                        # new conversation
codex-tether tui resume --last          # continue the most recent one
codex-tether tui resume                 # pick one
codex-tether tui --model gpt-5.5        # any other codex option
```

When the app is running, the session attaches to its server and appears in
the app's thread list. When the app is closed, `tui` runs Codex locally, as
plain `codex` would.

Other commands:

| Command | What it does |
| --- | --- |
| `codex-tether status` | Prints the shared server's address, CLI path and start time. |
| `codex-tether attach` | Bridges stdin/stdout JSON-RPC lines to the server, for scripts. |

Set `CODEX_TETHER_DISABLE=1` to bypass sharing for one app launch or one
`tui` run.

## How it works

The desktop app finds its CLI through `CODEX_CLI_PATH`. codex-tether takes that
place. When the app starts its app-server, codex-tether starts the real
app-server with the same arguments and environment, listening on a random
loopback port behind a token (`--listen ws://127.0.0.1:<port> --ws-auth
capability-token`), and relays the app's stdio to it. `codex-tether tui` reads
the published endpoint and runs the app's own CLI with `--remote`, so both
clients talk to one server that owns every thread. Every other CLI call from
the app goes straight to the real binary.

Details, including what the app expects and what codex-tether restores, are in
[docs/how-it-works.md](docs/how-it-works.md). Problems and fixes are in
[docs/troubleshooting.md](docs/troubleshooting.md).

## Security

The shared server runs a full Codex agent with the app's permissions. It
listens on `127.0.0.1` only and accepts a connection only with the token in
`%USERPROFILE%\.codex\tether\token`. That file and its folder are readable by
your user account alone. Any process running as you can read the token, so the
protection is the same as for the rest of your `.codex` folder.

## Status

codex-tether is an unofficial community tool, not affiliated with or endorsed
by OpenAI. It relies on the app's `CODEX_CLI_PATH` override and the CLI's
`--listen`, `--ws-auth` and `--remote` options. A future app or CLI release may
change those.

Related upstream issues: openai/codex#44063, openai/codex#41849,
openai/codex#49137.

## License

MIT
