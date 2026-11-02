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

func adminOrderKB(o *store.Order) models.ReplyMarkup {
	id := itoa(o.ID)
	switch o.Status {
	case store.StatusBooked:
		return inline(
			[]models.InlineKeyboardButton{ib("🚗 Arrived", "a:arrive:"+id), ib("🧽 Start", "a:start:"+id)},
			[]models.InlineKeyboardButton{ib("❌ Cancel", "a:cancel:"+id)},
		)
	case store.StatusWaiting:
		return inline(
			[]models.InlineKeyboardButton{ib("🧽 Start washing", "a:start:"+id)},
			[]models.InlineKeyboardButton{ib("❌ Cancel", "a:cancel:"+id)},
		)
	case store.StatusWashing:
		return inline(
			[]models.InlineKeyboardButton{ib("⏱ +10 min", "a:plus10:"+id), ib("✅ Ready", "a:ready:"+id)},
		)
	case store.StatusReady:
		return inline([]models.InlineKeyboardButton{ib("🏁 Picked up", "a:done:"+id)})
	}
	return noKeyboard()
}

func (a *App) adminToday(ctx context.Context, chatID int64) {
	end := a.dayStart(time.Now()).AddDate(0, 0, 1)
	orders, err := a.st.TodayOrders(ctx, end)
	if err != nil {
		a.fail(ctx, chatID, err)
		return
	}
	if len(orders) == 0 {
		a.send(ctx, chatID, "📋 Nothing for today yet.", nil)
		return
	}
	a.send(ctx, chatID, "📋 <b>Today: "+strconv.Itoa(len(orders))+" active</b>", nil)
	for _, o := range orders {
		a.send(ctx, chatID, a.orderCard(o, true), adminOrderKB(o))
	}
}

func (a *App) adminUpcoming(ctx context.Context, chatID int64) {
	orders, err := a.st.UpcomingBookings(ctx, a.dayStart(time.Now()).AddDate(0, 0, 1))
	if err != nil {
		a.fail(ctx, chatID, err)
		return
	}
	if len(orders) == 0 {
		a.send(ctx, chatID, "🗓 No bookings after today.", nil)
		return
	}
	text := "🗓 <b>Upcoming bookings</b>\n"
	lastDay := ""
	for _, o := range orders {
		if d := a.local(o.ScheduledAt).Format("Mon, 02 Jan"); d != lastDay {
			text += "\n<b>" + d + "</b>\n"
			lastDay = d
		}
		who := ""
		if o.ClientName != nil {
			who = " · " + esc(*o.ClientName)
		}
		text += a.fmtTime(o.ScheduledAt) + " — " + esc(o.CarPlate) + " · " + esc(o.ServiceName) + who + "\n"
	}
	a.send(ctx, chatID, text, nil)
}

// ---------- walk-in: plate → service → phone (optional) → QR for the client ----------

func (a *App) adminWalkinStart(ctx context.Context, chatID, tgID int64) {
	a.setState(tgID, &state{step: stepWalkinPlate})
	a.send(ctx, chatID, "➕ <b>New walk-in car</b>\n\n🚘 Send the car plate number:", cancelKeyboard())
}

func (a *App) onWalkinText(ctx context.Context, chatID, tgID int64, s *state, text string) {
	switch s.step {
	case stepWalkinPlate:
		plate, ok := normalizePlate(text)
		if !ok {
			a.send(ctx, chatID, "Invalid plate, try again (e.g. <code>01A123BC</code>):", nil)
			return
		}
		s.plate, s.step = plate, stepWalkinService
		msg, kb, err := a.servicesView(ctx, "ws")
		if err != nil {
			a.fail(ctx, chatID, err)
			return
		}
		a.send(ctx, chatID, "🚘 "+esc(plate)+"\n\n"+msg, kb)

	case stepWalkinPhone:
		phone := digitsOnly(text)
		if len(phone) < 9 {
			a.send(ctx, chatID, "Phone looks too short. Send it like <code>90 123 45 67</code> or tap Skip.", nil)
			return
		}
		a.createWalkin(ctx, chatID, tgID, phone)
	}
}

func (a *App) onAdminCallback(ctx context.Context, cq *models.CallbackQuery, kind, rest string) {
	chatID := cq.From.ID
	switch kind {
	case "ws": // walk-in service chosen
		s := a.getState(cq.From.ID)
		if s == nil || s.step != stepWalkinService {
			a.answer(ctx, cq, "Start again with "+btnWalkin, true)
			return
		}
		id, err := strconv.Atoi(rest)
		if err != nil {
			return
		}
		s.serviceID, s.step = id, stepWalkinPhone
		a.edit(ctx, cq, "🚘 "+esc(s.plate)+"\n\n📱 Send the client's <b>phone number</b> — if they already use this bot, they'll be notified automatically.\n\nOr skip and show them the QR code.",
			inline([]models.InlineKeyboardButton{ib("Skip ➡️", "wskip")}))
		a.answer(ctx, cq, "", false)

	case "wskip":
		s := a.getState(cq.From.ID)
		if s == nil || s.step != stepWalkinPhone {
			a.answer(ctx, cq, "Start again with "+btnWalkin, true)
			return
		}
		a.edit(ctx, cq, "🚘 "+esc(s.plate), noKeyboard())
		a.answer(ctx, cq, "", false)
		a.createWalkin(ctx, chatID, cq.From.ID, "")

	case "a":
		a.adminAction(ctx, cq, rest)
	}
}

