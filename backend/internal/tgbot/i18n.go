package tgbot

import (
	"fmt"
	"strings"
	"time"
)

const (
	langUz = "uz"
	langRu = "ru"
	langEn = "en"

	defaultLang = langUz
)

var langs = []string{langUz, langRu, langEn}

// resolveLang maps a Telegram language_code ("ru", "en-US", "uz", …) to a
// supported language; anything else falls back to Uzbek.
func resolveLang(code string) string {
	code = strings.ToLower(code)
	for _, l := range langs {
		if code == l || strings.HasPrefix(code, l+"-") {
			return l
		}
	}
	return defaultLang
}

func isLang(l string) bool {
	for _, x := range langs {
		if x == l {
			return true
		}
	}
	return false
}

func langIndex(l string) int {
	switch l {
	case langRu:
		return 1
	case langEn:
		return 2
	}
	return 0
}

// T returns the translation of key in lang (uz, ru, en), formatted with args.
func T(lang, key string, args ...any) string {
	m, ok := msgs[key]
	if !ok {
		return key
	}
	s := m[langIndex(lang)]
	if len(args) > 0 {
		return fmt.Sprintf(s, args...)
	}
	return s
}

// buttonKey finds which reply-keyboard button was pressed, in any language,
// so an old keyboard keeps working after the user switches language.
func buttonKey(text string) string {
	for _, k := range replyButtons {
		for _, s := range msgs[k] {
			if s == text {
				return k
			}
		}
	}
	return ""
}

var replyButtons = []string{
	"btn_book", "btn_orders", "btn_contacts", "btn_lang",
	"btn_today", "btn_upcoming", "btn_walkin", "btn_cancel",
}

var langNames = map[string]string{langUz: "🇺🇿 O‘zbekcha", langRu: "🇷🇺 Русский", langEn: "🇬🇧 English"}

// ---------- dates ----------

