package tgbot

import (
	"regexp"
	"testing"
	"time"
)

var verbRe = regexp.MustCompile(`%[a-z]`)

func TestCatalogComplete(t *testing.T) {
	for key, tr := range msgs {
		want := verbRe.FindAllString(tr[2], -1) // English as reference
		for i, s := range tr {
			if s == "" {
				t.Errorf("%s[%s] is empty", key, langs[i])
				continue
			}
			got := verbRe.FindAllString(s, -1)
			if len(got) != len(want) {
				t.Errorf("%s[%s] has placeholders %v, English has %v", key, langs[i], got, want)
				continue
			}
			for j := range got {
				if got[j] != want[j] {
					t.Errorf("%s[%s] placeholder order %v, English %v", key, langs[i], got, want)
				}
			}
		}
	}
}

func TestReplyButtonsUnique(t *testing.T) {
	seen := map[string]string{}
	for _, k := range replyButtons {
		for _, s := range msgs[k] {
			if other, ok := seen[s]; ok && other != k {
				t.Errorf("button text %q used by %s and %s", s, k, other)
			}
			seen[s] = k
		}
	}
	if buttonKey("🌐 Язык") != "btn_lang" || buttonKey("✖️ Bekor qilish") != "btn_cancel" {
		t.Error("buttonKey lookup failed")
	}
}

func TestResolveLang(t *testing.T) {
	cases := map[string]string{
		"ru": langRu, "en": langEn, "en-US": langEn, "uz": langUz, "RU": langRu,
		"": langUz, "kk": langUz, "de": langUz, "tr": langUz,
	}
	for in, want := range cases {
		if got := resolveLang(in); got != want {
			t.Errorf("resolveLang(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFmtDate(t *testing.T) {
	d := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC) // Tuesday
	for l, want := range map[string]string{langUz: "Se, 03 noy", langRu: "Вт, 03 ноя", langEn: "Tue, 03 Nov"} {
		if got := fmtDate(l, d); got != want {
			t.Errorf("fmtDate(%s) = %q, want %q", l, got, want)
		}
	}
}
