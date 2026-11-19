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
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	maxBodyBytes    = 8 << 10
	maxFieldRunes   = 160
	providerTimeout = 15 * time.Second
)

// version is stamped at build time (see scripts/build-release.sh); "dev" means
// this is a local build.
var version = "dev"

// OrderEvent is the JSON body posted by fleet_notifier/ui/notifier.lua.
// Kind is "order" for a finished order and "idle" when the ship's queue emptied.
type OrderEvent struct {
	Kind    string `json:"kind"`
	Code    string `json:"code"`
	Ship    string `json:"ship"`
	Order   string `json:"order"`
	Sector  string `json:"sector"`
	Captain string `json:"captain"`
}

type config struct {
	listen         string
	token          string
	ntfyURL        string
	ntfyTopic      string
	ntfyToken      string
	gotifyURL      string
	gotifyToken    string
	gotifyPriority int
	telegramBase   string
	telegramToken  string
	telegramChat   string
}

func loadConfig() config {
	return config{
		listen:         envOrDefault("X4_LISTEN", "127.0.0.1:47100"),
		token:          os.Getenv("X4_TOKEN"),
		ntfyURL:        strings.TrimRight(envOrDefault("NTFY_URL", "https://ntfy.sh"), "/"),
		ntfyTopic:      os.Getenv("NTFY_TOPIC"),
		ntfyToken:      os.Getenv("NTFY_TOKEN"),
		gotifyURL:      strings.TrimRight(os.Getenv("GOTIFY_URL"), "/"),
		gotifyToken:    os.Getenv("GOTIFY_TOKEN"),
		gotifyPriority: envInt("GOTIFY_PRIORITY", 5),
		telegramBase:   strings.TrimRight(envOrDefault("TELEGRAM_API_BASE", "https://api.telegram.org"), "/"),
		telegramToken:  os.Getenv("TELEGRAM_BOT_TOKEN"),
		telegramChat:   os.Getenv("TELEGRAM_CHAT_ID"),
	}
}

func (c config) ntfyEnabled() bool { return c.ntfyTopic != "" }

func (c config) gotifyEnabled() bool { return c.gotifyURL != "" && c.gotifyToken != "" }

func (c config) telegramEnabled() bool {
	return c.telegramToken != "" && c.telegramChat != ""
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
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
		n.logger.Printf("rejected request from %s: token mismatch (check X4_TOKEN in .env and TOKEN in ui/notifier.lua)", r.RemoteAddr)
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

	n.logger.Printf("event kind=%q ship=%q order=%q sector=%q code=%q captain=%q", event.Kind, event.Ship, event.Order, event.Sector, event.Code, event.Captain)

	// Answer at once: the Lua side should never wait on ntfy or Telegram.
	go n.dispatch(event)

	writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}

func (n *notifier) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":   "ok",
		"version":  version,
		"ntfy":     n.cfg.ntfyEnabled(),
		"gotify":   n.cfg.gotifyEnabled(),
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
	if n.cfg.gotifyEnabled() {
		if err := n.sendGotify(ctx, title, body); err != nil {
			n.logger.Printf("gotify: %v", err)
		}
	}
	if n.cfg.telegramEnabled() {
		if err := n.sendTelegram(ctx, title, body); err != nil {
			n.logger.Printf("telegram: %v", err)
		}
	}
}

func formatMessage(event OrderEvent) (title, body string) {
	ship := event.Ship
	if ship == "" {
		ship = "desconhecida"
	}
	if event.Code != "" {
		title = "Diário de bordo da nave " + ship + " " + event.Code + ","
	} else {
		title = "Diário de bordo da nave " + ship + ","
	}

	if event.Kind == "idle" {
		if event.Captain != "" {
			body = "Capitão " + event.Captain
			if event.Code != "" {
				body += ", da nave " + ship + " " + event.Code + ","
			} else {
				body += ", da nave " + ship + ","
			}
			return title, body + " informa que todas as missões foram concluídas."
		}
		return title, "Todas as missões foram concluídas."
	}

	order := event.Order
	if order == "" {
		order = "desconhecida"
	}
	sector := event.Sector
	if sector == "" {
		sector = "desconhecido"
	}

	lines := []string{"A missão " + order + ", no setor " + sector + ", foi concluída com sucesso."}
	if event.Captain != "" {
		lines = append(lines, "Capitão "+event.Captain)
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

func (n *notifier) sendGotify(ctx context.Context, title, body string) error {
	payload := map[string]any{
		"title":    title,
		"message":  body,
		"priority": n.cfg.gotifyPriority,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.cfg.gotifyURL+"/message", bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	// Header instead of the ?token= query parameter, so the token stays out of
	// URLs and proxy logs.
	req.Header.Set("X-Gotify-Key", n.cfg.gotifyToken)

	return n.do(req, "gotify")
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
		Kind:    trim(e.Kind),
		Code:    trim(e.Code),
		Ship:    trim(e.Ship),
		Order:   trim(e.Order),
		Sector:  trim(e.Sector),
		Captain: trim(e.Captain),
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

	// Non-developers run the shipped binary: a .env next to it is the config.
	// Reading it before the environment lets a shell export still win.
	if err := loadDotEnv(".env"); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			logger.Printf("warning: cannot read .env: %v", err)
		}
	} else {
		logger.Print("configuration loaded from .env")
	}

	cfg := loadConfig()
	n := newNotifier(cfg, logger)

	if !cfg.ntfyEnabled() && !cfg.gotifyEnabled() && !cfg.telegramEnabled() {
		logger.Print("warning: no provider configured; set NTFY_TOPIC, GOTIFY_URL + GOTIFY_TOKEN, and/or TELEGRAM_BOT_TOKEN + TELEGRAM_CHAT_ID")
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

	logger.Printf("x4-notifier %s", version)
	logger.Printf("listening on http://%s (ntfy=%t gotify=%t telegram=%t auth=%t)",
		cfg.listen, cfg.ntfyEnabled(), cfg.gotifyEnabled(), cfg.telegramEnabled(), cfg.token != "")

	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Fatalf("server: %v", err)
	}
}
