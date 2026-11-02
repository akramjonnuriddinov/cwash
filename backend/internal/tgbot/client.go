package tgbot

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/go-telegram/bot/models"

	"github.com/akramjon/cwash/backend/internal/slots"
	"github.com/akramjon/cwash/backend/internal/store"
)

const bookingBuffer = 15 * time.Minute // earliest bookable start = now + buffer

func (a *App) onStart(ctx context.Context, chatID int64, u *store.User, payload string, admin bool) {
	l := u.Lang
	// Deep link from a washer's QR code: t.me/<bot>?start=<order code>
	if payload != "" {
		o, err := a.st.LinkByCode(ctx, strings.ToUpper(payload), u.ID)
		if errors.Is(err, store.ErrNotFound) {
			a.send(ctx, chatID, T(l, "code_not_found"), a.mainMenu(l, admin))
			return
		}
		if err != nil {
			a.fail(ctx, chatID, l, err)
			return
		}
		a.send(ctx, chatID, T(l, "tracked_hi"), a.mainMenu(l, admin))
		a.send(ctx, chatID, a.orderCard(l, o, false), nil)
		return
	}

	text := T(l, "welcome")
	if admin {
		text += T(l, "welcome_admin", T(l, "btn_today"), T(l, "btn_walkin"))
	}
	a.send(ctx, chatID, text, a.mainMenu(l, admin))
}

func (a *App) onContact(ctx context.Context, m *models.Message, u *store.User, admin bool) {
	chatID, l := m.Chat.ID, u.Lang
	if m.Contact.UserID != m.From.ID {
		a.send(ctx, chatID, T(l, "share_own_phone"), phoneKeyboard(l))
		return
	}
	phone := digitsOnly(m.Contact.PhoneNumber)
	if err := a.st.SetPhone(ctx, u.ID, phone); err != nil {
		a.fail(ctx, chatID, l, err)
		return
	}
	u.Phone = phone

	// A washer may have registered this phone at the counter already.
	linked, err := a.st.LinkByPhone(ctx, u.ID, phone)
	if err != nil {
		a.fail(ctx, chatID, l, err)
		return
	}
	a.send(ctx, chatID, T(l, "phone_saved"), a.mainMenu(l, admin))
	for _, o := range linked {
		a.send(ctx, chatID, T(l, "found_car")+"\n\n"+a.orderCard(l, o, false), nil)
	}

	if s := a.getState(m.From.ID); s != nil && s.step == stepPhoneForBooking {
		a.clearState(m.From.ID)
		a.startBooking(ctx, chatID, u)
	}
}

func (a *App) showContacts(ctx context.Context, chatID int64, l string, admin bool) {
	text := "<b>CWash</b>\n"
	if a.cfg.Address != "" {
		text += "📍 " + esc(a.cfg.Address) + "\n"
	}
	if a.cfg.Phone != "" {
		text += "📞 " + esc(a.cfg.Phone) + "\n"
	}
	text += T(l, "hours", a.cfg.OpenHour, a.cfg.CloseHour)
	a.send(ctx, chatID, text, a.mainMenu(l, admin))
}

// ---------- my orders ----------

func (a *App) showMyOrders(ctx context.Context, chatID int64, u *store.User) {
	l := u.Lang
	orders, err := a.st.ActiveOrdersForUser(ctx, u.ID)
	if err != nil {
		a.fail(ctx, chatID, l, err)
		return
	}
	if len(orders) == 0 {
		a.send(ctx, chatID, T(l, "no_orders", T(l, "btn_book")), nil)
		return
	}
	for _, o := range orders {
		a.send(ctx, chatID, a.orderCard(l, o, false), clientOrderKB(l, o))
	}
}

func clientOrderKB(l string, o *store.Order) models.ReplyMarkup {
	switch o.Status {
	case store.StatusBooked:
		return inline([]models.InlineKeyboardButton{ib(T(l, "btn_cancel_booking"), "cx:"+itoa(o.ID))})
	case store.StatusDone, store.StatusCancelled:
		return noKeyboard()
	}
	return inline([]models.InlineKeyboardButton{ib(T(l, "btn_refresh"), "rf:"+itoa(o.ID))})
}

