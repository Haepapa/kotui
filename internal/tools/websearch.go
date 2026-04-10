// Package tools/websearch implements the web_search MCP tool.
//
// Operations:
//   - fetch:       HTTP GET a URL; uses go-readability for content extraction,
//                  smart content-type handling (JSON pass-through, markdown pass-through).
//   - search:      Keyword search via DuckDuckGo Lite; returns titles, URLs, and
//                  snippets as plain text. No API key required.
//   - fetch_jina:  Fetch a URL via Jina AI Reader (r.jina.ai), which renders
//                  JavaScript pages server-side and returns clean Markdown.
//                  NOTE: the URL is sent to Jina AI's servers.
//
// Safety controls:
//   - Private/loopback IP ranges are blocked for fetch/fetch_jina (RFC 1918, RFC 5735, ::1)
//   - 20-second timeout per request
//   - Maximum output: 8 KB
package tools

import (
	"bytes"
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

	readability "github.com/go-shiori/go-readability"
	"github.com/haepapa/kotui/internal/mcp"
	"github.com/haepapa/kotui/pkg/models"
)

const (
	webSearchMaxBytes = 8192
	webSearchTimeout  = 20 * time.Second
)

var webSearchSchema = json.RawMessage(`{
	"type": "object",
	"required": ["operation"],
	"properties": {
		"operation": {
			"type": "string",
			"description": "fetch | search | fetch_jina"
		},
		"url": {
			"type": "string",
			"description": "Fully-qualified URL to retrieve (required for fetch and fetch_jina)"
		},
		"query": {
			"type": "string",
			"description": "Search query string (required for search)"
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
	// safeTransport blocks outbound connections to private/reserved IP ranges.
	safeTransport := &http.Transport{
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
	}
	client := &http.Client{Timeout: webSearchTimeout, Transport: safeTransport}

	// searchClient is used for DuckDuckGo Lite queries — no IP restriction needed
	// since ddg.gg is a known public service, but we reuse the same timeout.
	searchClient := &http.Client{Timeout: webSearchTimeout}

	return mcp.ToolDef{
		Name:      "web_search",
		Clearance: models.ClearanceSpecialist,
		Description: "Access public web content. Operations: " +
			"fetch — retrieve a URL and extract its main readable text (uses Readability; handles JSON/Markdown natively; max 8 KB); " +
			"search — keyword web search via DuckDuckGo returning titles, URLs, and snippets (no API key needed); " +
			"fetch_jina — fetch a JS-rendered page via Jina AI Reader (returns clean Markdown; NOTE: URL is sent to Jina AI servers). " +
			"Private IP addresses are blocked for fetch/fetch_jina.",
		Schema:  webSearchSchema,
		Handler: webSearchHandler(client, searchClient),
	}
}

func webSearchHandler(client, searchClient *http.Client) mcp.Handler {
	return func(ctx context.Context, args map[string]any) (string, error) {
		op, _ := args["operation"].(string)
		rawURL, _ := args["url"].(string)
		query, _ := args["query"].(string)

		switch op {
		case "fetch":
			return webFetch(ctx, client, rawURL)
		case "search":
			return webSearch(ctx, searchClient, query)
		case "fetch_jina":
			return webFetchJina(ctx, client, rawURL)
		default:
			return "", fmt.Errorf("web_search: unknown operation %q — supported: fetch, search, fetch_jina", op)
		}
	}
}

// webFetch retrieves a URL and extracts its main readable content.
// For HTML pages it uses go-readability; for JSON/plain-text it returns the
// content directly. Output is capped at webSearchMaxBytes.
func webFetch(ctx context.Context, client *http.Client, rawURL string) (string, error) {
	if rawURL == "" {
		return "", fmt.Errorf("web_search: url is required for fetch")
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
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; kotui-agent/1.0)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/json,text/plain,*/*")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("web_search: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("web_search: server returned HTTP %d for %q", resp.StatusCode, rawURL)
	}

	// Read up to 512 KB — readability needs enough context to work well.
	limited := io.LimitReader(resp.Body, 512*1024)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return "", fmt.Errorf("web_search: failed to read response: %w", err)
	}

	ct := resp.Header.Get("Content-Type")
	var text string

	switch {
	case strings.Contains(ct, "application/json"):
		// Pretty-print JSON so the agent can read it.
		var v any
		if json.Unmarshal(raw, &v) == nil {
			if pretty, err := json.MarshalIndent(v, "", "  "); err == nil {
				text = string(pretty)
			}
		}
		if text == "" {
			text = string(raw)
		}

	case strings.Contains(ct, "text/plain") || strings.Contains(ct, "text/markdown"):
		text = string(raw)

	default:
		// HTML (or unknown) — run through Readability for clean article text.
		parsedURL, _ := url.Parse(rawURL)
		article, rerr := readability.FromReader(bytes.NewReader(raw), parsedURL)
		if rerr == nil && strings.TrimSpace(article.TextContent) != "" {
			prefix := ""
			if article.Title != "" {
				prefix = "# " + article.Title + "\n\n"
			}
			text = prefix + strings.TrimSpace(article.TextContent)
		} else {
			// Readability found nothing useful — fall back to tag stripping.
			text = sanitiseHTML(string(raw))
		}
	}

	if len(text) > webSearchMaxBytes {
		text = text[:webSearchMaxBytes] + "\n[...truncated — use a more specific URL or narrow your query]"
	}

	return fmt.Sprintf("URL: %s\nStatus: %d\n\n%s", rawURL, resp.StatusCode, text), nil
}

// webSearch queries DuckDuckGo Lite and returns titles, URLs, and snippets.
func webSearch(ctx context.Context, client *http.Client, query string) (string, error) {
	if query == "" {
		return "", fmt.Errorf("web_search: query is required for search")
	}

	searchURL := "https://lite.duckduckgo.com/lite/?q=" + url.QueryEscape(query)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return "", fmt.Errorf("web_search: failed to create search request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; kotui-agent/1.0)")
	req.Header.Set("Accept", "text/html")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("web_search: search request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("web_search: DuckDuckGo returned HTTP %d", resp.StatusCode)
	}

	limited := io.LimitReader(resp.Body, 256*1024)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return "", fmt.Errorf("web_search: failed to read search response: %w", err)
	}

	results := parseDDGLite(string(raw))
	if len(results) == 0 {
		return fmt.Sprintf("Search: %q\n\nNo results found.", query), nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Search: %q\n\n", query)
	for i, r := range results {
		fmt.Fprintf(&sb, "%d. %s\n   %s\n   %s\n\n", i+1, r.title, r.url, r.snippet)
	}

	text := sb.String()
	if len(text) > webSearchMaxBytes {
		text = text[:webSearchMaxBytes] + "\n[...truncated]"
	}
	return text, nil
}

type ddgResult struct{ title, url, snippet string }

// parseDDGLite extracts search results from DuckDuckGo Lite HTML.
// DDG Lite uses a simple table layout that has been stable for years.
var (
	ddgLinkRe    = regexp.MustCompile(`<a[^>]+class="[^"]*result-link[^"]*"[^>]*href="([^"]+)"[^>]*>(.*?)</a>`)
	ddgSnippetRe = regexp.MustCompile(`<td[^>]+class="[^"]*result-snippet[^"]*"[^>]*>(.*?)</td>`)
	ddgUDDGRe    = regexp.MustCompile(`[?&]uddg=([^&]+)`)
)

