// Package tools/websearch implements the web_search MCP tool.
//
// Phase 10 scope (self-built):
//   - fetch: HTTP GET a URL, strip HTML tags, truncate to 4 KB, return plain text
//
// Safety controls:
//   - Private/loopback IP ranges are blocked (RFC 1918, RFC 5735, ::1)
//   - 15-second timeout per request
//   - Maximum output: 4096 bytes
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/haepapa/kotui/internal/mcp"
	"github.com/haepapa/kotui/pkg/models"
)

const (
	webSearchMaxBytes = 4096
	webSearchTimeout  = 15 * time.Second
)

var webSearchSchema = json.RawMessage(`{
	"type": "object",
	"required": ["operation", "url"],
	"properties": {
		"operation": {
			"type": "string",
			"description": "fetch — HTTP GET the URL and return sanitised plain text"
		},
		"url": {
			"type": "string",
			"description": "Fully-qualified URL to retrieve (https:// recommended)"
		}
	}
}`)

// blockedCIDRs covers RFC 1918, loopback, link-local, and other non-routable ranges.
var blockedCIDRs = func() []*net.IPNet {
	ranges := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.0/8",
		"169.254.0.0/16",
		"100.64.0.0/10",
		"192.0.0.0/24",
		"198.18.0.0/15",
		"198.51.100.0/24",
		"203.0.113.0/24",
		"240.0.0.0/4",
		"::1/128",
		"fc00::/7",
		"fe80::/10",
	}
	var nets []*net.IPNet
	for _, cidr := range ranges {
		_, n, _ := net.ParseCIDR(cidr)
		if n != nil {
			nets = append(nets, n)
		}
	}
	return nets
}()

var (
	htmlTagRe     = regexp.MustCompile(`<[^>]+>`)
	htmlEntityRe  = regexp.MustCompile(`&[a-zA-Z0-9#]+;`)
	whitespaceRe  = regexp.MustCompile(`\s{3,}`)
)

func webSearchTool() mcp.ToolDef {
	client := &http.Client{
		Timeout: webSearchTimeout,
		// Custom transport to block private IP resolution.
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				host, _, err := net.SplitHostPort(addr)
				if err != nil {
					return nil, err
				}
				ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
				if err != nil {
					return nil, err
				}
				for _, ip := range ips {
					if isPrivateIP(ip.IP) {
						return nil, fmt.Errorf("web_search: access to private/reserved IP %s is blocked", ip.IP)
					}
				}
				return (&net.Dialer{}).DialContext(ctx, network, addr)
			},
		},
	}

	return mcp.ToolDef{
		Name:      "web_search",
		Clearance: models.ClearanceSpecialist,
		Description: "Retrieve content from a public URL. Returns sanitised plain text (max 4 KB). " +
			"Private/internal IP addresses are blocked. Use for fetching public documentation, " +
			"API references, or research content. operation: fetch.",
		Schema:  webSearchSchema,
		Handler: webSearchHandler(client),
	}
}

func webSearchHandler(client *http.Client) mcp.Handler {
	return func(ctx context.Context, args map[string]any) (string, error) {
		op, _ := args["operation"].(string)
		rawURL, _ := args["url"].(string)

		switch op {
		case "fetch":
			return webFetch(ctx, client, rawURL)
		default:
			return "", fmt.Errorf("web_search: unknown operation %q — supported: fetch", op)
		}
	}
}

func webFetch(ctx context.Context, client *http.Client, rawURL string) (string, error) {
	if rawURL == "" {
		return "", fmt.Errorf("web_search: url is required")
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("web_search: invalid url %q: %w", rawURL, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("web_search: only http/https schemes are allowed, got %q", parsed.Scheme)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("web_search: failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", "kotui-agent/1.0")
	req.Header.Set("Accept", "text/html,text/plain,application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("web_search: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("web_search: server returned HTTP %d for %q", resp.StatusCode, rawURL)
	}

	limited := io.LimitReader(resp.Body, webSearchMaxBytes*4)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return "", fmt.Errorf("web_search: failed to read response: %w", err)
	}

	text := sanitiseHTML(string(raw))
	if len(text) > webSearchMaxBytes {
		text = text[:webSearchMaxBytes] + "\n[...truncated]"
	}

	return fmt.Sprintf("URL: %s\nStatus: %d\n\n%s", rawURL, resp.StatusCode, text), nil
}

func sanitiseHTML(s string) string {
	s = htmlTagRe.ReplaceAllString(s, " ")
	s = htmlEntityRe.ReplaceAllStringFunc(s, decodeHTMLEntity)
	s = whitespaceRe.ReplaceAllString(s, "\n")
	return strings.TrimSpace(s)
}

func decodeHTMLEntity(e string) string {
	switch e {
	case "&amp;":
		return "&"
	case "&lt;":
		return "<"
	case "&gt;":
		return ">"
	case "&quot;":
		return `"`
	case "&#39;", "&apos;":
		return "'"
	case "&nbsp;":
		return " "
	default:
		return " "
	}
}

func isPrivateIP(ip net.IP) bool {
	for _, cidr := range blockedCIDRs {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}
