package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type capturedRequest struct {
	header http.Header
	body   []byte
}

// fakeProvider records every request it receives.
func fakeProvider(t *testing.T) (*httptest.Server, <-chan capturedRequest) {
	t.Helper()
	ch := make(chan capturedRequest, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		ch <- capturedRequest{header: r.Header.Clone(), body: body}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv, ch
}

func waitFor(t *testing.T, ch <-chan capturedRequest) capturedRequest {
	t.Helper()
	select {
	case got := <-ch:
		return got
	case <-time.After(2 * time.Second):
		t.Fatal("provider was not called")
		return capturedRequest{}
	}
}

func testNotifier(cfg config) *notifier {
	return newNotifier(cfg, log.New(io.Discard, "", 0))
}

func post(t *testing.T, h http.Handler, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/x4/order-finished", strings.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestHandleOrderFinishedRequiresToken(t *testing.T) {
	n := testNotifier(config{token: "s3cret"})
	body := `{"ship":"Titan","order":"FlyTo","sector":"Argon Prime"}`

	if rec := post(t, n.routes(), body, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("without token: got %d, want 401", rec.Code)
	}
	if rec := post(t, n.routes(), body, map[string]string{"Authorization": "Bearer wrong"}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token: got %d, want 401", rec.Code)
	}
	if rec := post(t, n.routes(), body, map[string]string{"Authorization": "Bearer s3cret"}); rec.Code != http.StatusAccepted {
		t.Fatalf("valid bearer: got %d, want 202", rec.Code)
	}
	if rec := post(t, n.routes(), body, map[string]string{"X-Auth-Token": "s3cret"}); rec.Code != http.StatusAccepted {
		t.Fatalf("valid X-Auth-Token: got %d, want 202", rec.Code)
	}
}

func TestHandleOrderFinishedRejectsBadPayloads(t *testing.T) {
	n := testNotifier(config{})
	h := n.routes()

	if rec := post(t, h, `{`, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid json: got %d, want 400", rec.Code)
	}
	if rec := post(t, h, `{"order":"FlyTo"}`, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("missing ship: got %d, want 400", rec.Code)
	}

	huge := `{"ship":"` + strings.Repeat("a", maxBodyBytes+64) + `"}`
	if rec := post(t, h, huge, nil); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized body: got %d, want 413", rec.Code)
	}
}

func TestDispatchSendsNtfyAndTelegram(t *testing.T) {
	ntfySrv, ntfyCh := fakeProvider(t)
	telegramSrv, telegramCh := fakeProvider(t)

	n := testNotifier(config{
		ntfyURL:       ntfySrv.URL,
		ntfyTopic:     "x4-fleet",
		ntfyToken:     "ntfy-secret",
		telegramBase:  telegramSrv.URL,
		telegramToken: "bot-token",
		telegramChat:  "42",
	})

	rec := post(t, n.routes(), `{"code":"ABC-123","ship":"Titan","order":"DockAt","sector":"Argon Prime"}`, nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("got %d, want 202", rec.Code)
	}

	ntfy := waitFor(t, ntfyCh)
	if got := ntfy.header.Get("Authorization"); got != "Bearer ntfy-secret" {
		t.Errorf("ntfy authorization: got %q", got)
	}
	var ntfyPayload struct {
		Topic   string   `json:"topic"`
		Title   string   `json:"title"`
		Message string   `json:"message"`
		Tags    []string `json:"tags"`
	}
	if err := json.Unmarshal(ntfy.body, &ntfyPayload); err != nil {
		t.Fatalf("ntfy payload is not json: %v", err)
	}
	if ntfyPayload.Topic != "x4-fleet" {
		t.Errorf("ntfy topic: got %q", ntfyPayload.Topic)
	}
	if !strings.Contains(ntfyPayload.Title, "Titan") {
		t.Errorf("ntfy title missing ship: %q", ntfyPayload.Title)
	}
	if !strings.Contains(ntfyPayload.Message, "Argon Prime") || !strings.Contains(ntfyPayload.Message, "ABC-123") {
		t.Errorf("ntfy message missing fields: %q", ntfyPayload.Message)
	}

	telegram := waitFor(t, telegramCh)
	var telegramPayload map[string]any
	if err := json.Unmarshal(telegram.body, &telegramPayload); err != nil {
		t.Fatalf("telegram payload is not json: %v", err)
	}
	if telegramPayload["chat_id"] != "42" {
		t.Errorf("telegram chat_id: got %v", telegramPayload["chat_id"])
	}
	if _, ok := telegramPayload["parse_mode"]; ok {
		t.Error("telegram must not set parse_mode, ship names break Markdown")
	}
	text, _ := telegramPayload["text"].(string)
	if !strings.Contains(text, "Titan") || !strings.Contains(text, "DockAt") {
		t.Errorf("telegram text missing fields: %q", text)
	}
}

func TestDispatchSkipsUnconfiguredProviders(t *testing.T) {
	ntfySrv, ntfyCh := fakeProvider(t)
	n := testNotifier(config{ntfyURL: ntfySrv.URL}) // no topic, no telegram

	if rec := post(t, n.routes(), `{"ship":"Titan"}`, nil); rec.Code != http.StatusAccepted {
		t.Fatalf("got %d, want 202", rec.Code)
	}

	select {
	case <-ntfyCh:
		t.Fatal("ntfy called without a topic")
	case <-time.After(150 * time.Millisecond):
	}
}

func TestProviderErrorsAreNotFatal(t *testing.T) {
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer failing.Close()

	n := testNotifier(config{ntfyURL: failing.URL, ntfyTopic: "x4"})
	if rec := post(t, n.routes(), `{"ship":"Titan"}`, nil); rec.Code != http.StatusAccepted {
		t.Fatalf("got %d, want 202", rec.Code)
	}
}

func TestHealth(t *testing.T) {
	n := testNotifier(config{token: "x", ntfyTopic: "x4", telegramToken: "t", telegramChat: "1"})
	rec := httptest.NewRecorder()
	n.routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rec.Code)
	}
	var health map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &health); err != nil {
		t.Fatalf("health is not json: %v", err)
	}
	for _, key := range []string{"status", "ntfy", "telegram", "auth"} {
		if _, ok := health[key]; !ok {
			t.Errorf("health missing %q", key)
		}
	}
	if health["status"] != "ok" {
		t.Errorf("status: got %v", health["status"])
	}
}

func TestSanitizedTrimsAndCaps(t *testing.T) {
	event := OrderEvent{
		Ship:   "  Argon\nTitan  ",
		Order:  strings.Repeat("x", maxFieldRunes+20),
		Sector: "",
	}.sanitized()

	if event.Ship != "Argon Titan" {
		t.Errorf("ship: got %q", event.Ship)
	}
	if got := len([]rune(event.Order)); got > maxFieldRunes+1 {
		t.Errorf("order not capped: %d runes", got)
	}
	if event.Sector != "" {
		t.Errorf("sector: got %q", event.Sector)
	}
}

func TestFormatMessageFillsMissingFields(t *testing.T) {
	title, body := formatMessage(OrderEvent{Ship: "Titan"})
	if !strings.Contains(title, "Titan") {
		t.Errorf("title: %q", title)
	}
	if !strings.Contains(body, "desconhecida") || !strings.Contains(body, "desconhecido") {
		t.Errorf("body should mark missing order/sector: %q", body)
	}
}
