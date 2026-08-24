package handlers

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// collectNativeResponsesResponse turns a native Responses SSE transcript into
// the JSON response expected by a non-streaming Responses client. The Codex
// endpoint sends the completed response metadata separately from
// response.output_item.done, so the latter is used to fill an empty output
// array in response.completed.
func collectNativeResponsesResponse(streamBody []byte, requestedModel string) ([]byte, int, int, error) {
	var completed json.RawMessage
	var outputItems []json.RawMessage

	eventType := ""
	var dataLines []string
	flush := func() error {
		if len(dataLines) == 0 {
			eventType = ""
			return nil
		}
		payload := []byte(strings.Join(dataLines, "\n"))
		dataLines = nil

		if eventType == "" {
			var event struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal(payload, &event); err == nil {
				eventType = event.Type
			}
		}

		switch eventType {
		case "response.output_item.done":
			var event struct {
				Item json.RawMessage `json:"item"`
			}
			if err := json.Unmarshal(payload, &event); err == nil && len(event.Item) > 0 && string(event.Item) != "null" {
				outputItems = append(outputItems, append(json.RawMessage(nil), event.Item...))
			}
		case "response.completed":
			var event struct {
				Response json.RawMessage `json:"response"`
			}
			if err := json.Unmarshal(payload, &event); err != nil {
				return fmt.Errorf("parse ChatGPT completion event: %w", err)
			}
			if len(event.Response) == 0 || string(event.Response) == "null" {
				return fmt.Errorf("ChatGPT completion event has no response")
			}
			completed = append(json.RawMessage(nil), event.Response...)
		case "response.failed", "error":
			return fmt.Errorf("ChatGPT response reported failure")
		}

		eventType = ""
		return nil
	}

	scanner := bufio.NewScanner(bytes.NewReader(streamBody))
	scanner.Buffer(make([]byte, 256*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case line == "":
			if err := flush(); err != nil {
				return nil, 0, 0, err
			}
		case strings.HasPrefix(line, "event:"):
			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			dataLines = append(dataLines, strings.TrimPrefix(line, "data:"))
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, 0, 0, fmt.Errorf("read ChatGPT response stream: %w", err)
	}
	if err := flush(); err != nil {
		return nil, 0, 0, err
	}
	if len(completed) == 0 {
		return nil, 0, 0, fmt.Errorf("ChatGPT response stream ended without response.completed")
	}

	var response map[string]json.RawMessage
	if err := json.Unmarshal(completed, &response); err != nil {
		return nil, 0, 0, fmt.Errorf("parse ChatGPT response: %w", err)
	}
	if response == nil {
		return nil, 0, 0, fmt.Errorf("ChatGPT completion response is empty")
	}

	var status string
	if rawStatus := response["status"]; len(rawStatus) > 0 {
		_ = json.Unmarshal(rawStatus, &status)
	}
	if status == "failed" {
		return nil, 0, 0, fmt.Errorf("ChatGPT response reported failure")
	}

	// The Codex streaming endpoint currently returns output: [] on the final
	// response and carries the actual message in output_item.done.
	var existingOutput []json.RawMessage
	if rawOutput := response["output"]; len(rawOutput) > 0 {
		_ = json.Unmarshal(rawOutput, &existingOutput)
	}
	if len(existingOutput) == 0 && len(outputItems) > 0 {
		encoded, err := json.Marshal(outputItems)
		if err != nil {
			return nil, 0, 0, fmt.Errorf("encode ChatGPT output: %w", err)
		}
		response["output"] = encoded
	}

	if _, ok := response["model"]; !ok && requestedModel != "" {
		encoded, _ := json.Marshal(requestedModel)
		response["model"] = encoded
	}

	var usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	}
	if rawUsage := response["usage"]; len(rawUsage) > 0 {
		_ = json.Unmarshal(rawUsage, &usage)
	}

	body, err := json.Marshal(response)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("encode ChatGPT response: %w", err)
	}
	return body, usage.InputTokens, usage.OutputTokens, nil
}
