package archive

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRedactStripsSensitiveKeys(t *testing.T) {
	input := `{"model":"coding","authorization":"Bearer secret","api_key":"sk-xxx","password":"hunter2","messages":[{"role":"user","content":"hi"}]}`
	out := Redact([]byte(input))

	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("redacted output is not valid JSON: %v", err)
	}
	for _, key := range []string{"authorization", "api_key", "password"} {
		if m[key] != "[REDACTED]" {
			t.Errorf("key %q = %v, want [REDACTED]", key, m[key])
		}
	}
	if m["model"] != "coding" {
		t.Errorf("model = %v, want coding", m["model"])
	}
}

func TestRedactHandlesNestedObjects(t *testing.T) {
	input := `{"header":{"Authorization":"Bearer xyz","Cookie":"session=abc"},"body":{"data":"ok"}}`
	out := Redact([]byte(input))

	var m map[string]any
	json.Unmarshal(out, &m)
	header := m["header"].(map[string]any)
	if header["Authorization"] != "[REDACTED]" {
		t.Errorf("nested Authorization not redacted: %v", header["Authorization"])
	}
	if header["Cookie"] != "[REDACTED]" {
		t.Errorf("nested Cookie not redacted: %v", header["Cookie"])
	}
}

func TestRedactReplacesMultimediaBase64(t *testing.T) {
	// A fake but large-ish base64 string
	fakeB64 := strings.Repeat("A", 500)
	input := `{"type":"image","source":{"type":"base64","media_type":"image/png","data":"` + fakeB64 + `"}}`
	out := Redact([]byte(input))

	var m map[string]any
	json.Unmarshal(out, &m)
	source := m["source"].(map[string]any)
	data := source["data"].(map[string]any)
	if data["type"] != "redacted_base64" {
		t.Errorf("multimedia data type = %v, want redacted_base64", data["type"])
	}
	if data["size"].(float64) != float64(500) {
		t.Errorf("multimedia size = %v, want 500", data["size"])
	}
	hash, ok := data["sha256"].(string)
	if !ok || hash == "" {
		t.Errorf("multimedia sha256 missing or empty: %v", data["sha256"])
	}
}

func TestRedactHandlesOpenAIImageURL(t *testing.T) {
	input := `{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,iVBORw0KG"}}]}]}`
	out := Redact([]byte(input))

	var m map[string]any
	json.Unmarshal(out, &m)
	messages := m["messages"].([]any)
	msg := messages[0].(map[string]any)
	content := msg["content"].([]any)
	block := content[0].(map[string]any)
	if block["type"] != "image_url" {
		t.Errorf("block type = %v, want image_url", block["type"])
	}
	// The image_url object should still exist but the inline data: URL should
	// have been digested into {type,size,sha256}.
	imgURL := block["image_url"].(map[string]any)
	urlValue := imgURL["url"]
	digest, ok := urlValue.(map[string]any)
	if !ok {
		t.Fatalf("image_url.url was not digested: type=%T value=%v", urlValue, urlValue)
	}
	if digest["type"] != "redacted_base64" {
		t.Errorf("digest type = %v, want redacted_base64", digest["type"])
	}
}

func TestRedactHandlesArray(t *testing.T) {
	input := `[{"token":"secret1"},{"token":"secret2"}]`
	out := Redact([]byte(input))
	var arr []any
	json.Unmarshal(out, &arr)
	for i, item := range arr {
		m := item.(map[string]any)
		if m["token"] != "[REDACTED]" {
			t.Errorf("array[%d].token = %v, want [REDACTED]", i, m["token"])
		}
	}
}

func TestRedactNonJSONPassthrough(t *testing.T) {
	input := []byte("this is not json")
	out := Redact(input)
	if string(out) != "this is not json" {
		t.Errorf("non-JSON input was modified")
	}
}

func TestRedactEmpty(t *testing.T) {
	out := Redact([]byte(""))
	if len(out) != 0 {
		t.Errorf("empty input should produce empty output")
	}
}

func TestRedactCaseInsensitiveKeys(t *testing.T) {
	input := `{"Authorization":"Bearer x","AUTHORIZATION":"Bearer y","Api_Key":"k"}`
	out := Redact([]byte(input))
	var m map[string]any
	json.Unmarshal(out, &m)
	for _, key := range []string{"Authorization", "AUTHORIZATION", "Api_Key"} {
		if m[key] != "[REDACTED]" {
			t.Errorf("key %q not redacted (case-insensitive match failed): %v", key, m[key])
		}
	}
}
