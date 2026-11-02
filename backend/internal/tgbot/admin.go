package tgbot

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	qrcode "github.com/skip2/go-qrcode"

	"github.com/akramjon/cwash/backend/internal/store"
)

func adminOrderKB(l string, o *store.Order) models.ReplyMarkup {
	id := itoa(o.ID)
	b := func(key, act string) models.InlineKeyboardButton { return ib(T(l, key), "a:"+act+":"+id) }
	switch o.Status {
	case store.StatusBooked:
		return inline(
			[]models.InlineKeyboardButton{b("btn_arrived", "arrive"), b("btn_start", "start")},
			[]models.InlineKeyboardButton{b("btn_cancel_order", "cancel")},
		)
	case store.StatusWaiting:
		return inline(
			[]models.InlineKeyboardButton{b("btn_start_washing", "start")},
			[]models.InlineKeyboardButton{b("btn_cancel_order", "cancel")},
		)
	case store.StatusWashing:
		return inline([]models.InlineKeyboardButton{b("btn_plus10", "plus10"), b("btn_ready", "ready")})
	case store.StatusReady:
		return inline([]models.InlineKeyboardButton{b("btn_picked", "done")})
	}
	return noKeyboard()
}

func (a *App) adminToday(ctx context.Context, chatID int64, l string) {
	end := a.dayStart(time.Now()).AddDate(0, 0, 1)
	orders, err := a.st.TodayOrders(ctx, end)
	if err != nil {
		a.fail(ctx, chatID, l, err)
		return
	}
	if len(orders) == 0 {
		a.send(ctx, chatID, T(l, "today_empty"), nil)
		return
	}
	a.send(ctx, chatID, T(l, "today_header", len(orders)), nil)
	for _, o := range orders {
		a.send(ctx, chatID, a.orderCard(l, o, true), adminOrderKB(l, o))
	}
}

func (a *App) adminUpcoming(ctx context.Context, chatID int64, l string) {
	orders, err := a.st.UpcomingBookings(ctx, a.dayStart(time.Now()).AddDate(0, 0, 1))
	if err != nil {
		a.fail(ctx, chatID, l, err)
		return
	}
	if len(orders) == 0 {
		a.send(ctx, chatID, T(l, "upcoming_empty"), nil)
		return
	}
	text := T(l, "upcoming_header") + "\n"
	lastDay := ""
	for _, o := range orders {
		if d := fmtDate(l, a.local(o.ScheduledAt)); d != lastDay {
			text += "\n<b>" + d + "</b>\n"
			lastDay = d
		}
		who := ""
		if o.ClientName != nil {
			who = " · " + esc(*o.ClientName)
		}
		text += a.fmtTime(o.ScheduledAt) + " — " + esc(o.CarPlate) + " · " + esc(o.ServiceName.Get(l)) + who + "\n"
	}
	a.send(ctx, chatID, text, nil)
}

// ---------- walk-in: plate → service → phone (optional) → QR for the client ----------

func (a *App) adminWalkinStart(ctx context.Context, chatID int64, u *store.User) {
	a.setState(u.TelegramID, &state{step: stepWalkinPlate})
	a.send(ctx, chatID, T(u.Lang, "walkin_title"), cancelKeyboard(u.Lang))
}

func (a *App) onWalkinText(ctx context.Context, chatID int64, u *store.User, s *state, text string) {
	l := u.Lang
	switch s.step {
	case stepWalkinPlate:
		plate, ok := normalizePlate(text)
		if !ok {
			a.send(ctx, chatID, T(l, "bad_plate"), nil)
			return
		}
		s.plate, s.step = plate, stepWalkinService
		msg, kb, err := a.servicesView(ctx, l, "ws")
		if err != nil {
			a.fail(ctx, chatID, l, err)
			return
		}
		a.send(ctx, chatID, "🚘 "+esc(plate)+"\n\n"+msg, kb)

	case stepWalkinPhone:
		phone := digitsOnly(text)
		if len(phone) < 9 {
			a.send(ctx, chatID, T(l, "phone_short"), nil)
			return
		}
		a.createWalkin(ctx, chatID, u, phone)
	}
}

func (a *App) onAdminCallback(ctx context.Context, cq *models.CallbackQuery, u *store.User, kind, rest string) {
	l := u.Lang
	restart := T(l, "walkin_restart", T(l, "btn_walkin"))
	switch kind {
	case "ws": // walk-in service chosen
		s := a.getState(cq.From.ID)
		if s == nil || s.step != stepWalkinService {
			a.answer(ctx, cq, restart, true)
			return
		}
		id, err := strconv.Atoi(rest)
		if err != nil {
			a.answer(ctx, cq, "", false)
			return
		}
		s.serviceID, s.step = id, stepWalkinPhone
		a.edit(ctx, cq, "🚘 "+esc(s.plate)+"\n\n"+T(l, "walkin_phone_prompt"),
			inline([]models.InlineKeyboardButton{ib(T(l, "btn_skip"), "wskip")}))
		a.answer(ctx, cq, "", false)

	case "wskip":
		s := a.getState(cq.From.ID)
		if s == nil || s.step != stepWalkinPhone {
			a.answer(ctx, cq, restart, true)
			return
		}
		a.edit(ctx, cq, "🚘 "+esc(s.plate), noKeyboard())
		a.answer(ctx, cq, "", false)
		a.createWalkin(ctx, cq.From.ID, u, "")

	case "a":
		a.adminAction(ctx, cq, u, rest)
	}
}

