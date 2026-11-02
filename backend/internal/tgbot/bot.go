// Package tgbot is the Telegram interface for clients and washers (admins).
package tgbot

import (
	"context"
	"log"
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
	b, err := bot.New(cfg.BotToken, bot.WithDefaultHandler(a.handle))
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

func (a *App) Run(ctx context.Context) {
	a.setCommands(ctx)
	go a.reminderLoop(ctx)
	a.b.Start(ctx) // long polling, blocks until ctx is done
}

func (a *App) setCommands(ctx context.Context) {
	_, err := a.b.SetMyCommands(ctx, &bot.SetMyCommandsParams{Commands: []models.BotCommand{
		{Command: "start", Description: "Main menu"},
		{Command: "orders", Description: "My cars & bookings"},
		{Command: "book", Description: "Book a wash"},
		{Command: "cancel", Description: "Cancel current action"},
		{Command: "myid", Description: "Show my Telegram ID"},
	}})
	if err != nil {
		log.Printf("setMyCommands: %v", err)
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
	return a.st.UpsertUser(ctx, from.ID, name)
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
		a.fail(ctx, chatID, err)
		return
	}
	admin := a.isAdmin(m.From.ID)

	if m.Contact != nil {
		a.onContact(ctx, m, u, admin)
		return
	}

	text := strings.TrimSpace(m.Text)
	cmd, arg, _ := strings.Cut(text, " ")
	switch cmd {
	case "/start":
		a.clearState(m.From.ID)
		a.onStart(ctx, chatID, u, strings.TrimSpace(arg), admin)
		return
	case "/cancel", btnCancel:
		a.clearState(m.From.ID)
		a.send(ctx, chatID, "OK, cancelled.", mainMenu(admin))
		return
	case "/myid":
		a.send(ctx, chatID, "Your Telegram ID: <code>"+itoa(m.From.ID)+"</code>", nil)
		return
	}

	if s := a.getState(m.From.ID); s != nil {
		switch s.step {
		case stepBookPlate:
			a.onBookingPlate(ctx, chatID, m.From.ID, text)
			return
		case stepWalkinPlate, stepWalkinPhone:
			if admin {
				a.onWalkinText(ctx, chatID, m.From.ID, s, text)
				return
			}
		}
		// any other text abandons the half-finished dialog and is handled below
		a.clearState(m.From.ID)
	}

	switch text {
	case btnBook, "/book":
		a.startBooking(ctx, chatID, u)
	case btnMyOrders, "/orders":
		a.showMyOrders(ctx, chatID, u)
	case btnContacts:
		a.showContacts(ctx, chatID, admin)
	case btnToday:
		if admin {
			a.adminToday(ctx, chatID)
			return
		}
		a.send(ctx, chatID, "Choose an option from the menu 👇", mainMenu(admin))
	case btnUpcoming:
		if admin {
			a.adminUpcoming(ctx, chatID)
			return
		}
		a.send(ctx, chatID, "Choose an option from the menu 👇", mainMenu(admin))
	case btnWalkin:
		if admin {
			a.adminWalkinStart(ctx, chatID, m.From.ID)
			return
		}
		a.send(ctx, chatID, "Choose an option from the menu 👇", mainMenu(admin))
	default:
		a.send(ctx, chatID, "Choose an option from the menu 👇", mainMenu(admin))
	}
}

func (a *App) onCallback(ctx context.Context, cq *models.CallbackQuery) {
	data := cq.Data
	kind, rest, _ := strings.Cut(data, ":")
	switch kind {
	case "a", "ws", "wskip":
		if !a.isAdmin(cq.From.ID) {
			a.answer(ctx, cq, "Admins only", true)
			return
		}
		a.onAdminCallback(ctx, cq, kind, rest)
	default:
		a.onClientCallback(ctx, cq, kind, rest)
	}
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
			mins := int(time.Until(o.ScheduledAt).Round(time.Minute).Minutes())
			text := "⏰ <b>Reminder</b>\nYour wash starts at <b>" + a.fmtTime(o.ScheduledAt) + "</b> (in ~" + itoa(int64(mins)) + " min).\n" +
				"🚘 " + esc(o.CarPlate) + " · " + esc(o.ServiceName)
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
