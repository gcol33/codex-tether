# How codex-tether works

## Why the app and the terminal conflict

Every Codex client (the desktop app, the terminal UI, the IDE extension) sends
its requests over JSON-RPC to an app-server, the process that loads
conversations and runs the agent. When an app-server loads a conversation, it
takes that conversation's writer lock, a file in
`%USERPROFILE%\.codex\thread-writer-locks\`. Another app-server that tries to
load the same conversation gets the error

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
The app skips that route on Windows, and also whenever it passes configuration
overrides to its server. That background server would also lack the
connection to the app's tools (`CODEX_APP_TOOLS_PIPE_PATH`), which the app
gives only to a server it starts itself.

## How the app starts codex-tether

The app looks for its CLI in `CODEX_CLI_PATH` before it uses the copy it
stages under `%LOCALAPPDATA%\OpenAI\Codex\bin\<hash>\codex.exe`. `install.ps1`
points `CODEX_CLI_PATH` at `codex-tether.exe`, so every CLI call the app makes
runs codex-tether with the arguments meant for `codex.exe`.

codex-tether shares a call that starts a plain stdio app-server: `codex`,
optional global options, then `app-server` with no further subcommand and
without `--listen` or `--stdio`. Every other call (`exec-server`,
`app-server proxy` and the rest) runs the real CLI with the same arguments,
input, output and environment, and codex-tether returns its exit code.

The real CLI is the most recently written `codex.exe` in the app's staging
folder. `CODEX_TETHER_REAL_CLI` overrides it with a full path.

## Starting the shared server

For an app-server call, codex-tether:

1. Picks a free port on `127.0.0.1` and creates a 32-byte random token in
   `%USERPROFILE%\.codex\tether\token`, if the file does not exist yet.
2. Starts the real CLI with the app's arguments, adding
   `--listen ws://127.0.0.1:<port> --ws-auth capability-token --ws-token-file <token file>`.
3. Connects to the server as a WebSocket client with the token, retrying until
   the server accepts.
4. Writes the server's address, the token file, the real CLI path and both
   process IDs to `%USERPROFILE%\.codex\tether\endpoint.json`.
5. Relays between the app and the server. Each line the app writes to
   codex-tether's standard input goes to the server as one WebSocket text
   message, and each message from the server goes back to the app as one line
   on standard output. A UTF-8 byte order mark at the start of standard input
   is dropped.

The server's own log output goes to codex-tether's standard error, which the
app reads as its server log, and to `tether.log`.

When the app closes the connection, codex-tether stops the server and deletes
`endpoint.json`. When the server exits on its own, codex-tether exits with the
server's exit code.

## Environment passed to the server

When `CODEX_CLI_PATH` is set, the app removes
`CODEX_WINDOWS_SANDBOX_PACKAGE_FAMILY` from the environment it passes on. The
Windows sandbox needs it, so codex-tether puts it back for every process it
starts. The value comes from the install folder of the app that launched
codex-tether: `...\WindowsApps\<Name>_<Version>_<Arch>__<PublisherId>\...`
gives `<Name>_<PublisherId>`. When codex-tether is started from a shell, as
for `codex-tether tui`, the variable is left out. Everything else the app
sets, including `CODEX_APP_TOOLS_PIPE_PATH`, reaches the server unchanged.

## Stopping the server with the app

codex-tether runs inside a Windows job object that ends all its processes when
codex-tether ends. The real server is one of them, so when the app stops what
it takes for its CLI, the server stops too and releases its locks. The job
still lets a process start detached children on purpose, as the
`app-server daemon` launcher does.

## Connecting a terminal session

`codex-tether tui [args]` reads `endpoint.json` and asks the server's
`/readyz` address whether it is ready. When it answers, codex-tether runs the
CLI recorded in `endpoint.json`, the app's own copy, as

```
codex.exe [args] --remote ws://127.0.0.1:<port> --remote-auth-token-env CODEX_TETHER_TOKEN
```

with the token in `CODEX_TETHER_TOKEN`. When `[args]` contain a `--`
separator, the added options go before it. When no server answers, codex-tether
prints the reason and runs the real CLI locally with `[args]`.

## Files

| Path | Content |
| --- | --- |
| `%LOCALAPPDATA%\codex-tether\codex-tether.exe` | Installed binary, named by `CODEX_CLI_PATH`. |
| `%USERPROFILE%\.codex\tether\token` | Token for connections to the shared server, readable by your user only. |
| `%USERPROFILE%\.codex\tether\endpoint.json` | Address and process IDs of the running shared server. |
| `%USERPROFILE%\.codex\tether\tether.log` | One line per CLI call and server event, plus the server's own log. |