func (a *App) createWalkin(ctx context.Context, chatID int64, u *store.User, phone string) {
	l := u.Lang
	s := a.getState(u.TelegramID)
	if s == nil || s.serviceID == 0 {
		a.send(ctx, chatID, T(l, "walkin_restart", T(l, "btn_walkin")), a.mainMenu(l, true))
		return
	}
	var userID *int64
	if phone != "" {
		if cu, err := a.st.FindUserByPhone(ctx, phone); err == nil {
			userID = &cu.ID
		} else if !errors.Is(err, store.ErrNotFound) {
			a.fail(ctx, chatID, l, err)
			return
		}
	}
	o, err := a.st.CreateWalkin(ctx, userID, phone, s.plate, s.serviceID)
	if err != nil {
		a.fail(ctx, chatID, l, err)
		return
	}
	a.clearState(u.TelegramID)

	link := "https://t.me/" + a.username + "?start=" + o.Code
	caption := T(l, "walkin_registered") + "\n\n"
	if o.ClientTelegramID != nil {
		caption += T(l, "walkin_client_found", esc(deref(o.ClientName)))
	} else {
		caption += T(l, "walkin_scan_qr", link)
	}
	a.sendQR(ctx, chatID, l, link, caption)
	a.send(ctx, chatID, a.orderCard(l, o, true), adminOrderKB(l, o))

	if o.ClientTelegramID != nil {
		cl := clientLang(o)
		a.send(ctx, *o.ClientTelegramID, T(cl, "walkin_client_msg")+"\n\n"+a.orderCard(cl, o, false), nil)
	}
}

func (a *App) sendQR(ctx context.Context, chatID int64, l, link, caption string) {
	png, err := qrcode.Encode(link, qrcode.Medium, 512)
	if err != nil {
		log.Printf("qr: %v", err)
		a.send(ctx, chatID, caption, a.mainMenu(l, true))
		return
	}
	_, err = a.b.SendPhoto(ctx, &bot.SendPhotoParams{
		ChatID:      chatID,
		Photo:       &models.InputFileUpload{Filename: "order.png", Data: bytes.NewReader(png)},
		Caption:     caption,
		ParseMode:   models.ParseModeHTML,
		ReplyMarkup: a.mainMenu(l, true),
	})
	if err != nil {
		log.Printf("send qr: %v", err)
		a.send(ctx, chatID, caption, a.mainMenu(l, true))
	}
}

// ---------- status buttons ----------

func (a *App) adminAction(ctx context.Context, cq *models.CallbackQuery, u *store.User, rest string) {
	l := u.Lang
	act, idStr, _ := strings.Cut(rest, ":")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		a.answer(ctx, cq, T(l, "bad_request"), false)
		return
	}
	cur, err := a.st.OrderByID(ctx, id)
	if err != nil {
		a.answer(ctx, cq, T(l, "order_not_found"), true)
		return
	}

	var o *store.Order
	switch act {
	case "arrive":
		o, err = a.st.SetStatus(ctx, id, []string{store.StatusBooked}, store.StatusWaiting, nil)
	case "start":
		eta := time.Now().Add(time.Duration(cur.DurationMin) * time.Minute)
		o, err = a.st.SetStatus(ctx, id, []string{store.StatusBooked, store.StatusWaiting}, store.StatusWashing, &eta)
	case "plus10":
		o, err = a.st.ExtendEta(ctx, id, 10)
	case "ready":
		o, err = a.st.SetStatus(ctx, id, []string{store.StatusWashing, store.StatusWaiting}, store.StatusReady, nil)
	case "done":
		o, err = a.st.SetStatus(ctx, id, []string{store.StatusReady, store.StatusWashing}, store.StatusDone, nil)
	case "cancel":
		o, err = a.st.SetStatus(ctx, id, []string{store.StatusBooked, store.StatusWaiting}, store.StatusCancelled, nil)
	default:
		a.answer(ctx, cq, T(l, "unknown_action"), false)
		return
	}
	if errors.Is(err, store.ErrNotFound) {
		// someone else already changed it — show the current state
		a.edit(ctx, cq, a.orderCard(l, cur, true), adminOrderKB(l, cur))
		a.answer(ctx, cq, T(l, "already_updated"), true)
		return
	}
	if err != nil {
		a.answer(ctx, cq, "", false)
		a.fail(ctx, cq.From.ID, l, err)
		return
	}
	a.edit(ctx, cq, a.orderCard(l, o, true), adminOrderKB(l, o))
	a.answer(ctx, cq, T(l, "saved"), false)
	a.notifyClient(ctx, o, act)
}

func (a *App) notifyClient(ctx context.Context, o *store.Order, act string) {
	if o.ClientTelegramID == nil {
		return
	}
	l := clientLang(o)
	plate := esc(o.CarPlate)
	var text string
	switch act {
	case "arrive":
		text = T(l, "n_arrive", plate)
	case "start":
		text = T(l, "n_start", plate, a.fmtTime(*o.EtaAt))
	case "plus10":
		text = T(l, "n_plus10", plate, a.fmtTime(*o.EtaAt))
	case "ready":
		text = T(l, "n_ready", plate)
		if a.cfg.Address != "" {
			text += "\n📍 " + esc(a.cfg.Address)
		}
	case "done":
		text = T(l, "n_done", T(l, "btn_book"))
	case "cancel":
		text = T(l, "n_cancel", plate)
	default:
		return
	}
	a.send(ctx, *o.ClientTelegramID, text, nil)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
