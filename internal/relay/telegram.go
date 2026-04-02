// Package relay — Telegram bot relay adapter.
//
// Outbound: sends messages to a configured chat via the Telegram Bot API sendMessage.
// Inbound: long-polls getUpdates for /status, /approve, /summary commands.
// No external SDK — pure net/http.
package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/haepapa/kotui/pkg/models"
)

const (
	telegramAPIBase   = "https://api.telegram.org/bot"
	telegramPollTimeout = 30 // seconds, Telegram long-poll window
	telegramMaxRetry  = 3
)

// TelegramRelay implements relay.Relay for Telegram.
type TelegramRelay struct {
	token  string
	chatID string
	cmdFn  CommandFunc
	log    *slog.Logger
	client *http.Client

	mu     sync.Mutex
	offset int64
	cancel context.CancelFunc
	done   chan struct{}
}

// NewTelegramRelay creates a TelegramRelay. Call Start() to begin polling.
// chatID is the Telegram chat_id to send messages to (may be a user ID or group ID).
func NewTelegramRelay(token, chatID string, cmdFn CommandFunc, log *slog.Logger) *TelegramRelay {
	if log == nil {
		log = slog.Default()
	}
	return &TelegramRelay{
		token:  token,
		chatID: chatID,
		cmdFn:  cmdFn,
		log:    log,
		client: &http.Client{Timeout: (telegramPollTimeout + 10) * time.Second},
	}
}

// Name implements relay.Relay.
func (r *TelegramRelay) Name() string { return "telegram" }

// Send implements relay.Relay — forwards a message to the configured Telegram chat.
func (r *TelegramRelay) Send(ctx context.Context, msg models.Message) error {
	if r.token == "" || r.chatID == "" {
		return nil // unconfigured — skip silently
	}
	text := formatMessage(msg)
	return r.sendText(ctx, r.chatID, text)
}

// Start begins the long-polling goroutine for inbound commands.
// It is a no-op if the relay has no token configured.
func (r *TelegramRelay) Start() {
	if r.token == "" {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.mu.Lock()
	r.cancel = cancel
	r.done = make(chan struct{})
	r.mu.Unlock()

	go r.pollLoop(ctx)
}

// Stop gracefully shuts down the polling goroutine.
func (r *TelegramRelay) Stop() {
	r.mu.Lock()
	cancel := r.cancel
	done := r.done
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

func (r *TelegramRelay) pollLoop(ctx context.Context) {
	defer close(r.done)
	r.log.Info("telegram: polling started")
	for {
		select {
		case <-ctx.Done():
			r.log.Info("telegram: polling stopped")
			return
		default:
		}

		updates, err := r.getUpdates(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			r.log.Warn("telegram: getUpdates failed", "err", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
			continue
		}

		for _, u := range updates {
			r.handleUpdate(ctx, u)
		}
	}
}

type tgUpdate struct {
	UpdateID int64 `json:"update_id"`
	Message  *struct {
		MessageID int64 `json:"message_id"`
		Chat      struct {
			ID int64 `json:"id"`
		} `json:"chat"`
		Text string `json:"text"`
		From *struct {
			ID       int64  `json:"id"`
			Username string `json:"username"`
		} `json:"from"`
	} `json:"message"`
}

type tgGetUpdatesResp struct {
	OK     bool       `json:"ok"`
	Result []tgUpdate `json:"result"`
}

func (r *TelegramRelay) getUpdates(ctx context.Context) ([]tgUpdate, error) {
	r.mu.Lock()
	offset := r.offset
	r.mu.Unlock()

	url := fmt.Sprintf("%s%s/getUpdates?offset=%d&timeout=%d",
		telegramAPIBase, r.token, offset, telegramPollTimeout)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result tgGetUpdatesResp
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if !result.OK {
		return nil, fmt.Errorf("telegram API returned ok=false")
	}

	if len(result.Result) > 0 {
		last := result.Result[len(result.Result)-1]
		r.mu.Lock()
		r.offset = last.UpdateID + 1
		r.mu.Unlock()
	}
	return result.Result, nil
}

func (r *TelegramRelay) handleUpdate(ctx context.Context, u tgUpdate) {
	if u.Message == nil || u.Message.Text == "" {
		return
	}
	text := strings.TrimSpace(u.Message.Text)
	chatID := strconv.FormatInt(u.Message.Chat.ID, 10)

	r.log.Info("telegram: inbound message",
		"chat_id", chatID,
		"text", truncate(text, 100),
	)

	if _, _, ok := ParseCommand(text); ok {
		cmdCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		reply := r.cmdFn(cmdCtx, text)
		if err := r.sendText(ctx, chatID, reply); err != nil {
			r.log.Warn("telegram: reply failed", "err", err)
		}
	}
}

func (r *TelegramRelay) sendText(ctx context.Context, chatID, text string) error {
	url := fmt.Sprintf("%s%s/sendMessage", telegramAPIBase, r.token)
	body, _ := json.Marshal(map[string]string{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": "Markdown",
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url,
		bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("telegram: sendMessage returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func formatMessage(msg models.Message) string {
	return fmt.Sprintf("[%s] %s", msg.AgentID, truncate(msg.Content, 300))
}
