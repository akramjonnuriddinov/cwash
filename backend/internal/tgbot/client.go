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
	// Deep link from a washer's QR code: t.me/<bot>?start=<order code>
	if payload != "" {
		o, err := a.st.LinkByCode(ctx, strings.ToUpper(payload), u.ID)
		if errors.Is(err, store.ErrNotFound) {
			a.send(ctx, chatID, "This order code was not found or is linked to another account.", mainMenu(admin))
			return
		}
		if err != nil {
			a.fail(ctx, chatID, err)
			return
		}
		a.send(ctx, chatID, "👋 Hi! I'll message you as soon as your car's status changes — no need to call.", mainMenu(admin))
		a.send(ctx, chatID, a.orderCard(o, false), nil)
		return
	}

	text := "👋 <b>Welcome to CWash!</b>\n\n" +
		"• 📅 Book a wash at a free time\n" +
		"• 🚗 See your car's status live — I'll notify you when it's ready\n\n" +
		"No more phone calls 🙂"
	if admin {
		text += "\n\n🛠 You are an <b>admin</b>: use 📋 Today and ➕ Walk-in car."
	}
	a.send(ctx, chatID, text, mainMenu(admin))
}

func (a *App) onContact(ctx context.Context, m *models.Message, u *store.User, admin bool) {
	chatID := m.Chat.ID
	if m.Contact.UserID != m.From.ID {
		a.send(ctx, chatID, "Please share <b>your own</b> number with the button below.", phoneKeyboard())
		return
	}
	phone := digitsOnly(m.Contact.PhoneNumber)
	if err := a.st.SetPhone(ctx, u.ID, phone); err != nil {
		a.fail(ctx, chatID, err)
		return
	}
	u.Phone = phone

	// A washer may have registered this phone at the counter already.
	linked, err := a.st.LinkByPhone(ctx, u.ID, phone)
	if err != nil {
		a.fail(ctx, chatID, err)
		return
	}
	a.send(ctx, chatID, "✅ Thanks, phone saved.", mainMenu(admin))
	for _, o := range linked {
		a.send(ctx, chatID, "We found your car at the wash:\n\n"+a.orderCard(o, false), nil)
	}

	if s := a.getState(m.From.ID); s != nil && s.step == stepPhoneForBooking {
		a.clearState(m.From.ID)
		a.startBooking(ctx, chatID, u)
	}
}

func (a *App) showContacts(ctx context.Context, chatID int64, admin bool) {
	text := "<b>CWash</b>\n"
	if a.cfg.Address != "" {
		text += "📍 " + esc(a.cfg.Address) + "\n"
	}
	if a.cfg.Phone != "" {
		text += "📞 " + esc(a.cfg.Phone) + "\n"
	}
	text += "🕘 Every day " + strconv.Itoa(a.cfg.OpenHour) + ":00 – " + strconv.Itoa(a.cfg.CloseHour) + ":00"
	a.send(ctx, chatID, text, mainMenu(admin))
}

// ---------- my orders ----------

func (a *App) showMyOrders(ctx context.Context, chatID int64, u *store.User) {
	orders, err := a.st.ActiveOrdersForUser(ctx, u.ID)
	if err != nil {
		a.fail(ctx, chatID, err)
		return
	}
	if len(orders) == 0 {
		a.send(ctx, chatID, "You have no active orders.\nTap <b>"+btnBook+"</b> to reserve a time.", nil)
		return
	}
	for _, o := range orders {
		var kb models.ReplyMarkup
		if o.Status == store.StatusBooked {
			kb = inline([]models.InlineKeyboardButton{ib("❌ Cancel booking", "cx:"+itoa(o.ID))})
		} else {
			kb = inline([]models.InlineKeyboardButton{ib("🔄 Refresh", "rf:"+itoa(o.ID))})
		}
		a.send(ctx, chatID, a.orderCard(o, false), kb)
	}
}

// ---------- booking flow: service → day → time → plate → confirm ----------

func (a *App) startBooking(ctx context.Context, chatID int64, u *store.User) {
	if u.Phone == "" {
		a.setState(u.TelegramID, &state{step: stepPhoneForBooking})
		a.send(ctx, chatID, "To book, please share your phone number so the wash can reach you if needed.", phoneKeyboard())
		return
	}
	text, kb, err := a.servicesView(ctx, "svc")
	if err != nil {
		a.fail(ctx, chatID, err)
		return
	}
	a.send(ctx, chatID, text, kb)
}

func (a *App) servicesView(ctx context.Context, prefix string) (string, models.ReplyMarkup, error) {
	svcs, err := a.st.Services(ctx)
	if err != nil {
		return "", nil, err
	}
	var rows [][]models.InlineKeyboardButton
	for _, s := range svcs {
		rows = append(rows, []models.InlineKeyboardButton{ib(a.serviceLabel(s), prefix+":"+strconv.Itoa(s.ID))})
	}
	return "🧼 <b>Choose a service:</b>", inline(rows...), nil
}

