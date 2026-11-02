# CWash backend — Telegram bot

## Run locally
```bash
cp .env.example .env          # put BOT_TOKEN, then ADMIN_IDS (send /myid to the bot)
docker compose up -d          # Postgres on localhost:5434 (compose project "cwash")
go run ./cmd/bot              # migrations run automatically on start
go test ./...
```
Only ONE instance may poll a token at a time (Telegram returns "Conflict" otherwise).

## Languages
Uzbek, Russian, English. A user's language follows their Telegram app language
(anything other than uz/ru/en → Uzbek) until they pick one with 🌐 / `/lang`.
All texts live in `internal/tgbot/i18n.go`; `go test` checks every key has all 3
translations with matching placeholders. Service names: `services.name` (en),
`name_uz`, `name_ru`.

## Order lifecycle
`booked → waiting (arrived) → washing (ETA = now + service duration, "+10 min" extends) → ready → done`
`booked/waiting → cancelled`. Every change is pushed to the client in Telegram.

## Client
- 📅 Book: service → day → free time (capacity = BOXES, step = SLOT_MINUTES) → plate → confirm. Reminder 1h before.
- 🚗 My orders: live status + ETA, cancel booking.
- Walk-in client: scans the washer's QR (`t.me/<bot>?start=<CODE>`) → car linked, gets notifications.
  Or, if the washer typed their phone and they've shared it with the bot before, it links automatically.

## Admin (Telegram IDs in ADMIN_IDS)
- 📋 Today — cards with status buttons. 🗓 Upcoming — bookings after today.
- ➕ Walk-in car — plate → service → phone/skip → QR to show the client.

## Layout
```
cmd/bot            entrypoint
internal/config    env config
internal/store     Postgres (pgx) + embedded SQL migrations
internal/slots     free-slot calculation (pure, tested)
internal/tgbot     Telegram handlers: client.go, admin.go, ui.go
```
