package relay_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/haepapa/kotui/internal/relay"
	"github.com/haepapa/kotui/pkg/models"
)

// ──────────────────────────────────────────────
// HMAC helpers
// ──────────────────────────────────────────────

func TestHMAC_Slack_Valid(t *testing.T) {
	secret := "slack_signing_secret"
	timestamp := "1609459200"
	body := []byte(`{"type":"event_callback"}`)

	// Compute expected signature.
	baseString := fmt.Sprintf("v0:%s:%s", timestamp, string(body))
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(baseString))
	sig := "v0=" + hex.EncodeToString(mac.Sum(nil))

	if !relay.VerifySlackSignature(secret, "v0", timestamp, body, sig) {
		t.Error("expected valid Slack signature to pass")
	}
}

func TestHMAC_Slack_Invalid(t *testing.T) {
	if relay.VerifySlackSignature("secret", "v0", "12345", []byte("body"), "v0=wrongsig") {
		t.Error("expected invalid Slack signature to fail")
	}
}

func TestHMAC_Slack_EmptySecret(t *testing.T) {
	if relay.VerifySlackSignature("", "v0", "12345", []byte("body"), "v0=anything") {
		t.Error("expected empty secret to fail")
	}
}

func TestHMAC_WhatsApp_Valid(t *testing.T) {
	secret := "whatsapp_app_secret"
	body := []byte(`{"object":"whatsapp_business_account"}`)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if !relay.VerifyWhatsAppSignature(secret, body, sig) {
		t.Error("expected valid WhatsApp signature to pass")
	}
}

func TestHMAC_WhatsApp_Invalid(t *testing.T) {
	if relay.VerifyWhatsAppSignature("secret", []byte("body"), "sha256=wrongsig") {
		t.Error("expected invalid WhatsApp signature to fail")
	}
}

func TestHMAC_WhatsApp_MissingPrefix(t *testing.T) {
	if relay.VerifyWhatsAppSignature("secret", []byte("body"), "noshaprefix") {
		t.Error("expected signature without sha256= prefix to fail")
	}
}

func TestHMAC_WhatsApp_EmptySecret(t *testing.T) {
	if relay.VerifyWhatsAppSignature("", []byte("body"), "sha256=something") {
		t.Error("expected empty secret to fail")
	}
}

// ──────────────────────────────────────────────
// Command parser
// ──────────────────────────────────────────────

func TestParseCommand_Status(t *testing.T) {
	cmd, args, ok := relay.ParseCommand("/status")
	if !ok || cmd != "/status" || args != "" {
		t.Errorf("unexpected: ok=%v cmd=%q args=%q", ok, cmd, args)
	}
}

func TestParseCommand_Approve_WithID(t *testing.T) {
	cmd, args, ok := relay.ParseCommand("/approve abc123")
	if !ok || cmd != "/approve" || args != "abc123" {
		t.Errorf("unexpected: ok=%v cmd=%q args=%q", ok, cmd, args)
	}
}

func TestParseCommand_Approve_NoID(t *testing.T) {
	cmd, args, ok := relay.ParseCommand("/approve")
	if !ok || cmd != "/approve" || args != "" {
		t.Errorf("unexpected: ok=%v cmd=%q args=%q", ok, cmd, args)
	}
}

func TestParseCommand_Summary(t *testing.T) {
	cmd, _, ok := relay.ParseCommand("/summary")
	if !ok || cmd != "/summary" {
		t.Errorf("unexpected: ok=%v cmd=%q", ok, cmd)
	}
}

func TestParseCommand_Unknown(t *testing.T) {
	_, _, ok := relay.ParseCommand("/unknown")
	if ok {
		t.Error("expected unknown command to return ok=false")
	}
}

func TestParseCommand_PlainText(t *testing.T) {
	_, _, ok := relay.ParseCommand("hello world")
	if ok {
		t.Error("expected plain text to return ok=false")
	}
}

func TestParseCommand_CaseInsensitive(t *testing.T) {
	cmd, _, ok := relay.ParseCommand("/STATUS")
	if !ok || cmd != "/status" {
		t.Errorf("expected case-insensitive match, got cmd=%q ok=%v", cmd, ok)
	}
}

// ──────────────────────────────────────────────
// TelegramRelay
// ──────────────────────────────────────────────

