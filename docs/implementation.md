# Design notes

Why the mod is built this way, and the X4 modding rules that forced it. Checked
against the X4 9.00 modding documentation (2026-11); the in-game behaviour of the
Mission Director side still needs a test pass per game update.

## Pipeline

```
event_object_order_finished   (MD, per ship in $FleetShips)
        │  filters: owner == faction.player, not event.param.isdefault
        ▼
raise_lua_event "FleetNotifier.OrderFinished"  (one string)
        │  "shipcode|shipname|ordername|sectorname"
        ▼
RegisterEvent handler in ui/notifier.lua  (menus Lua environment)
        │  x4_http: async POST, JSON body
        ▼
go_server /x4/order-finished   (auth, validation, 202 at once)
        ▼
ntfy + Telegram, in the background
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

## Why the event needs a group source

`event_object_order_finished` takes a source filter: `object` (one object),
`group` (an MD group, membership may change) or `order` (order definition). A cue
with no source has nothing to subscribe to.

The tempting filter, `object="player.ship"`, means the ship the player is
piloting, so a fleet would never trigger. Instead `FleetNotifier_TrackFleet`
rebuilds `$FleetShips` with `find_object ... space="player.galaxy"
class="class.ship" owner="faction.player" multiple="true" groupname="$FleetShips"`
every 5 minutes, and the listener subscribes with `group="$FleetShips"`.

## Why loop orders are filtered

`event.param` is the finished order. Orders from loop behaviours — AutoTrade,
AutoMine, Patrol, Escort — finish every cycle, so `not event.param.isdefault`
is what separates "a command I gave" from "the loop turned over again". Removing
that check turns the notifier into a firehose.

## `event.param` lifetime

The event documentation states that the finished order "may no longer exist when
handled". Reading `event.param.name` can therefore yield null, so the action
blocks set a placeholder first and only overwrite it inside
`<do_if value="event.param">`. The notification then says `Unknown order`
instead of failing the cue.

## Protected UI Mode

`djfhe_http` loads `socket.dll`, `mime.dll` and `ssl.dll` from the extension
folder. X4 only allows that with Protected UI Mode off; with it on, the library
cannot initialise and the Lua side logs a load failure.

The Lua file is defensive about this: the `require` is wrapped in `pcall`, so a
missing or disabled `djfhe_http` logs one line and leaves the rest of the UI
intact instead of throwing at load time.

## Server contract

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
intact.

## References

- [event_object_order_finished](https://x4wiki.com/en/pages/api-mdevent-event-object-order-finished-6bcd7797) — event fields and subscription filters
- [groupeventsource](https://x4wiki.com/en/pages/schema-groupeventsource-6492ab7b) — group source semantics
- [Talking with MD and AI scripts](https://chemodun.github.io/x4/modding-support/ui-modding/talking-with-md-and-ai-scripts) — `raise_lua_event`, `RegisterEvent`, what survives the crossing
- [Anatomy of an extension](https://chemodun.github.io/x4/modding-support/anatomy-of-an-extension/) — `content.xml`, `ui.xml`, dependencies, packaging
- [Mission Director cues](https://x4wiki.com/en/pages/md-cues.html) — `checkinterval`, instantiated listeners
- [djfhe/x4_http](https://github.com/djfhe/x4_http) — `Request.new(...):setUrl():setBody():send()`
