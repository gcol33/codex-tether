# How codex-tether works

Paths below use `~/.codex` for the Codex home directory: `.codex` in your home
directory (`%USERPROFILE%\.codex` on Windows), or `CODEX_HOME` when it is set.

## Why the app and the terminal conflict

Every Codex client (the desktop app, the terminal UI, the IDE extension) sends
its requests over JSON-RPC to an app-server, the process that loads
conversations and runs the agent. When an app-server loads a conversation, it
takes that conversation's writer lock, a file in
`~/.codex/thread-writer-locks/`. Another app-server that tries to load the
same conversation gets the error

```
thread <id> already has an active writer
```

and the terminal UI shows it as "This conversation is open in another app".

The desktop app starts its own app-server, and the terminal UI starts another,
so the two compete for every conversation they both open. Two clients
connected to one app-server share the loaded conversation and never meet the
lock. codex-tether makes the app's app-server that one server.

On macOS and Linux the app can attach to a shared background server
(`codex app-server daemon`) when `CODEX_APP_SERVER_USE_LOCAL_DAEMON=1` is set.
The app skips that route on Windows, and on every system whenever it passes
configuration overrides to its server. That background server would also lack
the connection to the app's tools (`CODEX_APP_TOOLS_PIPE_PATH`), which the app
gives only to a server it starts itself.

## How the app starts codex-tether

The app looks for its CLI in `CODEX_CLI_PATH` before it uses the copy it
ships. The installer points `CODEX_CLI_PATH` at codex-tether, so every CLI
call the app makes runs codex-tether with the arguments meant for the CLI.

codex-tether shares a call that starts a plain stdio app-server: `codex`,
optional global options, then `app-server` with no further subcommand and
without `--listen`, `--stdio` or a print-and-exit option (`--help`,
`--version`, `--print-startup-config`). Every other call (`exec-server`,
`app-server proxy` and the rest) runs the real CLI with the same arguments,
input, output and environment, and codex-tether returns its exit code.

codex-tether finds the real CLI the way the app does:

| System | Real CLI |
| --- | --- |
| Windows | `codex.exe` in the `resources` folder beside the app's executable, inside its package. |
| macOS | `codex` in the `Contents/Resources` folder of the app bundle that started codex-tether. |
| Linux | `codex`, `app.asar.unpacked/codex` or `codex-linux/codex` in the `resources` folder beside the app's executable. |

Each time the app starts codex-tether, the path it found is written to
`~/.codex/tether/app-cli`. A terminal session with no app running uses that
path, then `codex` on your PATH. `CODEX_TETHER_REAL_CLI` overrides all of
these with a full path.

## Starting the shared server

For an app-server call, codex-tether, with `<pid>` its own process ID:

1. Deletes the files of servers whose codex-tether is no longer running, and
   on macOS stops such a server (see below).
2. Picks a free port on `127.0.0.1` and writes a new 32-byte random token to
   `~/.codex/tether/token-<pid>`.
3. Starts the real CLI with the app's arguments, adding
   `--listen ws://127.0.0.1:<port> --ws-auth capability-token --ws-token-file <token file>`.
   On macOS and Linux the server gets its own process group.
4. Connects to the server as a WebSocket client with the token, retrying until
   the server accepts.
5. Writes the server's address, the token file, the real CLI path and both
   process IDs to `~/.codex/tether/server-<pid>.json`.
6. Relays between the app and the server. Each line the app writes to
   codex-tether's standard input goes to the server as one WebSocket text
   message, and each message from the server goes back to the app as one line
   on standard output. A UTF-8 byte order mark at the start of standard input
   is dropped.

If the server exits before step 4 succeeds, or does not accept a connection
within 60 seconds, codex-tether stops it and runs the real CLI with the app's
original arguments, so the app gets an ordinary unshared app-server. This
happens, for example, when a CLI does not accept `--listen`.

Because each server has its own token and files, two app-servers started at
the same time do not interfere: each authenticates only with its own server,
and terminal sessions use the most recently started one that is running.

The server's own log output goes to codex-tether's standard error, which the
app reads as its server log, and to `tether.log`.

When the app closes the connection, codex-tether stops the server and deletes
its server and token files. When the server exits on its own, codex-tether
exits with the server's exit code.

## Stopping the server with the app

A server that outlives its codex-tether would keep its conversations locked,
so each system stops it with the app:

- **Windows**: codex-tether runs inside a job object that ends all its
  processes when codex-tether is terminated. The job still lets a process
  start detached children on purpose, as the `app-server daemon` launcher
  does. For calls other than the app-server, codex-tether releases the job
  before it exits normally, so processes the CLI leaves running keep running,
  as they would without codex-tether.
- **Linux**: the server gets a parent-death signal, so the kernel kills it
  when codex-tether exits, including by `SIGKILL`. `SIGTERM` and `SIGHUP` sent
  to codex-tether are passed on to the server's process group, and stopping
  the server stops that whole group.
- **macOS**: signals and stopping work on the process group as on Linux.
  macOS has no parent-death signal, so a server whose codex-tether was killed
  outright keeps running until the next app-server call, when step 1 above
  stops it. That step first checks that the recorded process ID still runs
  the recorded CLI and started at the recorded time.

## Environment passed to the server

Everything the app sets, including `CODEX_APP_TOOLS_PIPE_PATH`, reaches the
server unchanged, with one addition on Windows. There the app removes
`CODEX_WINDOWS_SANDBOX_PACKAGE_FAMILY` from the environment when
`CODEX_CLI_PATH` is set, and the Windows sandbox needs it. codex-tether puts it
back for every process it starts, derived from the install folder of the app
that launched it: `...\WindowsApps\<Name>_<Version>_<Arch>__<PublisherId>\...`
gives `<Name>_<PublisherId>`. When codex-tether is started from a shell, as
for `codex-tether tui`, the variable is left out.

## Connecting a terminal session

`codex-tether tui [args]` reads the server files, newest first, and asks each
server's `/readyz` address whether it is ready. With the first that answers,
codex-tether runs the CLI recorded in its file, the app's own copy, as

```
codex [args] --remote ws://127.0.0.1:<port> --remote-auth-token-env CODEX_TETHER_TOKEN
```

with the token in `CODEX_TETHER_TOKEN`. When `[args]` contain a `--`
separator, the added options go before it. When no server answers, codex-tether
prints the reason and runs the real CLI locally with `[args]`.

## Files

| Path | Content |
| --- | --- |
| `~/.codex/tether/token-<pid>` | Token for connections to one shared server, readable by your user only. |
| `~/.codex/tether/server-<pid>.json` | Address, CLI path and process IDs of one running shared server. |
| `~/.codex/tether/app-cli` | Path of the app's CLI, from the last time the app started codex-tether. |
| `~/.codex/tether/tether.log` | One line per CLI call and server event, plus the server's own log. Moved to `tether.log.1` when it passes 8 MB. |

`<pid>` is the process ID of the codex-tether that started the server.

The installed binary is `%LOCALAPPDATA%\codex-tether\codex-tether.exe` on
Windows and `~/.local/bin/codex-tether` on macOS and Linux.
