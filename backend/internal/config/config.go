package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	BotToken    string
	DatabaseURL string
	AdminIDs    map[int64]bool
	Location    *time.Location

	OpenHour    int // first slot start, e.g. 9
	CloseHour   int // all washes must finish by this hour, e.g. 21
	SlotMinutes int // booking grid step
	Boxes       int // how many cars can be washed in parallel
	BookingDays int // how many days ahead clients can book

	Currency string
	Address  string
	Phone    string

	// WebhookURL is the app's public base URL (e.g. https://x.laravel.cloud).
	// Set → Telegram pushes updates to <WebhookURL>/telegram/webhook.
	// Empty → long polling (local development).
	WebhookURL string
}

func Load() (*Config, error) {
	_ = godotenv.Load() // .env is optional; real env vars win

	c := &Config{
		BotToken:    os.Getenv("BOT_TOKEN"),
		DatabaseURL: env("DATABASE_URL", "postgres://cwash:cwash@localhost:5434/cwash?sslmode=disable"),
		AdminIDs:    map[int64]bool{},
		Currency:    env("CURRENCY", "UZS"),
		Address:     env("WASH_ADDRESS", ""),
		Phone:       env("WASH_PHONE", ""),
		WebhookURL:  strings.TrimRight(env("WEBHOOK_URL", ""), "/"),
	}
	if c.BotToken == "" {
		return nil, errors.New("BOT_TOKEN is required")
	}
	if c.WebhookURL != "" && !strings.HasPrefix(c.WebhookURL, "https://") {
		return nil, errors.New("WEBHOOK_URL must start with https://")
	}

	var err error
	if c.Location, err = time.LoadLocation(env("TIMEZONE", "Asia/Tashkent")); err != nil {
		return nil, fmt.Errorf("TIMEZONE: %w", err)
	}
	ints := []struct {
		dst  *int
		key  string
		def  int
		min_ int
	}{
		{&c.OpenHour, "OPEN_HOUR", 9, 0},
		{&c.CloseHour, "CLOSE_HOUR", 21, 1},
		{&c.SlotMinutes, "SLOT_MINUTES", 30, 5},
		{&c.Boxes, "BOXES", 2, 1},
		{&c.BookingDays, "BOOKING_DAYS", 7, 1},
	}
	for _, i := range ints {
		v, err := strconv.Atoi(env(i.key, strconv.Itoa(i.def)))
		if err != nil || v < i.min_ {
			return nil, fmt.Errorf("%s: invalid value", i.key)
		}
		*i.dst = v
	}
	if c.CloseHour <= c.OpenHour || c.CloseHour > 24 {
		return nil, errors.New("CLOSE_HOUR must be after OPEN_HOUR and <= 24")
	}

	for _, s := range strings.Split(os.Getenv("ADMIN_IDS"), ",") {
		if s = strings.TrimSpace(s); s == "" {
			continue
		}
		id, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("ADMIN_IDS: bad id %q", s)
		}
		c.AdminIDs[id] = true
	}
	return c, nil
}

func env(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}
