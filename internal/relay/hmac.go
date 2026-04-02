// Package relay — HMAC verification helpers for incoming webhooks.
//
// Slack uses HMAC-SHA256 with a signing secret over "v0:{timestamp}:{body}".
// WhatsApp uses HMAC-SHA256 with the app secret over the raw body, prefixed "sha256=".
package relay

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// VerifySlackSignature checks the X-Slack-Signature header against the computed HMAC.
// signingSecret is the Slack App signing secret.
// version is the signature version prefix (always "v0" for current Slack API).
// timestamp is the X-Slack-Request-Timestamp header value.
// body is the raw request body bytes.
// sig is the full X-Slack-Signature header value (e.g. "v0=abc123...").
func VerifySlackSignature(signingSecret, version, timestamp string, body []byte, sig string) bool {
	if signingSecret == "" || sig == "" || timestamp == "" {
		return false
	}
	baseString := version + ":" + timestamp + ":" + string(body)
	mac := hmac.New(sha256.New, []byte(signingSecret))
	mac.Write([]byte(baseString))
	expected := version + "=" + hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(sig))
}

// VerifyWhatsAppSignature checks the X-Hub-Signature-256 header against the computed HMAC.
// appSecret is the WhatsApp App Secret from the Meta developer portal.
// body is the raw request body bytes.
// xHubSig is the X-Hub-Signature-256 header value (e.g. "sha256=abc123...").
func VerifyWhatsAppSignature(appSecret string, body []byte, xHubSig string) bool {
	if appSecret == "" || xHubSig == "" {
		return false
	}
	const prefix = "sha256="
	if !strings.HasPrefix(xHubSig, prefix) {
		return false
	}
	got, err := hex.DecodeString(strings.TrimPrefix(xHubSig, prefix))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(appSecret))
	mac.Write(body)
	return hmac.Equal(mac.Sum(nil), got)
}