var weekdays = [3][7]string{ // Sunday first, like time.Weekday
	{"Ya", "Du", "Se", "Ch", "Pa", "Ju", "Sh"},
	{"Вс", "Пн", "Вт", "Ср", "Чт", "Пт", "Сб"},
	{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"},
}

var months = [3][12]string{
	{"yan", "fev", "mar", "apr", "may", "iyn", "iyl", "avg", "sen", "okt", "noy", "dek"},
	{"янв", "фев", "мар", "апр", "мая", "июн", "июл", "авг", "сен", "окт", "ноя", "дек"},
	{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"},
}

// fmtDate: "Du, 03 noy" / "Пн, 03 ноя" / "Mon, 03 Nov" (t must already be local).
func fmtDate(lang string, t time.Time) string {
	i := langIndex(lang)
	return fmt.Sprintf("%s, %02d %s", weekdays[i][t.Weekday()], t.Day(), months[i][t.Month()-1])
}

// fmtDayButton: "Du 03.11"
func fmtDayButton(lang string, t time.Time) string {
	return weekdays[langIndex(lang)][t.Weekday()] + " " + t.Format("02.01")
}

// ---------- catalog: {uz, ru, en} ----------

var msgs = map[string][3]string{
	// reply keyboard
	"btn_book":     {"📅 Yuvishga yozilish", "📅 Записаться", "📅 Book a wash"},
	"btn_orders":   {"🚗 Buyurtmalarim", "🚗 Мои заказы", "🚗 My orders"},
	"btn_contacts": {"📍 Aloqa", "📍 Контакты", "📍 Contacts"},
	"btn_lang":     {"🌐 Til", "🌐 Язык", "🌐 Language"},
	"btn_today":    {"📋 Bugun", "📋 Сегодня", "📋 Today"},
	"btn_upcoming": {"🗓 Kelgusi", "🗓 Предстоящие", "🗓 Upcoming"},
	"btn_walkin":   {"➕ Navbatsiz mashina", "➕ Машина без записи", "➕ Walk-in car"},
	"btn_cancel":   {"✖️ Bekor qilish", "✖️ Отмена", "✖️ Cancel"},
	"btn_phone":    {"📱 Telefon raqamni yuborish", "📱 Отправить номер телефона", "📱 Share phone number"},

	// bot commands menu
	"cmd_start":  {"Asosiy menyu", "Главное меню", "Main menu"},
	"cmd_orders": {"Mashinalarim va yozilishlarim", "Мои машины и записи", "My cars & bookings"},
	"cmd_book":   {"Yuvishga yozilish", "Записаться на мойку", "Book a wash"},
	"cmd_lang":   {"Tilni o‘zgartirish", "Сменить язык", "Change language"},
	"cmd_cancel": {"Joriy amalni bekor qilish", "Отменить текущее действие", "Cancel current action"},
	"cmd_myid":   {"Telegram ID’ni ko‘rsatish", "Показать мой Telegram ID", "Show my Telegram ID"},

	// general
	"cancelled":   {"Bekor qilindi.", "Отменено.", "OK, cancelled."},
	"myid":        {"Sizning Telegram ID: <code>%d</code>", "Ваш Telegram ID: <code>%d</code>", "Your Telegram ID: <code>%d</code>"},
	"choose_menu": {"Menyudan tanlang 👇", "Выберите действие в меню 👇", "Choose an option from the menu 👇"},
	"admins_only": {"Faqat adminlar uchun", "Только для админов", "Admins only"},
	"error":       {"⚠️ Xatolik yuz berdi, qaytadan urinib ko‘ring.", "⚠️ Что-то пошло не так, попробуйте ещё раз.", "⚠️ Something went wrong, please try again."},
	"choose_lang": {"🌐 Tilni tanlang:", "🌐 Выберите язык:", "🌐 Choose language:"},
	"lang_set":    {"✅ Til: O‘zbekcha", "✅ Язык: Русский", "✅ Language: English"},
	"today":       {"Bugun", "Сегодня", "Today"},
	"tomorrow":    {"Ertaga", "Завтра", "Tomorrow"},
	"svc_label":   {"%s · %d daq · %s", "%s · %d мин · %s", "%s · %d min · %s"},
	"hours":       {"🕘 Har kuni %d:00 – %d:00", "🕘 Ежедневно %d:00 – %d:00", "🕘 Every day %d:00 – %d:00"},
	"reminder": {
		"⏰ <b>Eslatma</b>\nMashinangizni yuvish soat <b>%s</b>da boshlanadi (~%d daqiqadan so‘ng).\n🚘 %s · %s",
		"⏰ <b>Напоминание</b>\nМойка начнётся в <b>%s</b> (через ~%d мин).\n🚘 %s · %s",
		"⏰ <b>Reminder</b>\nYour wash starts at <b>%s</b> (in ~%d min).\n🚘 %s · %s",
	},

	// order status
	"st_booked":      {"📅 Yozilgan: <b>%s, %s</b>", "📅 Запись: <b>%s, %s</b>", "📅 Booked for <b>%s, %s</b>"},
	"st_waiting":     {"🚗 Mashina moykada, navbatda", "🚗 Машина на мойке, в очереди", "🚗 At the wash, waiting in queue"},
	"st_washing":     {"🧽 Yuvilmoqda", "🧽 Моется", "🧽 Washing now"},
	"st_washing_eta": {"🧽 Yuvilmoqda — taxminan soat <b>%s</b>da tayyor", "🧽 Моется — будет готова примерно в <b>%s</b>", "🧽 Washing — ready at about <b>%s</b>"},
	"st_left":        {" (~%d daq)", " (~%d мин)", " (~%d min)"},
	"st_ready":       {"✅ <b>Tayyor!</b> Mashinangizni olib ketishingiz mumkin", "✅ <b>Готово!</b> Можете забирать машину", "✅ <b>Ready!</b> You can pick up your car"},
	"st_done":        {"🏁 Olib ketildi", "🏁 Выдана", "🏁 Picked up"},
	"st_cancelled":   {"❌ Bekor qilingan", "❌ Отменён", "❌ Cancelled"},
	"unlinked":       {"navbatsiz, botga ulanmagan", "без записи, не привязан", "walk-in, not linked"},

	// client: start / phone / orders
	"code_not_found": {"Bu buyurtma kodi topilmadi yoki boshqa akkauntga bog‘langan.", "Код заказа не найден или привязан к другому аккаунту.", "This order code was not found or is linked to another account."},
	"tracked_hi":     {"👋 Salom! Mashinangiz holati o‘zgarishi bilan xabar beraman — qo‘ng‘iroq qilish shart emas.", "👋 Здравствуйте! Я сообщу, как только статус машины изменится — звонить не нужно.", "👋 Hi! I'll message you as soon as your car's status changes — no need to call."},
	"welcome": {
		"👋 <b>CWash’ga xush kelibsiz!</b>\n\n• 📅 Bo‘sh vaqtga yuvishga yoziling\n• 🚗 Mashinangiz holatini kuzating — tayyor bo‘lganda xabar beraman\n\nEndi qo‘ng‘iroq qilish shart emas 🙂",
		"👋 <b>Добро пожаловать в CWash!</b>\n\n• 📅 Записывайтесь на мойку в свободное время\n• 🚗 Следите за статусом машины — я сообщу, когда она будет готова\n\nБольше никаких звонков 🙂",
		"👋 <b>Welcome to CWash!</b>\n\n• 📅 Book a wash at a free time\n• 🚗 See your car's status live — I'll notify you when it's ready\n\nNo more phone calls 🙂",
	},
	"welcome_admin":      {"\n\n🛠 Siz <b>adminsiz</b>: %s va %s tugmalaridan foydalaning.", "\n\n🛠 Вы <b>администратор</b>: используйте %s и %s.", "\n\n🛠 You are an <b>admin</b>: use %s and %s."},
	"share_own_phone":    {"Iltimos, pastdagi tugma orqali <b>o‘zingizning</b> raqamingizni yuboring.", "Пожалуйста, отправьте <b>свой</b> номер кнопкой ниже.", "Please share <b>your own</b> number with the button below."},
	"phone_saved":        {"✅ Rahmat, raqam saqlandi.", "✅ Спасибо, номер сохранён.", "✅ Thanks, phone saved."},
	"found_car":          {"Mashinangiz moykada topildi:", "Мы нашли вашу машину на мойке:", "We found your car at the wash:"},
	"no_orders":          {"Faol buyurtmalaringiz yo‘q.\nVaqt band qilish uchun <b>%s</b> tugmasini bosing.", "У вас нет активных заказов.\nНажмите <b>%s</b>, чтобы записаться.", "You have no active orders.\nTap <b>%s</b> to reserve a time."},
	"btn_cancel_booking": {"❌ Yozilishni bekor qilish", "❌ Отменить запись", "❌ Cancel booking"},
	"btn_refresh":        {"🔄 Yangilash", "🔄 Обновить", "🔄 Refresh"},
	"order_not_found":    {"Buyurtma topilmadi", "Заказ не найден", "Order not found"},
	"cant_cancel":        {"Bu yozilishni endi bekor qilib bo‘lmaydi", "Эту запись уже нельзя отменить", "This booking can't be cancelled anymore"},

	// client: booking flow
	"need_phone":          {"Yozilish uchun telefon raqamingizni yuboring — kerak bo‘lsa moyka siz bilan bog‘lanadi.", "Для записи отправьте номер телефона — чтобы мойка могла связаться с вами при необходимости.", "To book, please share your phone number so the wash can reach you if needed."},
	"choose_service":      {"🧼 <b>Xizmatni tanlang:</b>", "🧼 <b>Выберите услугу:</b>", "🧼 <b>Choose a service:</b>"},
	"choose_day":          {"📅 <b>Kunni tanlang:</b>", "📅 <b>Выберите день:</b>", "📅 <b>Choose a day:</b>"},
	"choose_time":         {"🕐 <b>Vaqtni tanlang:</b>", "🕐 <b>Выберите время:</b>", "🕐 <b>Choose a time:</b>"},
	"btn_back":            {"⬅️ Orqaga", "⬅️ Назад", "⬅️ Back"},
	"btn_other_day":       {"⬅️ Boshqa kun", "⬅️ Другой день", "⬅️ Other day"},
	"no_free":             {"😔 <b>%s</b> kuni bo‘sh vaqt yo‘q. Boshqa kunni tanlang.", "😔 На <b>%s</b> нет свободного времени. Выберите другой день.", "😔 No free time on <b>%s</b>. Try another day."},
	"service_unavailable": {"Xizmat mavjud emas", "Услуга недоступна", "Service unavailable"},
	"time_passed":         {"Bu vaqt o‘tib ketdi, boshqasini tanlang", "Это время уже прошло, выберите другое", "This time has passed, choose another"},
	"send_plate":          {"🚘 <b>Mashina davlat raqamini yuboring</b> (masalan, 01A123BC):", "🚘 <b>Отправьте госномер машины</b> (например, 01A123BC):", "🚘 <b>Send your car plate number</b> (e.g. 01A123BC):"},
	"bad_plate":           {"Bu davlat raqamiga o‘xshamaydi. Qaytadan yuboring, masalan <code>01A123BC</code>", "Это не похоже на госномер. Попробуйте ещё раз, например <code>01A123BC</code>", "That doesn't look like a plate number. Try again, e.g. <code>01A123BC</code>"},
	"start_again":         {"Iltimos, qaytadan boshlang: %s", "Пожалуйста, начните заново: %s", "Please start again: %s"},
	"confirm_title":       {"<b>Yozilishni tasdiqlang:</b>", "<b>Подтвердите запись:</b>", "<b>Please confirm your booking:</b>"},
	"btn_confirm":         {"✅ Tasdiqlash", "✅ Подтвердить", "✅ Confirm"},
	"booking_cancelled":   {"Yozilish bekor qilindi.", "Запись отменена.", "Booking cancelled."},
	"booking_expired":     {"Bu yozilish eskirdi, qaytadan boshlang.", "Запись устарела, начните заново.", "This booking expired, please start again."},
	"slot_taken":          {"😔 Kechirasiz, bu vaqt hozirgina band qilindi.", "😔 Извините, это время только что заняли.", "😔 Sorry, that time was just taken."},
	"booked":              {"🎉 <b>Yozildingiz!</b>", "🎉 <b>Вы записаны!</b>", "🎉 <b>Booked!</b>"},
	"remind_note":         {"1 soat oldin eslatib qo‘yaman.", "Напомню за 1 час.", "I'll remind you 1 hour before."},

	// admin
	"adm_new_booking":      {"🆕 <b>Yangi yozilish</b>", "🆕 <b>Новая запись</b>", "🆕 <b>New booking</b>"},
	"adm_client_cancelled": {"❌ <b>Mijoz yozilishni bekor qildi</b>", "❌ <b>Клиент отменил запись</b>", "❌ <b>Client cancelled booking</b>"},
	"btn_arrived":          {"🚗 Keldi", "🚗 Приехал", "🚗 Arrived"},
	"btn_start":            {"🧽 Boshlash", "🧽 Начать", "🧽 Start"},
	"btn_start_washing":    {"🧽 Yuvishni boshlash", "🧽 Начать мойку", "🧽 Start washing"},
	"btn_cancel_order":     {"❌ Bekor qilish", "❌ Отменить", "❌ Cancel"},
	"btn_plus10":           {"⏱ +10 daq", "⏱ +10 мин", "⏱ +10 min"},
	"btn_ready":            {"✅ Tayyor", "✅ Готово", "✅ Ready"},
	"btn_picked":           {"🏁 Olib ketildi", "🏁 Выдана", "🏁 Picked up"},
	"btn_skip":             {"O‘tkazib yuborish ➡️", "Пропустить ➡️", "Skip ➡️"},
	"today_empty":          {"📋 Bugun uchun hali hech narsa yo‘q.", "📋 На сегодня пока ничего нет.", "📋 Nothing for today yet."},
	"today_header":         {"📋 <b>Bugun: %d ta faol</b>", "📋 <b>Сегодня активных: %d</b>", "📋 <b>Today: %d active</b>"},
	"upcoming_empty":       {"🗓 Ertadan boshlab yozilishlar yo‘q.", "🗓 Записей после сегодняшнего дня нет.", "🗓 No bookings after today."},
	"upcoming_header":      {"🗓 <b>Kelgusi yozilishlar</b>", "🗓 <b>Предстоящие записи</b>", "🗓 <b>Upcoming bookings</b>"},
	"walkin_title":         {"➕ <b>Navbatsiz mashina</b>\n\n🚘 Davlat raqamini yuboring:", "➕ <b>Машина без записи</b>\n\n🚘 Отправьте госномер:", "➕ <b>New walk-in car</b>\n\n🚘 Send the car plate number:"},
	"walkin_restart":       {"%s orqali qaytadan boshlang", "Начните заново: %s", "Start again with %s"},
	"phone_short":          {"Raqam juda qisqa. <code>90 123 45 67</code> ko‘rinishida yuboring yoki «O‘tkazib yuborish»ni bosing.", "Номер слишком короткий. Отправьте как <code>90 123 45 67</code> или нажмите «Пропустить».", "Phone looks too short. Send it like <code>90 123 45 67</code> or tap Skip."},
	"walkin_phone_prompt": {
		"📱 Mijozning <b>telefon raqamini</b> yuboring — agar u botdan foydalansa, avtomatik xabar oladi.\n\nYoki o‘tkazib yuborib, unga QR kodni ko‘rsating.",
		"📱 Отправьте <b>номер телефона</b> клиента — если он уже пользуется ботом, получит уведомление автоматически.\n\nИли пропустите и покажите ему QR-код.",
		"📱 Send the client's <b>phone number</b> — if they already use this bot, they'll be notified automatically.\n\nOr skip and show them the QR code.",
	},
	"walkin_registered":   {"✅ Mashina ro‘yxatga olindi.", "✅ Машина зарегистрирована.", "✅ Car registered."},
	"walkin_client_found": {"👤 Mijoz <b>%s</b> raqam orqali topildi va xabardor qilindi.", "👤 Клиент <b>%s</b> найден по номеру и уведомлён.", "👤 Client <b>%s</b> found by phone and notified."},
	"walkin_scan_qr":      {"📲 Mashinani kuzatish uchun mijozdan <b>QR kodni skanerlashni</b> so‘rang:\n%s", "📲 Попросите клиента <b>отсканировать QR-код</b>, чтобы следить за машиной:\n%s", "📲 Ask the client to <b>scan this QR</b> to track the car:\n%s"},
	"walkin_client_msg":   {"👋 Mashinangiz CWash’da qabul qilindi. Tayyor bo‘lganda xabar beraman.", "👋 Ваша машина принята в CWash. Я сообщу, когда она будет готова.", "👋 Your car was accepted at CWash. I'll message you when it's ready."},
	"bad_request":         {"Noto‘g‘ri so‘rov", "Неверный запрос", "Bad request"},
	"unknown_action":      {"Noma’lum amal", "Неизвестное действие", "Unknown action"},
	"already_updated":     {"Boshqa xodim allaqachon yangilagan", "Уже обновлено другим сотрудником", "Already updated by someone else"},
	"saved":               {"Saqlandi ✅", "Сохранено ✅", "Saved ✅"},

	// client notifications on status change
	"n_arrive": {"🚗 Mashinangiz <b>%s</b> qabul qilindi. U navbatda — yuvish boshlanganda xabar beraman.", "🚗 Мы приняли вашу машину <b>%s</b>. Она в очереди — сообщу, когда начнётся мойка.", "🚗 We've got your car <b>%s</b>. It's in the queue — I'll tell you when washing starts."},
	"n_start":  {"🧽 <b>%s</b> yuvilmoqda.\nTaxminan soat <b>%s</b>da tayyor bo‘ladi.", "🧽 Мойка <b>%s</b> началась.\nБудет готова примерно в <b>%s</b>.", "🧽 Washing started for <b>%s</b>.\nReady at about <b>%s</b>."},
	"n_plus10": {"⏱ <b>%s</b> uchun biroz ko‘proq vaqt kerak.\nYangi tayyor bo‘lish vaqti: <b>%s</b>.", "⏱ Для <b>%s</b> нужно чуть больше времени.\nНовое время готовности: <b>%s</b>.", "⏱ A bit more time needed for <b>%s</b>.\nNew ready time: <b>%s</b>."},
	"n_ready":  {"✅ <b>Mashinangiz %s tayyor!</b>\nHozir olib ketishingiz mumkin.", "✅ <b>Ваша машина %s готова!</b>\nМожете забирать.", "✅ <b>Your car %s is ready!</b>\nYou can pick it up now."},
	"n_done":   {"🙏 CWash’ni tanlaganingiz uchun rahmat! Yana kutib qolamiz.\nKeyingi yuvishga yozilish uchun %s tugmasini bosing.", "🙏 Спасибо, что выбрали CWash! Ждём вас снова.\nНажмите %s, чтобы записаться снова.", "🙏 Thanks for choosing CWash! See you next time.\nTap %s to reserve your next wash."},
	"n_cancel": {"❌ <b>%s</b> bo‘yicha buyurtmangiz moyka tomonidan bekor qilindi. Xato bo‘lsa, biz bilan bog‘laning.", "❌ Ваш заказ на <b>%s</b> отменён мойкой. Если это ошибка — свяжитесь с нами.", "❌ Your order for <b>%s</b> was cancelled by the wash. Contact us if this is a mistake."},
}
