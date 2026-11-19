// Command x4-notifier receives finished-order events from the Fleet Order
// Notifier X4 extension and forwards them to ntfy and/or Telegram.
package main

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

const (
	maxBodyBytes    = 8 << 10
	maxFieldRunes   = 160
	providerTimeout = 15 * time.Second
)

// OrderEvent is the JSON body posted by fleet_notifier/ui/notifier.lua.
type OrderEvent struct {
	Code   string `json:"code"`
	Ship   string `json:"ship"`
	Order  string `json:"order"`
	Sector string `json:"sector"`
}

type config struct {
	listen        string
	token         string
	ntfyURL       string
	ntfyTopic     string
	ntfyToken     string
	telegramBase  string
	telegramToken string
	telegramChat  string
}

func loadConfig() config {
	return config{
		listen:        envOrDefault("X4_LISTEN", "127.0.0.1:8080"),
		token:         os.Getenv("X4_TOKEN"),
		ntfyURL:       strings.TrimRight(envOrDefault("NTFY_URL", "https://ntfy.sh"), "/"),
		ntfyTopic:     os.Getenv("NTFY_TOPIC"),
		ntfyToken:     os.Getenv("NTFY_TOKEN"),
		telegramBase:  strings.TrimRight(envOrDefault("TELEGRAM_API_BASE", "https://api.telegram.org"), "/"),
		telegramToken: os.Getenv("TELEGRAM_BOT_TOKEN"),
		telegramChat:  os.Getenv("TELEGRAM_CHAT_ID"),
	}
}

func (c config) ntfyEnabled() bool { return c.ntfyTopic != "" }
func (c config) telegramEnabled() bool {
	return c.telegramToken != "" && c.telegramChat != ""
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

type notifier struct {
	cfg    config
	client *http.Client
	logger *log.Logger
}

func newNotifier(cfg config, logger *log.Logger) *notifier {
	return &notifier{
		cfg:    cfg,
		client: &http.Client{Timeout: providerTimeout},
		logger: logger,
	}
}

func (n *notifier) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /x4/order-finished", n.handleOrderFinished)
	mux.HandleFunc("GET /health", n.handleHealth)
	return mux
}

func (n *notifier) handleOrderFinished(w http.ResponseWriter, r *http.Request) {
	if !n.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	var event OrderEvent
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err := decoder.Decode(&event); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "body too large"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}

	event = event.sanitized()
	if event.Ship == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "ship is required"})
		return
	}

	n.logger.Printf("event ship=%q order=%q sector=%q code=%q", event.Ship, event.Order, event.Sector, event.Code)

	// Answer at once: the Lua side should never wait on ntfy or Telegram.
	go n.dispatch(event)

	writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}

func (n *notifier) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":   "ok",
		"ntfy":     n.cfg.ntfyEnabled(),
		"telegram": n.cfg.telegramEnabled(),
		"auth":     n.cfg.token != "",
	})
}

// authorized accepts "Authorization: Bearer <token>" or "X-Auth-Token: <token>".
func (n *notifier) authorized(r *http.Request) bool {
	if n.cfg.token == "" {
		return true
	}

	var given string
	const prefix = "Bearer "
	if header := r.Header.Get("Authorization"); len(header) > len(prefix) && strings.EqualFold(header[:len(prefix)], prefix) {
		given = strings.TrimSpace(header[len(prefix):])
	}
	if given == "" {
		given = strings.TrimSpace(r.Header.Get("X-Auth-Token"))
	}

	return subtle.ConstantTimeCompare([]byte(given), []byte(n.cfg.token)) == 1
}

func (n *notifier) dispatch(event OrderEvent) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*providerTimeout)
	defer cancel()

	title, body := formatMessage(event)

	if n.cfg.ntfyEnabled() {
		if err := n.sendNtfy(ctx, title, body); err != nil {
			n.logger.Printf("ntfy: %v", err)
		}
	}
	if n.cfg.telegramEnabled() {
		if err := n.sendTelegram(ctx, title, body); err != nil {
			n.logger.Printf("telegram: %v", err)
		}
	}
}

func formatMessage(event OrderEvent) (title, body string) {
	title = fmt.Sprintf("Ordem concluída: %s", event.Ship)

	order := event.Order
	if order == "" {
		order = "desconhecida"
	}
	sector := event.Sector
	if sector == "" {
		sector = "desconhecido"
	}

	lines := []string{"Ordem: " + order, "Setor: " + sector}
	if event.Code != "" {
		lines = append(lines, "Código: "+event.Code)
	}
	return title, strings.Join(lines, "\n")
}

func (n *notifier) sendNtfy(ctx context.Context, title, body string) error {
	payload := map[string]any{
		"topic":   n.cfg.ntfyTopic,
		"title":   title,
		"message": body,
		"tags":    []string{"rocket"},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.cfg.ntfyURL, bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if n.cfg.ntfyToken != "" {
		req.Header.Set("Authorization", "Bearer "+n.cfg.ntfyToken)
	}

	return n.do(req, "ntfy")
}

func (n *notifier) sendTelegram(ctx context.Context, title, body string) error {
	url := fmt.Sprintf("%s/bot%s/sendMessage", n.cfg.telegramBase, n.cfg.telegramToken)
	// No parse_mode on purpose: ship names contain "_" and "*", which the
	// Markdown flavors reject with a 400.
	payload := map[string]any{
		"chat_id":                  n.cfg.telegramChat,
		"text":                     title + "\n" + body,
		"disable_web_page_preview": true,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	return n.do(req, "telegram")
}

func (n *notifier) do(req *http.Request, provider string) error {
	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("%s request: %w", provider, err)
	}
	defer resp.Body.Close()

	// Drain a little so the connection can be reused, and keep a snippet for errors.
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(snippet)))
	}
	return nil
}

func (e OrderEvent) sanitized() OrderEvent {
	return OrderEvent{
		Code:   trim(e.Code),
		Ship:   trim(e.Ship),
		Order:  trim(e.Order),
		Sector: trim(e.Sector),
	}
}

// trim removes surrounding space, folds newlines (they would break the
// notification layout) and caps the length.
func trim(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) > maxFieldRunes {
		value = string(runes[:maxFieldRunes]) + "…"
	}
	return value
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func main() {
	logger := log.New(os.Stdout, "", log.LstdFlags)
	cfg := loadConfig()
	n := newNotifier(cfg, logger)

	if !cfg.ntfyEnabled() && !cfg.telegramEnabled() {
		logger.Print("warning: no provider configured; set NTFY_TOPIC and/or TELEGRAM_BOT_TOKEN + TELEGRAM_CHAT_ID")
	}
	if cfg.token == "" {
		logger.Print("warning: X4_TOKEN is empty, the endpoint accepts any request")
	}

	srv := &http.Server{
		Addr:              cfg.listen,
		Handler:           n.routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	logger.Printf("listening on http://%s (ntfy=%t telegram=%t auth=%t)",
		cfg.listen, cfg.ntfyEnabled(), cfg.telegramEnabled(), cfg.token != "")

	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Fatalf("server: %v", err)
	}
}