func parseDDGLite(html string) []ddgResult {
	links := ddgLinkRe.FindAllStringSubmatch(html, 20)
	snippets := ddgSnippetRe.FindAllStringSubmatch(html, 20)

	var results []ddgResult
	for i, link := range links {
		if i >= 10 {
			break
		}
		rawHref := link[1]
		title := sanitiseHTML(link[2])

		// DDG Lite uses redirect URLs; extract the real URL from uddg= param.
		resolvedURL := rawHref
		if m := ddgUDDGRe.FindStringSubmatch(rawHref); len(m) > 1 {
			if decoded, err := url.QueryUnescape(m[1]); err == nil {
				resolvedURL = decoded
			}
		}

		snippet := ""
		if i < len(snippets) {
			snippet = sanitiseHTML(snippets[i][1])
		}

		if title != "" && resolvedURL != "" {
			results = append(results, ddgResult{title: title, url: resolvedURL, snippet: snippet})
		}
	}
	return results
}

// webFetchJina fetches a URL via Jina AI Reader, which renders JS-heavy pages
// server-side and returns clean Markdown. The URL is sent to Jina's servers.
func webFetchJina(ctx context.Context, client *http.Client, rawURL string) (string, error) {
	if rawURL == "" {
		return "", fmt.Errorf("web_search: url is required for fetch_jina")
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("web_search: invalid url %q: %w", rawURL, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("web_search: only http/https schemes are allowed, got %q", parsed.Scheme)
	}

	jinaURL := "https://r.jina.ai/" + rawURL

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jinaURL, nil)
	if err != nil {
		return "", fmt.Errorf("web_search: failed to create jina request: %w", err)
	}
	req.Header.Set("User-Agent", "kotui-agent/1.0")
	req.Header.Set("Accept", "text/plain,text/markdown,*/*")
	req.Header.Set("X-Return-Format", "markdown")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("web_search: jina request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("web_search: jina returned HTTP %d for %q", resp.StatusCode, rawURL)
	}

	limited := io.LimitReader(resp.Body, webSearchMaxBytes*4)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return "", fmt.Errorf("web_search: failed to read jina response: %w", err)
	}

	text := strings.TrimSpace(string(raw))
	if len(text) > webSearchMaxBytes {
		text = text[:webSearchMaxBytes] + "\n[...truncated]"
	}

	return fmt.Sprintf("⚠️  Note: This content was fetched via Jina AI servers (r.jina.ai).\nURL: %s\nStatus: %d\n\n%s", rawURL, resp.StatusCode, text), nil
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
