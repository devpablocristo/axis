package runtime

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strings"
)

func ValidateEgressPayload(raw json.RawMessage) *GuardrailEvent {
	if len(raw) == 0 {
		return nil
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil
	}
	for _, candidate := range collectURLCandidates(decoded) {
		if reason := unsafeEgressReason(candidate); reason != "" {
			return &GuardrailEvent{Type: "ssrf", Target: "egress:" + candidate, Reason: reason}
		}
	}
	return nil
}

func collectURLCandidates(value any) []string {
	var out []string
	var walk func(any)
	walk = func(v any) {
		switch typed := v.(type) {
		case map[string]any:
			for key, item := range typed {
				if s, ok := item.(string); ok && keyLooksLikeURL(key) {
					out = append(out, s)
				}
				walk(item)
			}
		case []any:
			for _, item := range typed {
				walk(item)
			}
		case string:
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(typed)), "http://") ||
				strings.HasPrefix(strings.ToLower(strings.TrimSpace(typed)), "https://") ||
				strings.HasPrefix(strings.ToLower(strings.TrimSpace(typed)), "file://") {
				out = append(out, typed)
			}
		}
	}
	walk(value)
	return out
}

func keyLooksLikeURL(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	return strings.Contains(key, "url") || strings.Contains(key, "uri") || strings.Contains(key, "webhook") || strings.Contains(key, "endpoint")
}

func unsafeEgressReason(candidate string) string {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(candidate), "file://") {
		return "file URLs are not allowed for external egress"
	}
	parsed, err := url.Parse(candidate)
	if err != nil || parsed.Hostname() == "" {
		return ""
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
	default:
		return fmt.Sprintf("scheme %q is not allowed for external egress", parsed.Scheme)
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || host == "metadata.google.internal" || strings.HasSuffix(host, ".internal") {
		return "internal host is not allowed for external egress"
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return ""
	}
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsPrivate() || ip.IsUnspecified() {
		return "private or metadata network address is not allowed for external egress"
	}
	return ""
}
