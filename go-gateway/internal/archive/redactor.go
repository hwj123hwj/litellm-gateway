package archive

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strings"
)

// sensitiveKeys lists JSON keys (case-insensitive) whose values must never be
// persisted. The redaction is applied recursively to every object encountered.
// The list includes exact-match names as well as common variant spellings
// (kebab-case, camelCase, abbreviated forms).
var sensitiveKeys = map[string]bool{
	"authorization": true,
	"cookie":        true,
	"setcookie":     true,
	"xapikey":       true,
	"apikey":        true,
	"token":         true,
	"accesstoken":   true,
	"refreshtoken":  true,
	"idtoken":       true,
	"oauthtoken":    true,
	"bearer":        true,
	"password":      true,
	"passwd":        true,
	"secret":        true,
	"clientsecret":  true,
	"privatekey":    true,
	"session":       true,
	"sessionid":     true,
	"privatetoken":  true,
	"xauthtoken":    true,
	"xcsrftoken":    true,
}

// normalizeKey makes JSON key matching insensitive to case and common
// separators, so both "api_key" and "apiKey" become "apikey".
func normalizeKey(key string) string {
	var b strings.Builder
	b.Grow(len(key))
	for _, r := range strings.ToLower(key) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// isSensitiveKey returns true if the key is sensitive or ends with a sensitive
// compound suffix. Matching is performed on normalized keys so camelCase,
// snake_case, kebab-case, and dotted variants are covered consistently.
func isSensitiveKey(key string) bool {
	normalized := normalizeKey(key)
	if sensitiveKeys[normalized] {
		return true
	}
	// Suffix-based matching catches names such as githubToken,
	// anthropic_api_key, and client-secret without treating a generic "key"
	// field as sensitive.
	for _, suffix := range []string{
		"apikey",
		"token",
		"secret",
		"password",
		"privatekey",
	} {
		if len(normalized) > len(suffix) && strings.HasSuffix(normalized, suffix) {
			return true
		}
	}
	return false
}

// multimediaFields holds JSON keys that may carry inline base64 payloads
// (images, audio, files). These are replaced by {type, size, sha256} digests
// so the archive retains provenance without storing potentially large blobs.
var multimediaFields = map[string]bool{
	"image":      true,
	"imageurl":   true,
	"video":      true,
	"videourl":   true,
	"source":     true, // Anthropic-style inline source.data
	"inputaudio": true,
	"audio":      true,
	"file":       true,
	"fileurl":    true,
	"filedata":   true,
	"inputfile":  true,
	"data":       true, // used by Anthropic source blocks for base64 content
}

func isMultimediaField(key string) bool {
	return multimediaFields[normalizeKey(key)]
}

// Redact sanitizes a raw JSON body for archival storage:
//   - Recursively replaces the values of sensitiveKeys with "[REDACTED]".
//   - Replaces inline multimedia base64 with a compact digest
//     ({type,size,sha256}) so the original binary never lands in the archive.
//   - Leaves all other fields untouched.
//
// If the input is not valid JSON, credential-shaped text is scrubbed with
// RedactText. This keeps archival best-effort without allowing malformed
// provider responses to bypass the no-secrets-at-rest guarantee.
func Redact(raw []byte) []byte {
	if len(raw) == 0 {
		return raw
	}
	var data any
	if err := json.Unmarshal(raw, &data); err != nil {
		return RedactText(raw)
	}
	redacted := redactValue(data)
	out, err := json.Marshal(redacted)
	if err != nil {
		return raw
	}
	// A valid JSON body can still contain an unstructured provider error string
	// such as {"message":"api_key=..."}; scrub that text after the recursive
	// key-based pass as well.
	return RedactText(out)
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
		if isSensitiveKey(key) {
			m[key] = "[REDACTED]"
			continue
		}
		if isMultimediaField(key) {
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
			// Check for sensitive keys inside multimedia objects first —
			// these must be redacted regardless of where they appear.
			if isSensitiveKey(key) {
				val[key] = "[REDACTED]"
				continue
			}
			normalizedKey := normalizeKey(key)
			// Digest known binary-carrying subfields.
			if normalizedKey == "data" || normalizedKey == "base64" || normalizedKey == "filedata" {
				if s, ok := inner.(string); ok && looksLikeBase64(s) {
					val[key] = digestString(s)
					continue
				}
			}
			// "url" subfield: only digest if it's a data: URI, not https://
			if normalizedKey == "url" {
				if s, ok := inner.(string); ok && looksLikeBase64(s) && strings.HasPrefix(strings.ToLower(strings.TrimSpace(s)), "data:") {
					val[key] = digestString(s)
					continue
				}
				// Regular URL → preserve, but still recurse for nested sensitive keys
				val[key] = redactValue(inner)
				continue
			}
			// Recurse into nested multimedia objects (e.g. nested image_url objects).
			if isMultimediaField(key) {
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
	trimmed := strings.TrimSpace(s)
	if strings.HasPrefix(strings.ToLower(trimmed), "data:") {
		return true
	}
	// Don't touch regular URLs
	if strings.HasPrefix(strings.ToLower(trimmed), "http://") || strings.HasPrefix(strings.ToLower(trimmed), "https://") {
		return false
	}
	// Heuristic: a long string of base64 characters (>=64 chars) is almost
	// certainly an inline binary, not a normal URL or text value.
	// Also validate that the length is a multiple of 4 (valid base64 padding).
	if len(trimmed) >= 64 {
		for _, encoding := range []*base64.Encoding{
			base64.StdEncoding,
			base64.RawStdEncoding,
			base64.URLEncoding,
			base64.RawURLEncoding,
		} {
			if _, err := encoding.DecodeString(trimmed); err == nil {
				return true
			}
		}
	}
	return false
}

var (
	// keyValueSecretPattern handles unstructured provider errors such as
	// "api_key=...", "githubToken: ...", and "password=...".
	keyValueSecretPattern = regexp.MustCompile(`(?i)((?:[a-z0-9]+[-_ ]?)?(?:authorization|cookie|set[-_ ]?cookie|x[-_ ]?api[-_ ]?key|api[-_ ]?key|token|secret|password|passwd|private[-_ ]?key)\s*[:=]\s*)(?:"[^"]*"|'[^']*'|[^\s,;]+)`)
	bearerSecretPattern   = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]+`)
	prefixSecretPattern   = regexp.MustCompile(`(?i)\b(?:sk-[A-Za-z0-9._-]+|gh[pousr]_[A-Za-z0-9_]+|github_pat_[A-Za-z0-9_]+|AIza[A-Za-z0-9_-]+|AKIA[0-9A-Z]{12,})\b`)
)

// RedactText removes credential-shaped values from unstructured text. It is
// used as a safe fallback when an upstream body or error is not valid JSON.
// Ordinary prose remains unchanged; only explicit key/value credentials,
// bearer tokens, and well-known provider token prefixes are replaced.
func RedactText(raw []byte) []byte {
	if len(raw) == 0 {
		return raw
	}
	result := string(raw)
	result = keyValueSecretPattern.ReplaceAllString(result, "${1}[REDACTED]")
	result = bearerSecretPattern.ReplaceAllString(result, "[REDACTED]")
	result = prefixSecretPattern.ReplaceAllString(result, "[REDACTED]")
	return []byte(result)
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
