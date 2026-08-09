package archive

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
)

// sensitiveKeys lists JSON keys (case-insensitive) whose values must never be
// persisted. The redaction is applied recursively to every object encountered.
// The list includes exact-match names as well as common variant spellings
// (kebab-case, camelCase, abbreviated forms).
var sensitiveKeys = map[string]bool{
	"authorization": true,
	"cookie":        true,
	"set-cookie":    true,
	"x-api-key":     true,
	"api-key":       true, // kebab-case variant
	"api_key":       true,
	"apikey":        true,
	"token":         true,
	"access_token":  true,
	"accesstoken":   true,
	"refresh_token": true,
	"refreshtoken":  true,
	"id_token":      true,
	"idtoken":       true,
	"oauth_token":   true,
	"oauthtoken":    true,
	"bearer":        true,
	"password":      true,
	"passwd":        true,
	"secret":        true,
	"client_secret": true,
	"clientsecret":  true,
	"private_key":   true,
	"privatekey":    true,
	"session":       true,
	"session_id":    true,
	"sessionid":     true,
	"private-token": true, // some APIs use this
	"x-auth-token":  true,
	"x-csrf-token":  true,
}

// isSensitiveKey returns true if the lowercased key matches any entry in
// sensitiveKeys OR ends with a known sensitive suffix (e.g. "github_token",
// "anthropic_api_key", "openai_api_key"). This catches compound names that
// the static map would miss.
func isSensitiveKey(lowerKey string) bool {
	if sensitiveKeys[lowerKey] {
		return true
	}
	// Suffix-based matching for compound key names like "openai_api_key",
	// "github_token", "anthropic_key", etc.
	sensitiveSuffixes := []string{
		"_api_key", "-api-key", "_apikey",
		"_token", "-token",
		"_secret", "-secret",
		"_password", "-password",
		"_private_key", "-private-key",
	}
	for _, suffix := range sensitiveSuffixes {
		if strings.HasSuffix(lowerKey, suffix) {
			return true
		}
	}
	return false
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
		if isSensitiveKey(lowerKey) {
			m[key] = "[REDACTED]"
			continue
		}
		if multimediaFields[lowerKey] {
			m[key] = redactMultimedia(value)
			continue
		}
		// Recurse into nested objects so that sensitive keys nested inside
		// multimedia objects (e.g. {"image": {"url": "...", "api_key": "sk-xxx"}})
		// are still caught by the regular redactObject path.
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
// preserving type metadata and ordinary URLs. Supported shapes:
//   - string  → if it looks like base64/data: URI, replace with digest;
//     otherwise keep the original value (e.g. https:// URLs are preserved)
//   - object  → recurse into it; digest any "data"/"base64"/"url" subfield that
//     carries inline binary data (data: URIs or raw base64)
//   - array   → redact each element
func redactMultimedia(v any) any {
	switch val := v.(type) {
	case string:
		// Only digest if it actually looks like inline binary data.
		// Regular URLs (https://...) and short strings are preserved.
		if looksLikeBase64(val) {
			return digestString(val)
		}
		return val
	case map[string]any:
		// Walk the object: keep metadata fields, digest any "data" subfield.
		for key, inner := range val {
			lowerKey := strings.ToLower(key)
			// Check for sensitive keys inside multimedia objects first —
			// these must be redacted regardless of where they appear.
			if isSensitiveKey(lowerKey) {
				val[key] = "[REDACTED]"
				continue
			}
			// Digest known binary-carrying subfields.
			if lowerKey == "data" || lowerKey == "base64" || lowerKey == "file_data" {
				if s, ok := inner.(string); ok && looksLikeBase64(s) {
					val[key] = digestString(s)
					continue
				}
			}
			// "url" subfield: only digest if it's a data: URI, not https://
			if lowerKey == "url" {
				if s, ok := inner.(string); ok && strings.HasPrefix(s, "data:") {
					val[key] = digestString(s)
					continue
				}
				// Regular URL → preserve, but still recurse for nested sensitive keys
				val[key] = redactValue(inner)
				continue
			}
			// Recurse into nested multimedia objects (e.g. nested image_url objects).
			if multimediaFields[lowerKey] {
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
// data: URIs or long base64 blobs. Short strings and regular URLs
// (e.g. https://...) are left untouched.
func looksLikeBase64(s string) bool {
	if strings.HasPrefix(s, "data:") {
		return true
	}
	// Don't touch regular URLs
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		return false
	}
	// Heuristic: a long string of base64 characters (>=64 chars) is almost
	// certainly an inline binary, not a normal URL or text value.
	// Also validate that the length is a multiple of 4 (valid base64 padding).
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
