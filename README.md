# x4-foundations-notifier

Sends a push notification to your phone (ntfy or Telegram) when a ship in your X4: Foundations fleet finishes an order.

```
X4 (Windows)                                   same machine
┌──────────────────────────────┐
│ md/fleet_orders.xml          │  raise_lua_event  ┌─────────────────────┐
│ event_object_order_finished  │ ────────────────► │ ui/notifier.lua     │
└──────────────────────────────┘                   │ (menus env, x4_http)│
                                                   └─────────┬───────────┘
                                                             │ HTTP POST json
                                                   ┌─────────▼───────────┐
                                                   │ x4-notifier (bin/)  │
                                                   │ /x4/order-finished  │
                                                   └─────────┬───────────┘
                                                             │
                                              ┌──────────────┴──────────────┐
                                        ntfy.sh                        Telegram Bot API
```

Nothing blocks the game loop: the Lua side fires an asynchronous HTTP request and the server answers immediately, before it talks to the providers.

## Quick start (nothing to build)

The server binaries are already in `bin/`. They are self-contained — no Go, no runtime, no installer.

**Windows**

1. Download the repository (Code → Download ZIP) and extract it.
2. Copy the `fleet_notifier` folder into `...\X4 Foundations\extensions\fleet_notifier`.
3. Copy the whole `bin` folder somewhere permanent, for example `%USERPROFILE%\x4-notifier`.
4. Rename `bin\.env.example` to `bin\.env` and fill in `X4_TOKEN`, plus `NTFY_TOPIC` and/or the two Telegram values.
5. Open `X4 Foundations\extensions\fleet_notifier\ui\notifier.lua` and put the same token in `local TOKEN = "..."` (or leave both empty).
6. Double-click `bin\start-x4-notifier.bat` and leave the window open while you play. It shows a log line for every event it receives.
7. Start X4 and check Settings → Extensions: **djfhe http** and **Fleet Order Notifier** must both be enabled.

If Windows SmartScreen warns about the executable, choose "More info → Run anyway". The binaries are unsigned, which is normal for a small mod tool.

**Linux (Proton / Steam Deck)**

```bash
cd bin
cp .env.example .env      # then edit .env
./x4-notifier-linux-amd64
```

Either way, `curl http://127.0.0.1:8080/health` should answer with `"status":"ok"` and the binary version.

## Requirements

