# tray

A StatusNotifierItem host for Linux.

Most tray hosts assume a status bar or panel to reside in. I see no use for such UI; I want a ui i can toggle with a keyboard shortcut.

## Features

- **Single binary** for host, CLI, and UI.
- **In-process UI**: the UI is not a separate process.
- **No-polling**: owns the SNI watcher, tracks items by reacting to D-Bus signals.
- **UI**: information tooltips, theme-based icons, pixmap-based icons, DBusMenu menus.
- **Safety**:
  - Readiness pipe, per-socket PID files, process-aware stop/restart, socket checks and 0600 permissions.
  - Bounded D-Bus/IPC timeouts and async/coalesced SNI refreshes.
  - Debug-tag mutex for deadlock/lock misuse.

## Build

```sh
go build -o tray ./cmd/tray
```

## Usage

Start the host (ui hidden by default):

```sh
./tray host start
```

Show the UI:

```sh
./tray ui show
```

Other commands:

```sh
./tray host stop
./tray host restart
./tray list
./tray status
./tray ui hide
./tray ui toggle
```

The host registers `org.kde.StatusNotifierWatcher` on the session bus and exposes a Unix socket API for CLI/UI control.

## Principles

- Polling and timing-based programming are anti-patterns. Timeouts are only used as safety bounds.
- Shutdown must be reliable and idempotent, including during UI startup.
- Never report false success.
- Do not block on slow or broken peers; keep the UI responsive.
- Safety over convenience for destructive lifecycle operations.
- The UI stays thin: the host owns state/policy; the UI renders and commands.