func (a *App) dayStart(t time.Time) time.Time {
	y, m, d := a.local(t).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, a.cfg.Location)
}

func (a *App) daysView(svc *store.Service, note string) (string, models.ReplyMarkup) {
	today := a.dayStart(time.Now())
	var rows [][]models.InlineKeyboardButton
	var row []models.InlineKeyboardButton
	for i := 0; i < a.cfg.BookingDays; i++ {
		d := today.AddDate(0, 0, i)
		label := d.Format("Mon 02.01")
		if i == 0 {
			label = "Today"
		} else if i == 1 {
			label = "Tomorrow"
		}
		row = append(row, ib(label, "day:"+strconv.Itoa(svc.ID)+":"+d.Format("20060102")))
		if len(row) == 3 {
			rows, row = append(rows, row), nil
		}
	}
	if len(row) > 0 {
		rows = append(rows, row)
	}
	rows = append(rows, []models.InlineKeyboardButton{ib("⬅️ Back", "bk:svc")})
	text := "🧼 " + esc(svc.Name) + "\n\n📅 <b>Choose a day:</b>"
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

func (a *App) slotsView(ctx context.Context, svc *store.Service, day time.Time) (string, models.ReplyMarkup, error) {
	free, err := a.freeSlots(ctx, svc, day)
	if err != nil {
		return "", nil, err
	}
	if len(free) == 0 {
		text, kb := a.daysView(svc, "😔 No free time on <b>"+day.Format("Mon, 02 Jan")+"</b>. Try another day.")
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
	rows = append(rows, []models.InlineKeyboardButton{ib("⬅️ Other day", "svc:"+strconv.Itoa(svc.ID))})
	text := "🧼 " + esc(svc.Name) + "\n📅 " + day.Format("Mon, 02 Jan") + "\n\n🕐 <b>Choose a time:</b>"
	return text, inline(rows...), nil
}

func (a *App) onClientCallback(ctx context.Context, cq *models.CallbackQuery, kind, rest string) {
	chatID := cq.From.ID
	parts := strings.Split(rest, ":")

	switch kind {
	case "bk": // booking navigation / confirm
		switch rest {
		case "svc":
			text, kb, err := a.servicesView(ctx, "svc")
			if err != nil {
				a.fail(ctx, chatID, err)
				break
			}
			a.edit(ctx, cq, text, kb)
		case "ok":
			a.confirmBooking(ctx, cq)
		case "no":
			a.clearState(cq.From.ID)
			a.edit(ctx, cq, "Booking cancelled.", noKeyboard())
		}

	case "svc": // svc:<serviceID>
		svc, err := a.serviceFromArg(ctx, rest)
		if err != nil {
			a.answer(ctx, cq, "Service unavailable", true)
			return
		}
		text, kb := a.daysView(svc, "")
		a.edit(ctx, cq, text, kb)

	case "day": // day:<serviceID>:<YYYYMMDD>
		if len(parts) != 2 {
			break
		}
		svc, err := a.serviceFromArg(ctx, parts[0])
		if err != nil {
			a.answer(ctx, cq, "Service unavailable", true)
			return
		}
		day, err := time.ParseInLocation("20060102", parts[1], a.cfg.Location)
		if err != nil {
			break
		}
		text, kb, err := a.slotsView(ctx, svc, day)
		if err != nil {
			a.fail(ctx, chatID, err)
			break
		}
		a.edit(ctx, cq, text, kb)

	case "sl": // sl:<serviceID>:<unix>
		if len(parts) != 2 {
			break
		}
		svc, err := a.serviceFromArg(ctx, parts[0])
		if err != nil {
			a.answer(ctx, cq, "Service unavailable", true)
			return
		}
		unix, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			break
		}
		slot := time.Unix(unix, 0)
		if slot.Before(time.Now().Add(bookingBuffer)) {
			a.answer(ctx, cq, "This time has passed, choose another", true)
			return
		}
		a.setState(cq.From.ID, &state{step: stepBookPlate, serviceID: svc.ID, slot: slot})

		var kb models.ReplyMarkup = noKeyboard()
		if u, err := a.user(ctx, &cq.From); err == nil {
			if plates, _ := a.st.LastPlates(ctx, u.ID); len(plates) > 0 {
				var row []models.InlineKeyboardButton
				for _, p := range plates {
					row = append(row, ib(p, "pl:"+p))
				}
				kb = inline(row)
			}
		}
		a.edit(ctx, cq, "🧼 "+esc(svc.Name)+"\n📅 "+a.fmtDay(slot)+", <b>"+a.fmtTime(slot)+"</b>\n\n🚘 <b>Send your car plate number</b> (e.g. 01A123BC):", kb)

	case "pl": // pl:<plate> — reuse a previous plate
		a.onBookingPlate(ctx, chatID, cq.From.ID, rest)

	case "cx": // cx:<orderID> — client cancels own booking
		a.clientCancel(ctx, cq, rest)

	case "rf": // rf:<orderID> — refresh status card
		id, _ := strconv.ParseInt(rest, 10, 64)
		o, err := a.st.OrderByID(ctx, id)
		if err != nil || o.ClientTelegramID == nil || *o.ClientTelegramID != cq.From.ID {
			a.answer(ctx, cq, "Order not found", false)
			return
		}
		var kb models.ReplyMarkup = noKeyboard()
		if o.Status != store.StatusDone && o.Status != store.StatusCancelled {
			kb = inline([]models.InlineKeyboardButton{ib("🔄 Refresh", "rf:"+itoa(o.ID))})
		}
		a.edit(ctx, cq, a.orderCard(o, false), kb)
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

func (a *App) onBookingPlate(ctx context.Context, chatID, tgID int64, text string) {
	s := a.getState(tgID)
	if s == nil || s.step != stepBookPlate {
		a.send(ctx, chatID, "Please start again: "+btnBook, nil)
		return
	}
	plate, ok := normalizePlate(text)
	if !ok {
		a.send(ctx, chatID, "That doesn't look like a plate number. Try again, e.g. <code>01A123BC</code>", nil)
		return
	}
	s.plate = plate
	svc, err := a.st.Service(ctx, s.serviceID)
	if err != nil {
		a.fail(ctx, chatID, err)
		return
	}
	text = "<b>Please confirm your booking:</b>\n\n" +
		"🧼 " + esc(a.serviceLabel(*svc)) + "\n" +
		"📅 " + a.fmtDay(s.slot) + ", <b>" + a.fmtTime(s.slot) + "</b>\n" +
		"🚘 " + esc(plate)
	a.send(ctx, chatID, text, inline([]models.InlineKeyboardButton{ib("✅ Confirm", "bk:ok"), ib("✖️ Cancel", "bk:no")}))
}

func (a *App) confirmBooking(ctx context.Context, cq *models.CallbackQuery) {
	chatID := cq.From.ID
	s := a.getState(cq.From.ID)
	if s == nil || s.step != stepBookPlate || s.plate == "" {
		a.edit(ctx, cq, "This booking expired, please start again.", noKeyboard())
		return
	}
	u, err := a.user(ctx, &cq.From)
	if err != nil {
		a.fail(ctx, chatID, err)
		return
	}
	svc, err := a.st.Service(ctx, s.serviceID)
	if err != nil {
		a.fail(ctx, chatID, err)
		return
	}
	o, err := a.st.CreateBooking(ctx, u, s.plate, svc, s.slot, a.cfg.Boxes, time.Duration(a.cfg.SlotMinutes)*time.Minute)
	if errors.Is(err, store.ErrSlotTaken) {
		text, kb, err := a.slotsView(ctx, svc, a.dayStart(s.slot))
		if err != nil {
			a.fail(ctx, chatID, err)
			return
		}
		a.edit(ctx, cq, "😔 Sorry, that time was just taken.\n\n"+text, kb)
		return
	}
	if err != nil {
		a.fail(ctx, chatID, err)
		return
	}
	a.clearState(cq.From.ID)
	a.edit(ctx, cq, "🎉 <b>Booked!</b>\n\n"+a.orderCard(o, false)+"\n\nI'll remind you 1 hour before.", noKeyboard())
	a.notifyAdmins(ctx, "🆕 <b>New booking</b>\n\n"+a.orderCard(o, true), adminOrderKB(o))
}

func (a *App) clientCancel(ctx context.Context, cq *models.CallbackQuery, arg string) {
	id, _ := strconv.ParseInt(arg, 10, 64)
	o, err := a.st.OrderByID(ctx, id)
	if err != nil || o.ClientTelegramID == nil || *o.ClientTelegramID != cq.From.ID {
		a.answer(ctx, cq, "Order not found", true)
		return
	}
	o, err = a.st.SetStatus(ctx, id, []string{store.StatusBooked}, store.StatusCancelled, nil)
	if errors.Is(err, store.ErrNotFound) {
		a.answer(ctx, cq, "This booking can't be cancelled anymore", true)
		return
	}
	if err != nil {
		a.fail(ctx, cq.From.ID, err)
		return
	}
	a.edit(ctx, cq, a.orderCard(o, false), noKeyboard())
	a.notifyAdmins(ctx, "❌ <b>Client cancelled booking</b>\n\n"+a.orderCard(o, true), nil)
}