- X4: Foundations, with **Protected UI Mode disabled** (Settings → Game Settings). `djfhe_http` loads native DLLs, so it cannot run in protected mode.
- [djfhe/x4_http](https://github.com/djfhe/x4_http) installed in `extensions/djfhe_http`. It is a hard dependency in `content.xml`; the extension refuses to load without it.
- An ntfy topic and/or a Telegram bot (bot token from @BotFather, chat id from @userinfobot).

Building the server yourself is optional; see [Building from source](#building-from-source).

## Install the mod

1. Download/unpack [x4_http](https://github.com/djfhe/x4_http) into `X4 Foundations/extensions/djfhe_http`. If you downloaded the GitHub zip, rename the extracted folder to `djfhe_http` — the dependency resolves on the folder path.
2. Copy `fleet_notifier/` into `X4 Foundations/extensions/fleet_notifier`, or run `make mod-package` and extract `dist/fleet_notifier.zip` there.
3. Set `ENDPOINT` and `TOKEN` in `fleet_notifier/ui/notifier.lua`.
4. In the game: Settings → Extensions, enable **djfhe http** and **Fleet Order Notifier**.
5. Start X4 with `-debuglog` if you want to troubleshoot; the log is `Documents/Egosoft/X4/<id>/debuglog.txt`.

## Configuration

The server reads a `.env` file from the folder it is started in, and then the environment (a shell export still wins). `.env.example` lists everything:

| Variable | Default | Meaning |
| --- | --- | --- |
| `X4_LISTEN` | `127.0.0.1:8080` | Bind address. Use `0.0.0.0:8080` only if the game runs on another machine. |
| `X4_TOKEN` | empty | Shared secret. Must match `TOKEN` in `notifier.lua`. Empty means unauthenticated. |
| `NTFY_URL` | `https://ntfy.sh` | ntfy base URL, for a self-hosted instance too. |
| `NTFY_TOPIC` | empty | ntfy topic. Empty disables ntfy. |
| `NTFY_TOKEN` | empty | ntfy access token, for protected topics. |
| `TELEGRAM_BOT_TOKEN` | empty | Bot token from @BotFather. |
| `TELEGRAM_CHAT_ID` | empty | Chat that receives the messages. |
| `TELEGRAM_API_BASE` | `https://api.telegram.org` | Override for tests or a proxy. |

Both providers can be enabled at once. With neither set the server still runs and accepts events — it just logs them.

## Test it

```
curl -i http://127.0.0.1:8080/health
curl -i -X POST http://127.0.0.1:8080/x4/order-finished \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer change-me' \
  -d '{"code":"ABC-123","ship":"Argon Titan","order":"DockAt","sector":"Argon Prime"}'
```

A `202` means the notification was queued, `401` means the token does not match.

## How the pieces talk

Mission Director packs one string per event, because `raise_lua_event` carries a single value:

```
shipcode|shipname|ordername|sectorname
```

`ui/notifier.lua` splits it, builds a table and lets `x4_http` encode it as JSON with `Content-Type: application/json`:

```json
{"code": "ABC-123", "ship": "Argon Titan", "order": "DockAt", "sector": "Argon Prime"}
```

The server trims and caps every field, logs the event, answers `202` and dispatches in the background.

## Troubleshooting

**Nothing arrives.**
1. The server window (or its log) must show `event ship=...` for each order. Then do the same with `debuglog.txt`, which shows `FleetNotifier: <payload>` per event and any `[FleetNotifier]` Lua error. No `FleetNotifier:` line means the MD side did not fire — read on.
2. Check the extension list in game: both extensions enabled, no missing dependency warning.
3. Confirm Protected UI Mode is off; otherwise `djfhe_http` cannot load its DLLs.
4. `curl` the endpoint from the same machine (above), then compare with the Lua `ENDPOINT`.

**`401` in the server window / Lua logs "server returned 401".** `TOKEN` in `notifier.lua` and `X4_TOKEN` in `.env` differ.

**The mod fires for the ship I fly, but not for my fleet.** The roster cue rebuilds `$FleetShips` every 5 minutes and at load. A ship built in the last few minutes may need one refresh cycle; check the log for events after that.

**Way too many notifications.** Orders created by loop behaviours (AutoTrade, AutoMine, Patrol) finish constantly. The `not event.param.isdefault` check in `md/fleet_orders.xml` is what suppresses them — do not remove it. If a modded loop order still spams you, extend that check.

**`unknown order` in the notification.** The engine is allowed to drop `event.param` before the cue handles it. The code falls back to a placeholder instead of erroring.

**The server window closes instantly.** It only stays open while running; `start-x4-notifier.bat` adds the final pause. If it closes before that, the port is probably already in use by another copy.

## Building from source

Only needed to change the server. Requires Go 1.22+.

```
make test                  # go test ./... in go_server
make build                 # dev binary in dist/
make release               # bin/x4-notifier-windows-amd64.exe + bin/x4-notifier-linux-amd64
```

`make release` stamps the version from the git tag/commit; set `VERSION=1.0.0` to override (the shipped binaries say `1.0.0`, visible in `/health` and the startup log). `make mod-package` rebuilds `dist/fleet_notifier.zip`.

## Layout

```
fleet_notifier/              X4 extension (copy into extensions/)
  content.xml                id, version, dependency on djfhe_http
  ui.xml                     UI manifest, must live at the extension root
  ui/notifier.lua            menus-environment Lua, sends the HTTP request
  md/fleet_orders.xml        fleet roster + order-finished listener
bin/                         released server binaries, ready to run
  x4-notifier-windows-amd64.exe
  x4-notifier-linux-amd64
  start-x4-notifier.bat      Windows launcher, reads .env from the same folder
  .env.example               configuration template
go_server/
  main.go                    HTTP server, ntfy + Telegram dispatch
  dotenv.go                  .env loader
  *_test.go                  tests, providers behind httptest
  .env.example               same template, for running from source
scripts/build-release.sh     builds bin/ for Windows and Linux
scripts/package-mod.sh       builds dist/fleet_notifier.zip
docs/implementation.md       design notes and X4 modding gotchas
```

## Caveats

- The ntfy topic is only as private as the topic name; use a long random one or a token.
- The endpoint is plain HTTP on localhost by design. If you move the server to another host, put it behind TLS or a VPN — with no `X4_TOKEN` anyone on the LAN can trigger notifications.
- The mod is verified against the X4 9.00 modding docs, not yet in-game for every order type. The MD scripts are the part to re-check after a game update.