func (a *App) createWalkin(ctx context.Context, chatID, tgID int64, phone string) {
	s := a.getState(tgID)
	if s == nil || s.serviceID == 0 {
		a.send(ctx, chatID, "Start again with "+btnWalkin, mainMenu(true))
		return
	}
	var userID *int64
	if phone != "" {
		if u, err := a.st.FindUserByPhone(ctx, phone); err == nil {
			userID = &u.ID
		} else if !errors.Is(err, store.ErrNotFound) {
			a.fail(ctx, chatID, err)
			return
		}
	}
	o, err := a.st.CreateWalkin(ctx, userID, phone, s.plate, s.serviceID)
	if err != nil {
		a.fail(ctx, chatID, err)
		return
	}
	a.clearState(tgID)

	link := "https://t.me/" + a.username + "?start=" + o.Code
	caption := "✅ Car registered.\n\n"
	if o.ClientTelegramID != nil {
		caption += "👤 Client <b>" + esc(deref(o.ClientName)) + "</b> found by phone and notified."
	} else {
		caption += "📲 Ask the client to <b>scan this QR</b> to track the car:\n" + link
	}
	a.sendQR(ctx, chatID, link, caption)
	a.send(ctx, chatID, a.orderCard(o, true), adminOrderKB(o))

	if o.ClientTelegramID != nil {
		a.send(ctx, *o.ClientTelegramID, "👋 Your car was accepted at CWash. I'll message you when it's ready.\n\n"+a.orderCard(o, false), nil)
	}
}

func (a *App) sendQR(ctx context.Context, chatID int64, link, caption string) {
	png, err := qrcode.Encode(link, qrcode.Medium, 512)
	if err != nil {
		log.Printf("qr: %v", err)
		a.send(ctx, chatID, caption, mainMenu(true))
		return
	}
	_, err = a.b.SendPhoto(ctx, &bot.SendPhotoParams{
		ChatID:      chatID,
		Photo:       &models.InputFileUpload{Filename: "order.png", Data: bytes.NewReader(png)},
		Caption:     caption,
		ParseMode:   models.ParseModeHTML,
		ReplyMarkup: mainMenu(true),
	})
	if err != nil {
		log.Printf("send qr: %v", err)
		a.send(ctx, chatID, caption, mainMenu(true))
	}
}

// ---------- status buttons ----------

func (a *App) adminAction(ctx context.Context, cq *models.CallbackQuery, rest string) {
	act, idStr, _ := strings.Cut(rest, ":")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		a.answer(ctx, cq, "Bad request", false)
		return
	}
	cur, err := a.st.OrderByID(ctx, id)
	if err != nil {
		a.answer(ctx, cq, "Order not found", true)
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
		a.answer(ctx, cq, "Unknown action", false)
		return
	}
	if errors.Is(err, store.ErrNotFound) {
		// someone else already changed it — show the current state
		a.edit(ctx, cq, a.orderCard(cur, true), adminOrderKB(cur))
		a.answer(ctx, cq, "Already updated by someone else", true)
		return
	}
	if err != nil {
		a.fail(ctx, cq.From.ID, err)
		return
	}
	a.edit(ctx, cq, a.orderCard(o, true), adminOrderKB(o))
	a.answer(ctx, cq, "Saved ✅", false)
	a.notifyClient(ctx, o, act)
}

func (a *App) notifyClient(ctx context.Context, o *store.Order, act string) {
	if o.ClientTelegramID == nil {
		return
	}
	var text string
	switch act {
	case "arrive":
		text = "🚗 We've got your car <b>" + esc(o.CarPlate) + "</b>. It's in the queue — I'll tell you when washing starts."
	case "start":
		text = "🧽 Washing started for <b>" + esc(o.CarPlate) + "</b>.\nReady at about <b>" + a.fmtTime(*o.EtaAt) + "</b>."
	case "plus10":
		text = "⏱ A bit more time needed for <b>" + esc(o.CarPlate) + "</b>.\nNew ready time: <b>" + a.fmtTime(*o.EtaAt) + "</b>."
	case "ready":
		text = "✅ <b>Your car " + esc(o.CarPlate) + " is ready!</b>\nYou can pick it up now."
		if a.cfg.Address != "" {
			text += "\n📍 " + esc(a.cfg.Address)
		}
	case "done":
		text = "🙏 Thanks for choosing CWash! See you next time.\nTap " + btnBook + " to reserve your next wash."
	case "cancel":
		text = "❌ Your order for <b>" + esc(o.CarPlate) + "</b> was cancelled by the wash. Contact us if this is a mistake."
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
