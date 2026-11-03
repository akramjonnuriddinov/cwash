// Package tgbot is the Telegram interface for clients and washers (admins).
package tgbot

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/akramjon/cwash/backend/internal/config"
	"github.com/akramjon/cwash/backend/internal/store"
)

// conversation steps that wait for free-text input
const (
	stepPhoneForBooking = "phone_for_booking"
	stepBookPlate       = "book_plate"
	stepWalkinPlate     = "walkin_plate"
	stepWalkinService   = "walkin_service"
	stepWalkinPhone     = "walkin_phone"
)

type state struct {
	step      string
	serviceID int
	slot      time.Time
	plate     string
}

type App struct {
	b        *bot.Bot
	st       *store.Store
	cfg      *config.Config
	username string

	mu     sync.Mutex
	states map[int64]*state // in-memory: a restart only drops half-finished dialogs
}

func New(ctx context.Context, cfg *config.Config, st *store.Store) (*App, error) {
	a := &App{st: st, cfg: cfg, states: map[int64]*state{}}
	b, err := bot.New(cfg.BotToken,
		bot.WithDefaultHandler(a.handle),
		bot.WithWebhookSecretToken(webhookSecret(cfg.BotToken)))
	if err != nil {
		return nil, err
	}
	me, err := b.GetMe(ctx)
	if err != nil {
		return nil, err
	}
	a.b, a.username = b, me.Username
	return a, nil
}

func (a *App) Username() string { return a.username }

const webhookPath = "/telegram/webhook"

// webhookSecret is derived from the token so no extra env var is needed;
// Telegram sends it back in a header, proving the request came from Telegram.
func webhookSecret(token string) string {
	sum := sha256.Sum256([]byte("cwash-webhook:" + token))
	return hex.EncodeToString(sum[:])[:48]
}

// secretGuard rejects requests that don't carry Telegram's secret header.
func secretGuard(secret string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		got := r.Header.Get("X-Telegram-Bot-Api-Secret-Token")
		if subtle.ConstantTimeCompare([]byte(got), []byte(secret)) != 1 {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

// Run blocks until ctx is done. With WEBHOOK_URL set, Telegram pushes updates
// to mux (which also wakes a hibernated app); otherwise it long-polls.
func (a *App) Run(ctx context.Context, mux *http.ServeMux) error {
	a.setCommands(ctx)
	go a.reminderLoop(ctx)

	if a.cfg.WebhookURL == "" {
		info, err := a.b.GetWebhookInfo(ctx)
		if err != nil {
			return err
		}
		if info.URL != "" {
			// Polling would fail anyway; deleting the webhook here would silently
			// break production if a developer runs locally with the prod token.
			return fmt.Errorf("a webhook is active for this bot (%s): this token is used in webhook mode elsewhere. "+
				"Use a separate bot token for local development, or set WEBHOOK_URL", info.URL)
		}
		log.Printf("mode: long polling")
		a.b.Start(ctx)
		return nil
	}

	secret := webhookSecret(a.cfg.BotToken)
	handler := a.b.WebhookHandler()
	mux.HandleFunc("POST "+webhookPath, secretGuard(secret, handler))
	url := a.cfg.WebhookURL + webhookPath
	if _, err := a.b.SetWebhook(ctx, &bot.SetWebhookParams{
		URL:            url,
		SecretToken:    secret,
		AllowedUpdates: []string{"message", "callback_query"},
	}); err != nil {
		return fmt.Errorf("setWebhook: %w", err)
	}
	log.Printf("mode: webhook %s", url)
	a.b.StartWebhook(ctx)
	return nil
}

// setCommands registers the "/" menu per app language; Uzbek is the default
// for every other language.
func (a *App) setCommands(ctx context.Context) {
	for _, code := range []string{"", langUz, langRu, langEn} {
		l := code
		if l == "" {
			l = defaultLang
		}
		_, err := a.b.SetMyCommands(ctx, &bot.SetMyCommandsParams{
			LanguageCode: code,
			Commands: []models.BotCommand{
				{Command: "start", Description: T(l, "cmd_start")},
				{Command: "orders", Description: T(l, "cmd_orders")},
				{Command: "book", Description: T(l, "cmd_book")},
				{Command: "lang", Description: T(l, "cmd_lang")},
				{Command: "cancel", Description: T(l, "cmd_cancel")},
				{Command: "myid", Description: T(l, "cmd_myid")},
			},
		})
		if err != nil {
			log.Printf("setMyCommands(%q): %v", code, err)
		}
	}
}

func (a *App) handle(ctx context.Context, _ *bot.Bot, u *models.Update) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("panic: %v\n%s", r, debug.Stack())
		}
	}()
	switch {
	case u.CallbackQuery != nil:
		a.onCallback(ctx, u.CallbackQuery)
	case u.Message != nil && u.Message.Chat.Type == models.ChatTypePrivate && u.Message.From != nil:
		a.onMessage(ctx, u.Message)
	}
}

func (a *App) isAdmin(tgID int64) bool { return a.cfg.AdminIDs[tgID] }

func (a *App) user(ctx context.Context, from *models.User) (*store.User, error) {
	name := strings.TrimSpace(from.FirstName + " " + from.LastName)
	return a.st.UpsertUser(ctx, from.ID, name, resolveLang(from.LanguageCode))
}

// langOf returns the stored language of any Telegram user (for messages we
// send on our own initiative, e.g. notifications to admins).
func (a *App) langOf(ctx context.Context, tgID int64) string {
	if l := a.st.LangByTelegramID(ctx, tgID); isLang(l) {
		return l
	}
	return defaultLang
}

func clientLang(o *store.Order) string {
	if o.ClientLang != nil && isLang(*o.ClientLang) {
		return *o.ClientLang
	}
	return defaultLang
}