// ---------- booking flow: service → day → time → plate → confirm ----------

func (a *App) startBooking(ctx context.Context, chatID int64, u *store.User) {
	l := u.Lang
	if u.Phone == "" {
		a.setState(u.TelegramID, &state{step: stepPhoneForBooking})
		a.send(ctx, chatID, T(l, "need_phone"), phoneKeyboard(l))
		return
	}
	text, kb, err := a.servicesView(ctx, l, "svc")
	if err != nil {
		a.fail(ctx, chatID, l, err)
		return
	}
	a.send(ctx, chatID, text, kb)
}

func (a *App) servicesView(ctx context.Context, l, prefix string) (string, models.ReplyMarkup, error) {
	svcs, err := a.st.Services(ctx)
	if err != nil {
		return "", nil, err
	}
	var rows [][]models.InlineKeyboardButton
	for _, s := range svcs {
		rows = append(rows, []models.InlineKeyboardButton{ib(a.serviceLabel(l, s), prefix+":"+strconv.Itoa(s.ID))})
	}
	return T(l, "choose_service"), inline(rows...), nil
}

func (a *App) dayStart(t time.Time) time.Time {
	y, m, d := a.local(t).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, a.cfg.Location)
}

func (a *App) daysView(l string, svc *store.Service, note string) (string, models.ReplyMarkup) {
	today := a.dayStart(time.Now())
	var rows [][]models.InlineKeyboardButton
	var row []models.InlineKeyboardButton
	for i := 0; i < a.cfg.BookingDays; i++ {
		d := today.AddDate(0, 0, i)
		label := fmtDayButton(l, d)
		switch i {
		case 0:
			label = T(l, "today")
		case 1:
			label = T(l, "tomorrow")
		}
		row = append(row, ib(label, "day:"+strconv.Itoa(svc.ID)+":"+d.Format("20060102")))
		if len(row) == 3 {
			rows, row = append(rows, row), nil
		}
	}
	if len(row) > 0 {
		rows = append(rows, row)
	}
	rows = append(rows, []models.InlineKeyboardButton{ib(T(l, "btn_back"), "bk:svc")})
	text := "🧼 " + esc(svc.Name.Get(l)) + "\n\n" + T(l, "choose_day")
	if note != "" {
		text = note + "\n\n" + text
	}
	return text, inline(rows...)
}

func (a *App) freeSlots(ctx context.Context, svc *store.Service, day time.Time) ([]time.Time, error) {
	open := day.Add(time.Duration(a.cfg.OpenHour) * time.Hour)
	closeAt := day.Add(time.Duration(a.cfg.CloseHour) * time.Hour)
	busy, err := a.st.BusyIntervals(ctx, open, closeAt)
	if err != nil {
		return nil, err
	}
	return slots.Free(slots.Params{
		Open: open, Close: closeAt,
		Step:      time.Duration(a.cfg.SlotMinutes) * time.Minute,
		Duration:  svc.Duration(),
		Boxes:     a.cfg.Boxes,
		NotBefore: time.Now().Add(bookingBuffer),
	}, busy), nil
}

func (a *App) slotsView(ctx context.Context, l string, svc *store.Service, day time.Time) (string, models.ReplyMarkup, error) {
	free, err := a.freeSlots(ctx, svc, day)
	if err != nil {
		return "", nil, err
	}
	if len(free) == 0 {
		text, kb := a.daysView(l, svc, T(l, "no_free", fmtDate(l, day)))
		return text, kb, nil
	}
	var rows [][]models.InlineKeyboardButton
	var row []models.InlineKeyboardButton
	for _, t := range free {
		row = append(row, ib(a.fmtTime(t), "sl:"+strconv.Itoa(svc.ID)+":"+itoa(t.Unix())))
		if len(row) == 4 {
			rows, row = append(rows, row), nil
		}
	}
	if len(row) > 0 {
		rows = append(rows, row)
	}
	rows = append(rows, []models.InlineKeyboardButton{ib(T(l, "btn_other_day"), "svc:"+strconv.Itoa(svc.ID))})
	text := "🧼 " + esc(svc.Name.Get(l)) + "\n📅 " + fmtDate(l, day) + "\n\n" + T(l, "choose_time")
	return text, inline(rows...), nil
}