func TestTelegramRelay_Name(t *testing.T) {
	r := relay.NewTelegramRelay("", "", nil, nil)
	if r.Name() != "telegram" {
		t.Errorf("expected 'telegram', got %q", r.Name())
	}
}

func TestTelegramRelay_Send_NoToken(t *testing.T) {
	r := relay.NewTelegramRelay("", "", nil, nil)
	// No token — should return nil without making any HTTP calls.
	if err := r.Send(nil, mockMessage("hello")); err != nil {
		t.Errorf("expected nil error for unconfigured relay, got %v", err)
	}
}

// ──────────────────────────────────────────────
// SlackRelay
// ──────────────────────────────────────────────

func TestSlackRelay_Name(t *testing.T) {
	r := relay.NewSlackRelay("", "", "", nil, nil)
	if r.Name() != "slack" {
		t.Errorf("expected 'slack', got %q", r.Name())
	}
}

func TestSlackRelay_Send_NoToken(t *testing.T) {
	r := relay.NewSlackRelay("", "", "", nil, nil)
	if err := r.Send(nil, mockMessage("hello")); err != nil {
		t.Errorf("expected nil error for unconfigured relay, got %v", err)
	}
}

func TestSlackRelay_Webhook_UrlVerification(t *testing.T) {
	r := relay.NewSlackRelay("tok", "chan", "", noopCmd, nil)

	body := `{"type":"url_verification","challenge":"abc123"}`
	req := httptest.NewRequest(http.MethodPost, "/slack/events", mustReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.HandleWebhook(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if got := w.Body.String(); got == "" {
		t.Error("expected challenge in response body")
	}
}

func TestSlackRelay_Webhook_InvalidSig(t *testing.T) {
	r := relay.NewSlackRelay("tok", "chan", "secret", noopCmd, nil)

	req := httptest.NewRequest(http.MethodPost, "/slack/events", mustReader(`{}`))
	req.Header.Set("X-Slack-Request-Timestamp", "12345")
	req.Header.Set("X-Slack-Signature", "v0=invalidsig")
	w := httptest.NewRecorder()

	r.HandleWebhook(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

// ──────────────────────────────────────────────
// WhatsAppRelay
// ──────────────────────────────────────────────

func TestWhatsAppRelay_Name(t *testing.T) {
	r := relay.NewWhatsAppRelay("", "", "", "", nil, nil)
	if r.Name() != "whatsapp" {
		t.Errorf("expected 'whatsapp', got %q", r.Name())
	}
}

func TestWhatsAppRelay_Webhook_Verify_Valid(t *testing.T) {
	r := relay.NewWhatsAppRelay("tok", "phone", "myverifytoken", "", noopCmd, nil)

	req := httptest.NewRequest(http.MethodGet,
		"/whatsapp/webhook?hub.mode=subscribe&hub.verify_token=myverifytoken&hub.challenge=CHALLENGE123",
		nil)
	w := httptest.NewRecorder()

	r.HandleWebhook(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if w.Body.String() != "CHALLENGE123" {
		t.Errorf("expected challenge CHALLENGE123, got %q", w.Body.String())
	}
}

func TestWhatsAppRelay_Webhook_Verify_WrongToken(t *testing.T) {
	r := relay.NewWhatsAppRelay("tok", "phone", "correct", "", noopCmd, nil)

	req := httptest.NewRequest(http.MethodGet,
		"/whatsapp/webhook?hub.mode=subscribe&hub.verify_token=wrong&hub.challenge=X",
		nil)
	w := httptest.NewRecorder()

	r.HandleWebhook(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

// ──────────────────────────────────────────────
// WebhookServer
// ──────────────────────────────────────────────

func TestWebhookServer_StartStop(t *testing.T) {
	// Find a free port.
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("could not find free port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	srv := relay.NewWebhookServer(port, nil)
	if err := srv.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	// Server should respond on the port.
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/healthz", port))
	if err == nil {
		resp.Body.Close()
	}
	// Graceful shutdown must not panic.
	srv.Stop()
}

// ──────────────────────────────────────────────
// helpers
// ──────────────────────────────────────────────

func noopCmd(_ context.Context, _ string) string { return "ok" }

func mustReader(s string) *strings.Reader {
	return strings.NewReader(s)
}

func mockMessage(content string) models.Message {
	return models.Message{Content: content}
}