// ---------- state ----------

func (a *App) getState(id int64) *state {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.states[id]
}

func (a *App) setState(id int64, s *state) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.states[id] = s
}

func (a *App) clearState(id int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.states, id)
}

// ---------- routing ----------

func (a *App) onMessage(ctx context.Context, m *models.Message) {
	chatID := m.Chat.ID
	u, err := a.user(ctx, m.From)
	if err != nil {
		a.fail(ctx, chatID, defaultLang, err)
		return
	}
	l := u.Lang
	admin := a.isAdmin(m.From.ID)

	if m.Contact != nil {
		a.onContact(ctx, m, u, admin)
		return
	}

	text := strings.TrimSpace(m.Text)
	cmd, arg, _ := strings.Cut(text, " ")
	btn := buttonKey(text) // menu buttons contain spaces, so match the full text

	switch {
	case cmd == "/start":
		a.clearState(m.From.ID)
		a.onStart(ctx, chatID, u, strings.TrimSpace(arg), admin)
		return
	case cmd == "/cancel" || btn == "btn_cancel":
		a.clearState(m.From.ID)
		a.send(ctx, chatID, T(l, "cancelled"), a.mainMenu(l, admin))
		return
	case cmd == "/myid":
		a.send(ctx, chatID, T(l, "myid", m.From.ID), nil)
		return
	case cmd == "/lang" || btn == "btn_lang":
		a.clearState(m.From.ID)
		a.showLangPicker(ctx, chatID, l)
		return
	}

	// Tapping a menu button mid-dialog abandons the dialog instead of being
	// treated as input (e.g. "📋 Today" is not a plate number).
	if btn != "" {
		a.clearState(m.From.ID)
	}

	if s := a.getState(m.From.ID); s != nil {
		switch s.step {
		case stepBookPlate:
			a.onBookingPlate(ctx, chatID, u, text)
			return
		case stepWalkinPlate, stepWalkinPhone:
			if admin {
				a.onWalkinText(ctx, chatID, u, s, text)
				return
			}
		}
		// any other text abandons the half-finished dialog and is handled below
		a.clearState(m.From.ID)
	}

	switch {
	case btn == "btn_book" || cmd == "/book":
		a.startBooking(ctx, chatID, u)
	case btn == "btn_orders" || cmd == "/orders":
		a.showMyOrders(ctx, chatID, u)
	case btn == "btn_contacts":
		a.showContacts(ctx, chatID, l, admin)
	case btn == "btn_today" && admin:
		a.adminToday(ctx, chatID, l)
	case btn == "btn_upcoming" && admin:
		a.adminUpcoming(ctx, chatID, l)
	case btn == "btn_walkin" && admin:
		a.adminWalkinStart(ctx, chatID, u)
	default:
		a.send(ctx, chatID, T(l, "choose_menu"), a.mainMenu(l, admin))
	}
}

func (a *App) onCallback(ctx context.Context, cq *models.CallbackQuery) {
	u, err := a.user(ctx, &cq.From)
	if err != nil {
		a.answer(ctx, cq, "", false)
		a.fail(ctx, cq.From.ID, defaultLang, err)
		return
	}
	kind, rest, _ := strings.Cut(cq.Data, ":")
	switch kind {
	case "lang":
		a.onLangChosen(ctx, cq, u, rest)
	case "a", "ws", "wskip":
		if !a.isAdmin(cq.From.ID) {
			a.answer(ctx, cq, T(u.Lang, "admins_only"), true)
			return
		}
		a.onAdminCallback(ctx, cq, u, kind, rest)
	default:
		a.onClientCallback(ctx, cq, u, kind, rest)
	}
}

// ---------- language ----------

func (a *App) showLangPicker(ctx context.Context, chatID int64, l string) {
	var row []models.InlineKeyboardButton
	for _, code := range langs {
		row = append(row, ib(langNames[code], "lang:"+code))
	}
	a.send(ctx, chatID, T(l, "choose_lang"), inline(row))
}

func (a *App) onLangChosen(ctx context.Context, cq *models.CallbackQuery, u *store.User, l string) {
	if !isLang(l) {
		a.answer(ctx, cq, "", false)
		return
	}
	if err := a.st.SetLang(ctx, u.ID, l); err != nil {
		a.answer(ctx, cq, "", false)
		a.fail(ctx, cq.From.ID, u.Lang, err)
		return
	}
	a.answer(ctx, cq, "", false)
	a.edit(ctx, cq, T(l, "lang_set"), noKeyboard())
	// a new message is needed to replace the reply keyboard with translated buttons
	a.send(ctx, cq.From.ID, T(l, "choose_menu"), a.mainMenu(l, a.isAdmin(cq.From.ID)))
}

// ---------- reminders ----------

func (a *App) reminderLoop(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		a.sendReminders(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (a *App) sendReminders(ctx context.Context) {
	due, err := a.st.DueReminders(ctx, time.Now().Add(time.Hour))
	if err != nil {
		log.Printf("reminders: %v", err)
		return
	}
	for _, o := range due {
		if o.ClientTelegramID != nil {
			l := clientLang(o)
			mins := int(time.Until(o.ScheduledAt).Round(time.Minute).Minutes())
			text := T(l, "reminder", a.fmtTime(o.ScheduledAt), mins, esc(o.CarPlate), esc(o.ServiceName.Get(l)))
			if a.cfg.Address != "" {
				text += "\n📍 " + esc(a.cfg.Address)
			}
			a.send(ctx, *o.ClientTelegramID, text, nil)
		}
		if err := a.st.MarkReminded(ctx, o.ID); err != nil {
			log.Printf("mark reminded %d: %v", o.ID, err)
		}
	}
}
