package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
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

	// Hosting platforms (Laravel Cloud, Render, Fly…) health-check an HTTP port.
	// Start it first so the check passes while we connect to DB/Telegram.
	srv := startHealthServer()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

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

func startHealthServer() *http.Server {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok"))
	})
	srv := &http.Server{Addr: ":" + port, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("health server: %v", err)
		}
	}()
	log.Printf("health server listening on :%s", port)
	return srv
}