func (a *App) onClientCallback(ctx context.Context, cq *models.CallbackQuery, u *store.User, kind, rest string) {
	chatID, l := cq.From.ID, u.Lang
	parts := strings.Split(rest, ":")

	switch kind {
	case "bk": // booking navigation / confirm
		switch rest {
		case "svc":
			text, kb, err := a.servicesView(ctx, l, "svc")
			if err != nil {
				a.fail(ctx, chatID, l, err)
				break
			}
			a.edit(ctx, cq, text, kb)
		case "ok":
			a.confirmBooking(ctx, cq, u)
		case "no":
			a.clearState(cq.From.ID)
			a.edit(ctx, cq, T(l, "booking_cancelled"), noKeyboard())
		}

	case "svc": // svc:<serviceID>
		svc, err := a.serviceFromArg(ctx, rest)
		if err != nil {
			a.answer(ctx, cq, T(l, "service_unavailable"), true)
			return
		}
		text, kb := a.daysView(l, svc, "")
		a.edit(ctx, cq, text, kb)

	case "day": // day:<serviceID>:<YYYYMMDD>
		if len(parts) != 2 {
			break
		}
		svc, err := a.serviceFromArg(ctx, parts[0])
		if err != nil {
			a.answer(ctx, cq, T(l, "service_unavailable"), true)
			return
		}
		day, err := time.ParseInLocation("20060102", parts[1], a.cfg.Location)
		if err != nil {
			break
		}
		text, kb, err := a.slotsView(ctx, l, svc, day)
		if err != nil {
			a.fail(ctx, chatID, l, err)
			break
		}
		a.edit(ctx, cq, text, kb)

	case "sl": // sl:<serviceID>:<unix>
		if len(parts) != 2 {
			break
		}
		svc, err := a.serviceFromArg(ctx, parts[0])
		if err != nil {
			a.answer(ctx, cq, T(l, "service_unavailable"), true)
			return
		}
		unix, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			break
		}
		slot := time.Unix(unix, 0)
		if slot.Before(time.Now().Add(bookingBuffer)) {
			a.answer(ctx, cq, T(l, "time_passed"), true)
			return
		}
		a.setState(cq.From.ID, &state{step: stepBookPlate, serviceID: svc.ID, slot: slot})

		kb := noKeyboard()
		if plates, _ := a.st.LastPlates(ctx, u.ID); len(plates) > 0 {
			var row []models.InlineKeyboardButton
			for _, p := range plates {
				row = append(row, ib(p, "pl:"+p))
			}
			kb = inline(row)
		}
		a.edit(ctx, cq, "🧼 "+esc(svc.Name.Get(l))+"\n📅 "+a.fmtDay(l, slot)+", <b>"+a.fmtTime(slot)+"</b>\n\n"+T(l, "send_plate"), kb)

	case "pl": // pl:<plate> — reuse a previous plate
		a.onBookingPlate(ctx, chatID, u, rest)

	case "cx": // cx:<orderID> — client cancels own booking
		a.clientCancel(ctx, cq, u, rest)

	case "rf": // rf:<orderID> — refresh status card
		id, _ := strconv.ParseInt(rest, 10, 64)
		o, err := a.st.OrderByID(ctx, id)
		if err != nil || o.ClientTelegramID == nil || *o.ClientTelegramID != cq.From.ID {
			a.answer(ctx, cq, T(l, "order_not_found"), false)
			return
		}
		a.edit(ctx, cq, a.orderCard(l, o, false), clientOrderKB(l, o))
	}
	a.answer(ctx, cq, "", false)
}

