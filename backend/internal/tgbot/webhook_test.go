package tgbot

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWebhookRouting(t *testing.T) {
	secret := webhookSecret("123:abc")
	if len(secret) != 48 || secret == webhookSecret("123:abd") {
		t.Fatalf("bad secret %q", secret)
	}

	hits := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	mux.HandleFunc("POST "+webhookPath, secretGuard(secret, func(http.ResponseWriter, *http.Request) { hits++ }))

	do := func(method, path, hdr string) int {
		r := httptest.NewRequest(method, path, nil)
		if hdr != "" {
			r.Header.Set("X-Telegram-Bot-Api-Secret-Token", hdr)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w.Code
	}

	if c := do("POST", webhookPath, secret); c != 200 || hits != 1 {
		t.Errorf("valid webhook: code %d hits %d", c, hits)
	}
	if c := do("POST", webhookPath, "wrong"); c != 403 || hits != 1 {
		t.Errorf("wrong secret: code %d hits %d", c, hits)
	}
	if c := do("POST", webhookPath, ""); c != 403 {
		t.Errorf("no secret: code %d", c)
	}
	if c := do("GET", "/", ""); c != 200 {
		t.Errorf("health: code %d", c)
	}
}
