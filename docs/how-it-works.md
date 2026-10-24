# How codex-tether works

## The lock

Every Codex client (desktop app, TUI, IDE extension) talks to an app-server
over JSON-RPC. The app-server that loads a thread takes that thread's writer
lock in `%USERPROFILE%\.codex\thread-writer-locks\`. A second app-server that
tries to load the same thread gets

```
thread <id> already has an active writer
```

which the TUI shows as "This conversation is open in another app". The desktop
app starts its own app-server and the TUI starts another one, so the two always
compete. Two clients connected to the same app-server do not: they share the
loaded thread.

On macOS and Linux the app can already attach to a shared local daemon
(`codex app-server daemon`) when `CODEX_APP_SERVER_USE_LOCAL_DAEMON=1` is set.
The app skips that path on Windows, and it also skips it whenever it passes
config overrides to its server, which it does to enable its own tools. The
standalone daemon would also lack the app's tool connection
(`CODEX_APP_TOOLS_PIPE_PATH`), which the app hands only to the server it starts
itself.

## Launch path

The app resolves its CLI binary from `CODEX_CLI_PATH` before falling back to
the copy it stages under `%LOCALAPPDATA%\OpenAI\Codex\bin\<hash>\codex.exe`.
With codex-tether installed there, each CLI invocation from the app runs
`codex-tether.exe` with the arguments meant for `codex.exe`.

codex-tether decides per invocation:

- **Plain stdio app-server** (`codex [global options] app-server [options]`,
  with no nested subcommand and no `--listen` or `--stdio`): share it, below.
- **Anything else** (`exec-server`, `app-server daemon ...`, `app-server proxy`,
  and so on): run the real binary with the same arguments, stdio and
  environment, and return its exit code.

The real binary is the newest staged `codex.exe` under
`%LOCALAPPDATA%\OpenAI\Codex\bin\`. `CODEX_TETHER_REAL_CLI` overrides it.

## Sharing the app-server

1. Pick a free loopback port and make sure a 32-byte random token exists in
   `%USERPROFILE%\.codex\tether\token`.
2. Start the real binary with the app's arguments plus
   `--listen ws://127.0.0.1:<port> --ws-auth capability-token --ws-token-file <token>`.
3. Connect to it as a WebSocket client with `Authorization: Bearer <token>`,
   retrying until the server accepts.
4. Write `%USERPROFILE%\.codex\tether\endpoint.json` with the URL, token file,
   real CLI path and process IDs.
5. Relay: each line the app writes to stdin goes to the server as one text
   message, and each message from the server goes to stdout as one line. A
   UTF-8 byte order mark at the start of stdin is dropped. The server's own
   stdout and stderr go to codex-tether's stderr, which the app reads as logs,
   and to `tether.log`.

When the app closes stdin, codex-tether stops the server and removes the
endpoint file. When the server exits, codex-tether exits with its code.

## Environment

The app removes `CODEX_WINDOWS_SANDBOX_PACKAGE_FAMILY` from the environment
when `CODEX_CLI_PATH` is set. codex-tether puts it back for every child,
derived from the install folder of the process that launched it
(`...\WindowsApps\<Name>_<Version>_<Arch>__<PublisherId>\...` gives
`<Name>_<PublisherId>`). When the parent is not a packaged app, as for
`codex-tether tui` run from a shell, nothing is added. Everything else the app
sets, including `CODEX_APP_TOOLS_PIPE_PATH`, passes through unchanged.

## Process lifetime

codex-tether places itself in a Windows job object with
`JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`. Its children inherit the job, so when the
app terminates what it believes is its CLI, the real server goes with it and
cannot keep holding thread locks. `JOB_OBJECT_LIMIT_BREAKAWAY_OK` is set as
well, so children that start detached processes on purpose (the app-server
daemon launcher) still can.

## Terminal side

`codex-tether tui [args]` reads `endpoint.json` and checks the server's
`/readyz`. When it answers, it runs the real CLI recorded there (the app's own
CLI, so client and server versions match) as

```
codex.exe [args] --remote ws://127.0.0.1:<port> --remote-auth-token-env CODEX_TETHER_TOKEN
```

with the token in `CODEX_TETHER_TOKEN`. The flags go before a `--` separator
when there is one. When no server answers, it runs the real CLI with `[args]`
locally.

## Files

| Path | Content |
| --- | --- |
| `%LOCALAPPDATA%\codex-tether\codex-tether.exe` | Installed binary (`CODEX_CLI_PATH`). |
| `%USERPROFILE%\.codex\tether\token` | WebSocket capability token, user-only ACL. |
| `%USERPROFILE%\.codex\tether\endpoint.json` | Address of the running shared server. |
| `%USERPROFILE%\.codex\tether\tether.log` | One line per invocation and server lifecycle event, plus the server's own output. |