func (a *App) serviceFromArg(ctx context.Context, s string) (*store.Service, error) {
	id, err := strconv.Atoi(s)
	if err != nil {
		return nil, err
	}
	return a.st.Service(ctx, id)
}

func (a *App) onBookingPlate(ctx context.Context, chatID int64, u *store.User, text string) {
	l := u.Lang
	s := a.getState(u.TelegramID)
	if s == nil || s.step != stepBookPlate {
		a.send(ctx, chatID, T(l, "start_again", T(l, "btn_book")), nil)
		return
	}
	plate, ok := normalizePlate(text)
	if !ok {
		a.send(ctx, chatID, T(l, "bad_plate"), nil)
		return
	}
	s.plate = plate
	svc, err := a.st.Service(ctx, s.serviceID)
	if err != nil {
		a.fail(ctx, chatID, l, err)
		return
	}
	text = T(l, "confirm_title") + "\n\n" +
		"🧼 " + esc(a.serviceLabel(l, *svc)) + "\n" +
		"📅 " + a.fmtDay(l, s.slot) + ", <b>" + a.fmtTime(s.slot) + "</b>\n" +
		"🚘 " + esc(plate)
	a.send(ctx, chatID, text, inline([]models.InlineKeyboardButton{
		ib(T(l, "btn_confirm"), "bk:ok"), ib(T(l, "btn_cancel"), "bk:no"),
	}))
}

func (a *App) confirmBooking(ctx context.Context, cq *models.CallbackQuery, u *store.User) {
	chatID, l := cq.From.ID, u.Lang
	s := a.getState(cq.From.ID)
	if s == nil || s.step != stepBookPlate || s.plate == "" {
		a.edit(ctx, cq, T(l, "booking_expired"), noKeyboard())
		return
	}
	svc, err := a.st.Service(ctx, s.serviceID)
	if err != nil {
		a.fail(ctx, chatID, l, err)
		return
	}
	o, err := a.st.CreateBooking(ctx, u, s.plate, svc, s.slot, a.cfg.Boxes, time.Duration(a.cfg.SlotMinutes)*time.Minute)
	if errors.Is(err, store.ErrSlotTaken) {
		text, kb, err := a.slotsView(ctx, l, svc, a.dayStart(s.slot))
		if err != nil {
			a.fail(ctx, chatID, l, err)
			return
		}
		a.edit(ctx, cq, T(l, "slot_taken")+"\n\n"+text, kb)
		return
	}
	if err != nil {
		a.fail(ctx, chatID, l, err)
		return
	}
	a.clearState(cq.From.ID)
	a.edit(ctx, cq, T(l, "booked")+"\n\n"+a.orderCard(l, o, false)+"\n\n"+T(l, "remind_note"), noKeyboard())
	a.notifyAdmins(ctx, func(al string) (string, models.ReplyMarkup) {
		return T(al, "adm_new_booking") + "\n\n" + a.orderCard(al, o, true), adminOrderKB(al, o)
	})
}

func (a *App) clientCancel(ctx context.Context, cq *models.CallbackQuery, u *store.User, arg string) {
	l := u.Lang
	id, _ := strconv.ParseInt(arg, 10, 64)
	o, err := a.st.OrderByID(ctx, id)
	if err != nil || o.ClientTelegramID == nil || *o.ClientTelegramID != cq.From.ID {
		a.answer(ctx, cq, T(l, "order_not_found"), true)
		return
	}
	o, err = a.st.SetStatus(ctx, id, []string{store.StatusBooked}, store.StatusCancelled, nil)
	if errors.Is(err, store.ErrNotFound) {
		a.answer(ctx, cq, T(l, "cant_cancel"), true)
		return
	}
	if err != nil {
		a.fail(ctx, cq.From.ID, l, err)
		return
	}
	a.edit(ctx, cq, a.orderCard(l, o, false), noKeyboard())
	a.notifyAdmins(ctx, func(al string) (string, models.ReplyMarkup) {
		return T(al, "adm_client_cancelled") + "\n\n" + a.orderCard(al, o, true), nil
	})
}
