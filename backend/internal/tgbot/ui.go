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

func kbtn(l, key string) models.KeyboardButton { return models.KeyboardButton{Text: T(l, key)} }

func (a *App) mainMenu(l string, admin bool) models.ReplyMarkup {
	rows := [][]models.KeyboardButton{}
	if admin {
		rows = append(rows,
			[]models.KeyboardButton{kbtn(l, "btn_today"), kbtn(l, "btn_upcoming")},
			[]models.KeyboardButton{kbtn(l, "btn_walkin")},
		)
	}
	rows = append(rows,
		[]models.KeyboardButton{kbtn(l, "btn_book"), kbtn(l, "btn_orders")},
		[]models.KeyboardButton{kbtn(l, "btn_contacts"), kbtn(l, "btn_lang")},
	)
	return &models.ReplyKeyboardMarkup{Keyboard: rows, ResizeKeyboard: true}
}

func phoneKeyboard(l string) models.ReplyMarkup {
	return &models.ReplyKeyboardMarkup{
		Keyboard: [][]models.KeyboardButton{
			{{Text: T(l, "btn_phone"), RequestContact: true}},
			{kbtn(l, "btn_cancel")},
		},
		ResizeKeyboard:  true,
		OneTimeKeyboard: true,
	}
}

func cancelKeyboard(l string) models.ReplyMarkup {
	return &models.ReplyKeyboardMarkup{Keyboard: [][]models.KeyboardButton{{kbtn(l, "btn_cancel")}}, ResizeKeyboard: true}
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

func (a *App) fail(ctx context.Context, chatID int64, l string, err error) {
	log.Printf("error (chat %d): %v", chatID, err)
	a.send(ctx, chatID, T(l, "error"), nil)
}

// notifyAdmins sends each admin a message built in their own language.
func (a *App) notifyAdmins(ctx context.Context, build func(l string) (string, models.ReplyMarkup)) {
	for id := range a.cfg.AdminIDs {
		text, kb := build(a.langOf(ctx, id))
		a.send(ctx, id, text, kb)
	}
}

// ---------- formatting ----------

func esc(s string) string { return html.EscapeString(s) }

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func (a *App) local(t time.Time) time.Time { return t.In(a.cfg.Location) }

func (a *App) fmtTime(t time.Time) string { return a.local(t).Format("15:04") }

// fmtDay: "Today" / "Tomorrow" / "Mon, 03 Nov" in the given language.
func (a *App) fmtDay(l string, t time.Time) string {
	t = a.local(t)
	today := a.dayStart(time.Now())
	switch a.dayStart(t).Sub(today) {
	case 0:
		return T(l, "today")
	case 24 * time.Hour:
		return T(l, "tomorrow")
	}
	return fmtDate(l, t)
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

func (a *App) serviceLabel(l string, s store.Service) string {
	return T(l, "svc_label", s.Name.Get(l), s.DurationMin, a.fmtPrice(s.Price))
}

func (a *App) statusLine(l string, o *store.Order) string {
	switch o.Status {
	case store.StatusBooked:
		return T(l, "st_booked", a.fmtDay(l, o.ScheduledAt), a.fmtTime(o.ScheduledAt))
	case store.StatusWaiting:
		return T(l, "st_waiting")
	case store.StatusWashing:
		if o.EtaAt == nil {
			return T(l, "st_washing")
		}
		s := T(l, "st_washing_eta", a.fmtTime(*o.EtaAt))
		if left := time.Until(*o.EtaAt).Round(time.Minute); left > 0 {
			s += T(l, "st_left", int(left.Minutes()))
		}
		return s
	case store.StatusReady:
		return T(l, "st_ready")
	case store.StatusDone:
		return T(l, "st_done")
	case store.StatusCancelled:
		return T(l, "st_cancelled")
	}
	return o.Status
}

func (a *App) orderCard(l string, o *store.Order, forAdmin bool) string {
	var b strings.Builder
	b.WriteString("🚘 <b>" + esc(o.CarPlate) + "</b> · " + esc(o.ServiceName.Get(l)) + "\n")
	b.WriteString(a.statusLine(l, o) + "\n")
	if forAdmin {
		who := T(l, "unlinked")
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
