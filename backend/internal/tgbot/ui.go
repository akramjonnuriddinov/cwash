package tgbot

import (
	"context"
	"html"
	"log"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/akramjon/cwash/backend/internal/store"
)

const (
	btnBook     = "📅 Book a wash"
	btnMyOrders = "🚗 My orders"
	btnContacts = "📍 Contacts"
	btnToday    = "📋 Today"
	btnUpcoming = "🗓 Upcoming"
	btnWalkin   = "➕ Walk-in car"
	btnCancel   = "✖️ Cancel"
	btnPhone    = "📱 Share phone number"
)

func mainMenu(admin bool) models.ReplyMarkup {
	rows := [][]models.KeyboardButton{}
	if admin {
		rows = append(rows,
			[]models.KeyboardButton{{Text: btnToday}, {Text: btnUpcoming}},
			[]models.KeyboardButton{{Text: btnWalkin}},
		)
	}
	rows = append(rows,
		[]models.KeyboardButton{{Text: btnBook}, {Text: btnMyOrders}},
		[]models.KeyboardButton{{Text: btnContacts}},
	)
	return &models.ReplyKeyboardMarkup{Keyboard: rows, ResizeKeyboard: true}
}

func phoneKeyboard() models.ReplyMarkup {
	return &models.ReplyKeyboardMarkup{
		Keyboard: [][]models.KeyboardButton{
			{{Text: btnPhone, RequestContact: true}},
			{{Text: btnCancel}},
		},
		ResizeKeyboard:  true,
		OneTimeKeyboard: true,
	}
}

func cancelKeyboard() models.ReplyMarkup {
	return &models.ReplyKeyboardMarkup{Keyboard: [][]models.KeyboardButton{{{Text: btnCancel}}}, ResizeKeyboard: true}
}

func ib(text, data string) models.InlineKeyboardButton {
	return models.InlineKeyboardButton{Text: text, CallbackData: data}
}

func inline(rows ...[]models.InlineKeyboardButton) models.ReplyMarkup {
	return &models.InlineKeyboardMarkup{InlineKeyboard: rows}
}

// noKeyboard removes inline buttons when editing a message.
func noKeyboard() models.ReplyMarkup {
	return &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{}}
}

// ---------- telegram helpers ----------

func (a *App) send(ctx context.Context, chatID int64, text string, kb models.ReplyMarkup) {
	_, err := a.b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: chatID, Text: text, ParseMode: models.ParseModeHTML, ReplyMarkup: kb,
	})
	if err != nil {
		log.Printf("send to %d: %v", chatID, err)
	}
}

// edit replaces the message the callback came from (falls back to a new message).
func (a *App) edit(ctx context.Context, cq *models.CallbackQuery, text string, kb models.ReplyMarkup) {
	msg := cq.Message.Message
	if msg == nil {
		a.send(ctx, cq.From.ID, text, kb)
		return
	}
	_, err := a.b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID: msg.Chat.ID, MessageID: msg.ID, Text: text, ParseMode: models.ParseModeHTML, ReplyMarkup: kb,
	})
	if err != nil && !strings.Contains(err.Error(), "message is not modified") {
		log.Printf("edit: %v", err)
	}
}

func (a *App) answer(ctx context.Context, cq *models.CallbackQuery, text string, alert bool) {
	_, _ = a.b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
		CallbackQueryID: cq.ID, Text: text, ShowAlert: alert,
	})
}

func (a *App) fail(ctx context.Context, chatID int64, err error) {
	log.Printf("error (chat %d): %v", chatID, err)
	a.send(ctx, chatID, "⚠️ Something went wrong, please try again.", nil)
}

func (a *App) notifyAdmins(ctx context.Context, text string, kb models.ReplyMarkup) {
	for id := range a.cfg.AdminIDs {
		a.send(ctx, id, text, kb)
	}
}

// ---------- formatting ----------

func esc(s string) string { return html.EscapeString(s) }

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func (a *App) local(t time.Time) time.Time { return t.In(a.cfg.Location) }

func (a *App) fmtTime(t time.Time) string { return a.local(t).Format("15:04") }

func (a *App) fmtDay(t time.Time) string {
	t = a.local(t)
	now := a.local(time.Now())
	y1, m1, d1 := t.Date()
	y2, m2, d2 := now.Date()
	switch {
	case y1 == y2 && m1 == m2 && d1 == d2:
		return "Today"
	case t.Sub(time.Date(y2, m2, d2, 0, 0, 0, 0, a.cfg.Location)) < 48*time.Hour && t.After(now):
		return "Tomorrow"
	}
	return t.Format("Mon, 02 Jan")
}

func (a *App) fmtPrice(p int) string {
	s := strconv.Itoa(p)
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
	}
	return b.String() + " " + a.cfg.Currency
}

func (a *App) serviceLabel(s store.Service) string {
	return s.Name + " · " + strconv.Itoa(s.DurationMin) + " min · " + a.fmtPrice(s.Price)
}

func (a *App) statusLine(o *store.Order) string {
	switch o.Status {
	case store.StatusBooked:
		return "📅 Booked for <b>" + a.fmtDay(o.ScheduledAt) + ", " + a.fmtTime(o.ScheduledAt) + "</b>"
	case store.StatusWaiting:
		return "🚗 At the wash, waiting in queue"
	case store.StatusWashing:
		if o.EtaAt == nil {
			return "🧽 Washing now"
		}
		left := time.Until(*o.EtaAt).Round(time.Minute)
		s := "🧽 Washing — ready at about <b>" + a.fmtTime(*o.EtaAt) + "</b>"
		if left > 0 {
			s += " (~" + itoa(int64(left.Minutes())) + " min)"
		}
		return s
	case store.StatusReady:
		return "✅ <b>Ready!</b> You can pick up your car"
	case store.StatusDone:
		return "🏁 Picked up"
	case store.StatusCancelled:
		return "❌ Cancelled"
	}
	return o.Status
}

func (a *App) orderCard(o *store.Order, forAdmin bool) string {
	var b strings.Builder
	b.WriteString("🚘 <b>" + esc(o.CarPlate) + "</b> · " + esc(o.ServiceName) + "\n")
	b.WriteString(a.statusLine(o) + "\n")
	if forAdmin {
		who := "walk-in, not linked"
		if o.ClientName != nil {
			who = esc(*o.ClientName)
		}
		if o.Phone != "" {
			who += " · +" + esc(o.Phone)
		}
		b.WriteString("👤 " + who + "\n")
	}
	b.WriteString("<i>#" + o.Code + "</i>")
	return b.String()
}

// ---------- input normalisation ----------

// normalizePlate uppercases and strips separators; "01 a 123 bc" → "01A123BC".
func normalizePlate(s string) (string, bool) {
	var b strings.Builder
	for _, r := range strings.ToUpper(s) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		case r == ' ' || r == '-':
		default:
			return "", false
		}
	}
	p := b.String()
	n := len([]rune(p))
	return p, n >= 4 && n <= 12
}

func digitsOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
