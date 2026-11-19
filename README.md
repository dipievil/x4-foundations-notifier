# x4-foundations-notifier

Sends a push notification to your phone (ntfy or Telegram) when a ship in your X4: Foundations fleet finishes an order.

```
X4 (Windows)                                   same machine
┌──────────────────────────────┐
│ md/fleet_orders.xml          │  raise_lua_event  ┌────────────────────┐
│ event_object_order_finished  │ ────────────────► │ ui/notifier.lua    │
└──────────────────────────────┘                   │ (menus env, x4_http)│
                                                   └─────────┬──────────┘
                                                             │ HTTP POST json
                                                   ┌─────────▼──────────┐
                                                   │ go_server          │
                                                   │ /x4/order-finished │
                                                   └─────────┬──────────┘
                                                             │
                                              ┌──────────────┴──────────────┐
                                        ntfy.sh                        Telegram Bot API
```

Nothing blocks the game loop: the Lua side fires an asynchronous HTTP request and the Go server answers immediately, before it talks to the providers.

## Requirements

- X4: Foundations, with **Protected UI Mode disabled** (Settings → Game Settings). `djfhe_http` loads native DLLs, so it cannot run in protected mode.
- [djfhe/x4_http](https://github.com/djfhe/x4_http) installed in `extensions/djfhe_http`. It is a hard dependency in `content.xml`; the extension refuses to load without it.
- Go 1.22+ to build the server (only needed to compile, not to run).
- An ntfy topic and/or a Telegram bot.

## Install the mod

1. Download/unpack [x4_http](https://github.com/djfhe/x4_http) into `X4 Foundations/extensions/djfhe_http`. If you downloaded the GitHub zip, rename the extracted folder to `djfhe_http` — the dependency resolves on the folder path.
2. Copy `fleet_notifier/` into `X4 Foundations/extensions/fleet_notifier`, or run `make mod-package` and extract `dist/fleet_notifier.zip` there.
3. Open `fleet_notifier/ui/notifier.lua` and set `ENDPOINT` (and `TOKEN` if you use one).
4. In the game: Settings → Extensions, enable **djfhe http** and **Fleet Order Notifier**.
5. Start X4 with `-debuglog` if you want to troubleshoot; the log is `Documents/Egosoft/X4/<id>/debuglog.txt`.

## Run the server

```
cd go_server
go build -o ../dist/x4-notifier .
X4_TOKEN=change-me NTFY_TOPIC=my_x4_topic ../dist/x4-notifier
```

Or with the Makefile from the repository root: `make build`, then run `dist/x4-notifier` with the environment set.

Configuration is environment only (no config file). `.env.example` lists everything:

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

A `202` means the notification was queued; `401` means the token does not match.

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
1. `debuglog.txt` must show `FleetNotifier: <payload>` for each event, then any `[FleetNotifier]` line from Lua on an HTTP error. No `FleetNotifier:` line means the MD side did not fire — read on.
2. Check the extension list in game: both extensions enabled, no missing dependency warning.
3. Confirm Protected UI Mode is off; otherwise `djfhe_http` cannot load its DLLs.
4. `curl` the endpoint from the same machine (above), then compare with the Lua `ENDPOINT`.

**The mod fires for the ship I fly, but not for my fleet.** The roster cue rebuilds `$FleetShips` every 5 minutes and at load. A ship that was built in the last few minutes may need one refresh cycle; check the log for `FleetNotifier` events after that.

**Way too many notifications.** Orders created by loop behaviours (AutoTrade, AutoMine, Patrol) finish constantly. The `not event.param.isdefault` check in `md/fleet_orders.xml` is what suppresses them — do not remove it. If a modded loop order still spams you, extend that check.

**`unknown order` in the notification.** The engine is allowed to drop `event.param` before the cue handles it. The code falls back to a placeholder instead of erroring.

## Layout

```
fleet_notifier/           X4 extension (copy into extensions/)
  content.xml             id, version, dependency on djfhe_http
  ui.xml                  UI manifest, must live at the extension root
  ui/notifier.lua         menus-environment Lua, sends the HTTP request
  md/fleet_orders.xml     fleet roster + order-finished listener
go_server/
  main.go                 HTTP server, ntfy + Telegram dispatch
  main_test.go            tests, providers behind httptest
  .env.example            all environment variables
scripts/package-mod.sh    builds dist/fleet_notifier.zip
docs/implementation.md    design notes and X4 modding gotchas
```

## Caveats

- The ntfy topic is only as private as the topic name; use a long random one or a token.
- The endpoint is plain HTTP on localhost by design. If you move the server to another host, put it behind TLS or a VPN — with no `X4_TOKEN` anyone on the LAN can trigger notifications.
- The mod is verified against the X4 9.00 modding docs, not yet in-game for every order type. The MD scripts are the part to re-check after a game update.
