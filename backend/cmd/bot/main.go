package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	_ "time/tzdata" // works on servers without system timezone data

	"github.com/akramjon/cwash/backend/internal/config"
	"github.com/akramjon/cwash/backend/internal/store"
	"github.com/akramjon/cwash/backend/internal/tgbot"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer st.Close()

	app, err := tgbot.New(ctx, cfg, st)
	if err != nil {
		log.Fatalf("telegram: %v", err)
	}
	if len(cfg.AdminIDs) == 0 {
		log.Printf("WARNING: ADMIN_IDS is empty — send /myid to the bot and put your ID in .env")
	}
	log.Printf("CWash bot @%s started (boxes=%d, hours %d-%d, tz=%s)",
		app.Username(), cfg.Boxes, cfg.OpenHour, cfg.CloseHour, cfg.Location)
	app.Run(ctx)
	log.Printf("stopped")
}
