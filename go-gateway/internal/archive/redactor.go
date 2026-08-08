package archive

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// sensitiveKeys lists JSON keys (case-insensitive) whose values must never be
// persisted. The redaction is applied recursively to every object encountered.
var sensitiveKeys = map[string]bool{
	"authorization": true,
	"cookie":        true,
	"x-api-key":     true,
	"api_key":       true,
	"apikey":        true,
	"token":         true,
	"access_token":  true,
	"refresh_token": true,
	"password":      true,
	"secret":        true,
	"client_secret": true,
}

// multimediaFields holds JSON keys that may carry inline base64 payloads
// (images, audio, files). These are replaced by {type, size, sha256} digests
// so the archive retains provenance without storing potentially large blobs.
var multimediaFields = map[string]bool{
	"image":       true,
	"image_url":   true,
	"video":       true,
	"video_url":   true,
	"source":      true, // Anthropic-style inline source.data
	"input_audio": true,
	"audio":       true,
	"file":        true,
	"file_url":    true,
	"input_file":  true,
	"data":        true, // used by Anthropic source blocks for base64 content
}

// Redact sanitizes a raw JSON body for archival storage:
//   - Recursively replaces the values of sensitiveKeys with "[REDACTED]".
//   - Replaces inline multimedia base64 with a compact digest
//     ({type,size,sha256}) so the original binary never lands in the archive.
//   - Leaves all other fields untouched.
//
// If the input is not valid JSON, it is returned as-is; the caller still gets a
// faithful (though un-structured) record. This keeps archival best-effort and
// never blocks a request because a body failed to redact.
func Redact(raw []byte) []byte {
	if len(raw) == 0 {
		return raw
	}
	var data any
	if err := json.Unmarshal(raw, &data); err != nil {
		return raw
	}
	redacted := redactValue(data)
	out, err := json.Marshal(redacted)
	if err != nil {
		return raw
	}
	return out
}

// redactValue dispatches on the dynamic type of a decoded JSON value.
func redactValue(v any) any {
	switch val := v.(type) {
	case map[string]any:
		return redactObject(val)
	case []any:
		return redactArray(val)
	default:
		return v
	}
}

func redactObject(m map[string]any) map[string]any {
	for key, value := range m {
		lowerKey := strings.ToLower(key)
		if sensitiveKeys[lowerKey] {
			m[key] = "[REDACTED]"
			continue
		}
		if multimediaFields[lowerKey] {
			m[key] = redactMultimedia(value)
			continue
		}
		m[key] = redactValue(value)
	}
	return m
}

func redactArray(a []any) []any {
	for i := range a {
		a[i] = redactValue(a[i])
	}
	return a
}

// redactMultimedia replaces inline base64/binary content with a digest while
// preserving type metadata. Supported shapes:
//   - string  → assume base64; replace with {type:"base64", size, sha256}
//   - object  → recurse into it; digest any "data"/"base64"/"url" subfield that
//     carries inline binary data (data: URIs or raw base64)
//   - array   → redact each element
func redactMultimedia(v any) any {
	switch val := v.(type) {
	case string:
		return digestString(val)
	case map[string]any:
		// Walk the object: keep metadata fields, digest any "data" subfield.
		for key, inner := range val {
			lowerKey := strings.ToLower(key)
			if lowerKey == "data" || lowerKey == "base64" || lowerKey == "url" {
				if s, ok := inner.(string); ok && looksLikeBase64(s) {
					val[key] = digestString(s)
					continue
				}
			}
			if multimediaFields[lowerKey] || sensitiveKeys[lowerKey] {
				val[key] = redactMultimedia(inner)
			} else {
				val[key] = redactValue(inner)
			}
		}
		return val
	case []any:
		return redactArray(val)
	default:
		return v
	}
}

// looksLikeBase64 returns true for strings that look like inline binary data:
// data: URIs or long base64 blobs. Short strings (e.g. regular https:// URLs)
// are left untouched.
func looksLikeBase64(s string) bool {
	if strings.HasPrefix(s, "data:") {
		return true
	}
	// Heuristic: a long string of base64 characters (>=64 chars) is almost
	// certainly an inline binary, not a normal URL or text value.
	if len(s) >= 64 {
		isBase64 := true
		for _, c := range s {
			if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') ||
				(c >= '0' && c <= '9') || c == '+' || c == '/' || c == '=' || c == '\n' || c == '\r') {
				isBase64 = false
				break
			}
		}
		if isBase64 {
			return true
		}
	}
	return false
}

// digestString returns a compact digest object for a (potentially base64-encoded)
// payload. We intentionally do NOT decode base64 here: the goal is to prove we
// did not store the original bytes, and to give export consumers a stable
// content hash for deduplication. Size is the raw string length in bytes.
func digestString(s string) map[string]any {
	if s == "" {
		return map[string]any{"type": "empty", "size": 0, "sha256": ""}
	}
	sum := sha256.Sum256([]byte(s))
	return map[string]any{
		"type":   "redacted_base64",
		"size":   len(s),
		"sha256": hex.EncodeToString(sum[:]),
	}
}

func toString(v any) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case fmt.Stringer:
		return val.String()
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}
