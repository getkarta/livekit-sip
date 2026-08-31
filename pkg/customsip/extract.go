package customsip

import (
	"encoding/base64"
	"encoding/hex"
	"strings"
)

// FlowExtractor describes how to read a flow_id from an INVITE header.
type FlowExtractor struct {
	Header   string `json:"header"`
	Format   string `json:"format"`   // "plain" | "uuid_pipe"
	Encoding string `json:"encoding"` // "none" | "base64" | "hex" | "auto" (uuid_pipe only)
}

// DefaultFlowExtractor matches PhonePe User-to-User (uuid|flow_id, base64 auto-detect).
var DefaultFlowExtractor = FlowExtractor{
	Header:   "User-to-User",
	Format:   "uuid_pipe",
	Encoding: "auto",
}

// IsDefaultFlowExtractor reports whether e matches the built-in PhonePe User-to-User parser.
func IsDefaultFlowExtractor(e FlowExtractor) bool {
	if !e.valid() {
		return true
	}
	d := DefaultFlowExtractor.normalized()
	e = e.normalized()
	return e.Header == d.Header && e.Format == d.Format && e.Encoding == d.Encoding
}

func (e FlowExtractor) normalized() FlowExtractor {
	out := e
	out.Header = strings.TrimSpace(out.Header)
	out.Format = strings.ToLower(strings.TrimSpace(out.Format))
	out.Encoding = strings.ToLower(strings.TrimSpace(out.Encoding))
	if out.Header == "" {
		return out
	}
	if out.Format == "" {
		out.Format = "uuid_pipe"
	}
	if out.Format == "uuid_pipe" && out.Encoding == "" {
		out.Encoding = "auto"
	}
	return out
}

func (e FlowExtractor) valid() bool {
	return strings.TrimSpace(e.Header) != ""
}

// ParseFlowID extracts flow_id (and optional session id) from a header value.
func ParseFlowID(extractor FlowExtractor, headerValue string) (sessionID, flowID string, ok bool) {
	if !extractor.valid() {
		extractor = DefaultFlowExtractor
	}
	extractor = extractor.normalized()
	if strings.TrimSpace(headerValue) == "" {
		return "", "", false
	}
	switch extractor.Format {
	case "plain":
		flowID = strings.TrimSpace(headerValue)
		if flowID == "" {
			return "", "", false
		}
		return "", flowID, true
	case "uuid_pipe":
		return parseUserToUser(headerValue, extractor.Encoding)
	default:
		return "", "", false
	}
}

// parseUserToUser extracts session UUID and flow_id from a header payload.
// Expected decoded format: "<uuid>|<flow_id>".
func parseUserToUser(headerValue, encoding string) (sessionID, flowID string, ok bool) {
	if headerValue == "" {
		return "", "", false
	}

	payload, ok := decodeUserToUserPayload(headerValue, encoding)
	if !ok {
		return "", "", false
	}

	parts := strings.SplitN(payload, "|", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	sessionID = strings.TrimSpace(parts[0])
	flowID = strings.TrimSpace(parts[1])
	if flowID == "" {
		return "", "", false
	}
	return sessionID, flowID, true
}

func decodeUserToUserPayload(headerValue, encoding string) (string, bool) {
	lower := strings.ToLower(headerValue)
	value := headerValue
	if idx := strings.Index(value, ";"); idx >= 0 {
		value = strings.TrimSpace(value[:idx])
	}
	if value == "" {
		return "", false
	}

	switch encoding {
	case "none":
		if strings.Contains(value, "|") {
			return value, true
		}
		return "", false
	case "base64":
		return decodeBase64UserToUser(value)
	case "hex":
		b, err := hex.DecodeString(value)
		if err != nil {
			return "", false
		}
		return string(b), true
	default: // auto
		switch {
		case strings.Contains(lower, "encoding=base64"):
			return decodeBase64UserToUser(value)
		case strings.Contains(lower, "encoding=hex"):
			b, err := hex.DecodeString(value)
			if err != nil {
				return "", false
			}
			return string(b), true
		default:
			if strings.Contains(value, "|") {
				return value, true
			}
			if decoded, ok := decodeBase64UserToUser(value); ok && strings.Contains(decoded, "|") {
				return decoded, true
			}
			return "", false
		}
	}
}

func decodeBase64UserToUser(value string) (string, bool) {
	if b, err := base64.StdEncoding.DecodeString(value); err == nil {
		return string(b), true
	}
	if b, err := base64.RawStdEncoding.DecodeString(value); err == nil {
		return string(b), true
	}
	if b, err := base64.URLEncoding.DecodeString(value); err == nil {
		return string(b), true
	}
	if b, err := base64.RawURLEncoding.DecodeString(value); err == nil {
		return string(b), true
	}
	return "", false
}
