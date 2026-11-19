# Design notes

Why the mod is built this way, and the X4 modding rules that forced it. Checked
against the X4 9.00 modding documentation (2026-11); the in-game behaviour of the
Mission Director side still needs a test pass per game update.

## Pipeline

```
FleetNotifier_Watch            (MD, every 3 s)
        │  find_ship: every player ship
        │  compare ship.order with the previous sample
        ▼
raise_lua_event "FleetNotifier.OrderFinished"  (one string)
        │  "shipcode|shipname|ordername|sectorname"
        ▼
RegisterEvent handler in ui/notifier.lua  (menus Lua environment)
        │  x4_http: async POST, JSON body
        ▼
go_server /x4/order-finished   (auth, validation, 202 at once)
        ▼
ntfy + Gotify + Telegram, in the background
```

## Why `ui.xml` sits at the extension root

An extension's UI manifest is `extensions/<id>/ui.xml`, not `ui/ui.xml`, and the
`<file name>` path is relative to the extension root:

```xml
<addon name="fleet_notifier" ...>
  <environment type="menus">
    <file name="ui/notifier.lua" />
  </environment>
</addon>
```

`menus` is the only environment that accepts a `<file>`. The HUD runs in a
separate `core` environment with no way to register a file. A `<file>` that is
not listed here never loads, and a lua file that never loads registers no
`RegisterEvent` handler — the failure is silent, so this is the first thing to
check when nothing arrives.

## Why the payload is a delimited string

`raise_lua_event` carries exactly one value: a string, number or component. There
is no table channel from scripts to Lua. The alternatives are the entity
blackboard (`SetNPCBlackboard` / `GetNPCBlackboard`) for structured payloads, or
packing the fields into one string. One ship name, one order and one sector do
not justify the blackboard, so the fields are packed with `|`, a character that
does not appear in ship or sector names.

The same reason explains the Lua split function instead of a JSON string built
in MD: the Lua side builds a table and lets `x4_http` encode it, which removes
every escaping bug at once.

## Why the order is polled instead of listened to

`event_object_order_finished` exists, but it needs a source: `object` (one
object) or `group` (an MD group). Either one has to exist when the cue sets the
event up.

A group is built by a cue's actions, and cue state lives in the savegame. Once
that cue has completed, its actions do not run again when the save is loaded,
and the group is not there any more — the listener then subscribes to nothing,
silently. The first version rebuilt `$FleetShips` that way and stopped
reporting after one save/load cycle; the debuglog showed
`Property lookup failed: $FleetShips` and `Evaluated value 'null' is not of
type group`.

Polling reads live ship state instead: `find_ship ... owner="faction.player"`
every 3 s, then `ship.order` for each ship. Nothing in the mechanism depends on
what the savegame remembers, so it survives loads, new ships and script
updates.

## What counts as a finished order

The cue keeps the last order id, name and temporary flag per ship and reports
when the ship goes back to idle (no order) or to its default order.
`ship.order` returns the queued order while one runs and the default order once
the queue is empty, so that transition is the reliable "the order ended"
moment.

What is skipped, to keep the phone quiet:

- the previous order was the default order (AutoTrade, Patrol...): those loop
  forever and would otherwise flood the phone;
- the previous order was temporary (the engine's own Undock and similar
  orders that appear for a moment when a new order starts);
- a queued follow-up order means the finished one was not the last, so only
  the last order of a queue is reported.

## Two MD traps this design avoids

- **Table keys are not arbitrary strings.** A string key has to start with `$`
  (`table[$foo = 1]` or `table[{'$foo'} = 1]`). Keying by idcode
  (`$Seen.{$idcode}`) fails at runtime: writes error out and reads return
  null, which also looks like "no previous order" and silently disables the
  whole comparison. Keying by the ship component (`$Seen.{$Ship}`) is what
  vanilla scripts do, for example `$AttackingShipMarineTierAmounts.{$ship}`.
- **`null != null` is true.** An idle ship can report a null order id or name.
  Comparing null with null makes "did the order change?" true on every pass,
  which produced one notification every 3 s. Every stored value is a string or
  a boolean, never null.

## Protected UI Mode

`djfhe_http` loads `socket.dll`, `mime.dll` and `ssl.dll` from the extension
folder. X4 only allows that with Protected UI Mode off; with it on, the library
cannot initialise and the Lua side logs a load failure.

The Lua file is defensive about this: the `require` is wrapped in `pcall`, so a
missing or disabled `djfhe_http` logs one line and leaves the rest of the UI
intact instead of throwing at load time.

## Server contract

Configuration is read from `.env` in the working directory, then from the
environment (a shell export wins), so a shipped binary is configured by editing
one file. The default listener is `127.0.0.1:47100`; the port avoids `8080`,
which belongs to every web server and dev tool on a normal machine. The binary is built with `scripts/build-release.sh`, which stamps the
version into `main.version` (`-X main.version=...`) so `/health` and the startup
log name the build.

`POST /x4/order-finished`

```json
{"code": "ABC-123", "ship": "Argon Titan", "order": "DockAt", "sector": "Argon Prime"}
```

- `Authorization: Bearer <X4_TOKEN>` (or `X-Auth-Token`) when the token is set.
- Body capped at 8 KiB, every field trimmed, newlines folded, 160 runes max.
- `ship` is required; the other three may be empty.
- `202 Accepted` on success: dispatch happens in a goroutine so the game never
  waits on a provider. Provider failures are logged, never returned.
- `GET /health` reports which providers and auth are configured, no secrets.

Telegram is sent without `parse_mode` on purpose: ship names contain `_` and `*`,
and both Markdown flavors reject them with a 400. ntfy is published with a JSON
body rather than headers, which keeps the accented title (`Ordem concluída`)
intact. Gotify takes a JSON body too (`title`, `message`, `priority`) and the
application token as `X-Gotify-Key`, not the `?token=` query parameter, so the
secret never lands in a URL or a proxy log. Providers are independent: each one
that is configured is attempted, and a failure is logged without affecting the
others.

## References

- [Mission Director Guide](https://wiki.egosoft.com/X%20Rebirth%20Wiki/Modding%20support/Mission%20Director%20Guide) — expressions, lists, tables and the table-key rules, variables and namespaces
- [event_object_order_finished](https://x4wiki.com/en/pages/api-mdevent-event-object-order-finished-6bcd7797) — the event this design replaced, and why it needs a source
- [groupeventsource](https://x4wiki.com/en/pages/schema-groupeventsource-6492ab7b) — group source semantics
- [Talking with MD and AI scripts](https://chemodun.github.io/x4/modding-support/ui-modding/talking-with-md-and-ai-scripts) — `raise_lua_event`, `RegisterEvent`, what survives the crossing
- [Anatomy of an extension](https://chemodun.github.io/x4/modding-support/anatomy-of-an-extension/) — `content.xml`, `ui.xml`, dependencies, packaging
- [Mission Director cues](https://x4wiki.com/en/pages/md-cues.html) — `checkinterval`, instantiated listeners
- [djfhe/x4_http](https://github.com/djfhe/x4_http) — `Request.new(...):setUrl():setBody():send()`
