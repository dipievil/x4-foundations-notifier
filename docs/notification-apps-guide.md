# Setting up notifications (no coding required)

The server can deliver to **ntfy**, **Gotify** or **Telegram**. Pick one to start
with — you can enable more later, they all work at the same time.

| App | Account needed | Where it runs | Effort |
| --- | --- | --- | --- |
| [ntfy](#ntfy-easiest) | no | ntfy.sh (public) or your own machine | 5 minutes |
| [Gotify](#gotify-self-hosted) | no | your own machine | 20 minutes |
| [Telegram](#telegram) | yes (you probably have one) | Telegram | 5 minutes |

## Before you start

1. Copy `bin/.env.example` to `bin/.env`.
2. Fill in the values for the app you chose (each section below says which).
3. Start the server with `bin/start-x4-notifier.bat` (Windows) or
   `./bin/x4-notifier-linux-amd64` (Linux). It reads `.env` from its own folder,
   so keep them together.
4. Settings are read once at startup — after editing `.env`, stop the server
   (close the window) and start it again.

Only the values you fill in matter; leave the others empty.

## ntfy (easiest)

No account, no server. A "topic" is a channel name that you and your phone share.

1. Install the app on your phone: [ntfy.sh](https://ntfy.sh) → pick Android, iOS or
   the web app ([phone setup guide](https://docs.ntfy.sh/subscribe/phone/)).
2. Choose a topic name and treat it as a password. Anyone who knows it can read
   your messages, so make it long and random: `x4-fleet-7fk2n9qz`.
3. In the app, tap **+** and subscribe to that exact topic.
4. In `bin/.env`:

   ```ini
   NTFY_TOPIC=x4-fleet-7fk2n9qz
   ```

5. Check it: open `https://ntfy.sh/x4-fleet-7fk2n9qz` in a browser — the phone
   should buzz with a test message.

Useful links: [ntfy documentation](https://docs.ntfy.sh/) ·
[publishing messages](https://docs.ntfy.sh/publish/) ·
[self-hosting ntfy](https://docs.ntfy.sh/install/) ·
[server configuration](https://docs.ntfy.sh/config/)

If you self-host ntfy, also set `NTFY_URL=https://ntfy.mydomain.local` and, if the
topic is protected, `NTFY_TOKEN=<access token>`.

## Gotify (self-hosted)

Gotify is a small server you run yourself, which keeps every message on your own
network. You need a machine that is always on — a NAS, a Raspberry Pi, or the PC
running X4.

1. Install the server, following the [installation guide](https://gotify.net/docs/install).
   Docker is the shortest route:

   ```bash
   docker run -d --name gotify -p 8070:80 \
     -v /var/gotify:/app/data \
     -e GOTIFY_DEFAULTUSER_PASS=change-this \
     gotify/server
   ```

2. Open `http://<server-ip>:8070` in a browser and log in as `admin` with the
   password you set above (default is `admin` — change it,
   see [first login](https://gotify.net/docs/first-login)).
3. Go to **Apps → Create Application**, name it `x4`, and copy the token it shows.
   That is the token the game server uses to send messages.
4. Install the phone app ([Android](https://github.com/gotify/android), also on
   F-Droid) and add your server, logging in with your user account.
5. In `bin/.env`:

   ```ini
   GOTIFY_URL=http://192.168.0.50:8070
   GOTIFY_TOKEN=AbCdEfGhIjKlMnO
   GOTIFY_PRIORITY=5
   ```

6. Check it: open `http://192.168.0.50:8070` → the `x4` app page has a
   "send test message" button.

Useful links: [Gotify documentation](https://gotify.net/docs/) ·
[message API](https://gotify.net/docs/pushmsg) ·
[priority levels](https://gotify.net/docs/priority) · [API reference](https://gotify.net/api-docs)

Notes: `GOTIFY_URL` must be reachable **from the PC running X4** — use the LAN IP,
not `localhost`, unless Gotify runs on that same PC. The token must be an
*application* token, not your login password.

## Telegram

Messages arrive as a chat with a bot that you own. A bot cannot start a
conversation, so you message it first.

1. In Telegram, open [@BotFather](https://core.telegram.org/bots/features) and send
   `/newbot`. Pick a name and a username, and copy the token it replies with
   (looks like `123456789:AAE...`).
2. Open your new bot and send it `/start` — from now on it may message you.
3. Find your chat id. Two ways:
   - message the bot, then open
     `https://api.telegram.org/bot<YOUR_TOKEN>/getUpdates` and read
     `result[0].message.chat.id` (see [getting updates](https://core.telegram.org/bots/api#getting-updates));
   - or message [@userinfobot](https://t.me/userinfobot), which replies with your id.
4. In `bin/.env`:

   ```ini
   TELEGRAM_BOT_TOKEN=123456789:AAE-your-token-here
   TELEGRAM_CHAT_ID=987654321
   ```

5. Check it: open
   `https://api.telegram.org/bot<YOUR_TOKEN>/sendMessage?chat_id=<YOUR_ID>&text=test`
   in a browser — the bot should message you.

Useful links: [BotFather and bot basics](https://core.telegram.org/bots/features) ·
[Bot API](https://core.telegram.org/bots/api) ·
[sendMessage](https://core.telegram.org/bots/api#sendmessage)

Notes: for a group, add the bot to the group first; group chat ids are negative
(`-100...`). If you revoke the token in BotFather, update `.env`.

## Checking that it works

Start the server and look at its window:

```
configuration loaded from .env
x4-notifier 1.1.0
listening on http://127.0.0.1:47100 (ntfy=false gotify=true telegram=false auth=true)
```

`true` marks the apps it will actually use. Then:

```bash
curl http://127.0.0.1:47100/health
curl -X POST http://127.0.0.1:47100/x4/order-finished \
  -H "Authorization: Bearer <the X4_TOKEN from .env>" \
  -H "Content-Type: application/json" \
  -d '{"ship":"Test Ship","order":"FlyTo","sector":"Argon Prime"}'
```

A `202` answer means "queued". The phone should buzz within a second.

## When nothing arrives

| Symptom | Likely cause |
| --- | --- |
| `/health` shows `false` for your app | the variable is missing, empty, or has spaces; for Gotify both `GOTIFY_URL` and `GOTIFY_TOKEN` are required. Restart the server after editing `.env`. |
| Server log: `ntfy: status 404` | wrong topic name, or wrong `NTFY_URL`. |
| Server log: `gotify: status 401` | wrong token, or a login password was used instead of an application token. |
| Server log: `gotify: ... connection refused` | `GOTIFY_URL` is not reachable from the PC running X4. |
| Server log: `telegram: status 400` | wrong `TELEGRAM_CHAT_ID`, or you never sent `/start` to the bot. |
| Phone silent but server log has no error | the app is not subscribed to that topic (ntfy) or not logged into that server (Gotify); check battery optimisation on the phone. |

The server prints the exact provider error after `ntfy:`, `gotify:` or
`telegram:`, which is usually enough to fix the value.
