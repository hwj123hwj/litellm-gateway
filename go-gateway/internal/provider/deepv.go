package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// DeepVProvider implements the DeepV Server API (GenAI-style protocol). It is
// the aggregation backend behind EasyCode / DeepVCode, so credentials come from
// the local CLI login rather than an API key. The gateway reads the JWT written
// by either EasyCode or the legacy DeepVCode CLI.
type DeepVProvider struct {
	config     *Config
	client     *http.Client
	tokenCache *jwtToken
	workDir    string // 工作目录，用于获取 Git 信息
	boundModel string // 绑定的模型名
}

// jwtToken JWT token 结构（与 ~/.easycode-user/jwt-token.json 一致）
type jwtToken struct {
	AccessToken string `json:"accessToken"`
	ExpiresAt   int64  `json:"expiresAt"`
}

// gitInfo Git 仓库信息
type gitInfo struct {
	Remotes map[string]string `json:"remotes"`
	Branch  string            `json:"branch,omitempty"`
}

// deepVRequest DeepV Server 请求格式（GenAI 格式）
type deepVRequest struct {
	Model             string         `json:"model"`
	Contents          []deepVContent `json:"contents"`
	SystemInstruction *deepVContent  `json:"systemInstruction,omitempty"`
	Config            *deepVConfig   `json:"config,omitempty"`
}

// deepVContent GenAI content 格式
type deepVContent struct {
	Role  string      `json:"role"`
	Parts []deepVPart `json:"parts"`
}

// deepVPart GenAI part 格式
type deepVPart struct {
	Text             string                 `json:"text,omitempty"`
	InlineData       *deepVInlineData       `json:"inlineData,omitempty"`
	FileData         *deepVFileData         `json:"fileData,omitempty"`
	FunctionCall     *deepVFunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *deepVFunctionResponse `json:"functionResponse,omitempty"`
}

type deepVInlineData struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"`
}

type deepVFileData struct {
	MimeType string `json:"mimeType"`
	FileURI  string `json:"fileUri"`
}

// deepVFunctionCall GenAI function call 格式
type deepVFunctionCall struct {
	ID   string                 `json:"id,omitempty"`
	Name string                 `json:"name,omitempty"`
	Args map[string]interface{} `json:"args,omitempty"`
}

// deepVFunctionResponse GenAI function response 格式
type deepVFunctionResponse struct {
	ID       string                 `json:"id,omitempty"`
	Name     string                 `json:"name,omitempty"`
	Response map[string]interface{} `json:"response,omitempty"`
}

// deepVConfig GenAI config 格式
type deepVConfig struct {
	MaxOutputTokens int         `json:"maxOutputTokens,omitempty"`
	Temperature     float64     `json:"temperature,omitempty"`
	TopP            float64     `json:"topP,omitempty"`
	Tools           []deepVTool `json:"tools,omitempty"`
}

// deepVTool GenAI tool 格式
type deepVTool struct {
	FunctionDeclarations []deepVFunctionDecl `json:"functionDeclarations,omitempty"`
}

// deepVFunctionDecl GenAI function declaration 格式
type deepVFunctionDecl struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
}

// deepVResponse DeepV Server 响应格式
type deepVResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text         string             `json:"text,omitempty"`
				InlineData   *deepVInlineData   `json:"inlineData,omitempty"`
				FunctionCall *deepVFunctionCall `json:"functionCall,omitempty"`
			} `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
	UsageMetadata *struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
	} `json:"usageMetadata"`
}

// NewDeepVProvider 创建新的 DeepV 提供商实例
func NewDeepVProvider(config *Config, workDir string, boundModel string) *DeepVProvider {
	timeout := config.RequestTimeout
	if timeout <= 0 {
		timeout = 300 * time.Second
	}
	return &DeepVProvider{
		config:     config,
		client:     &http.Client{Timeout: timeout},
		workDir:    workDir,
		boundModel: boundModel,
	}
}

func (p *DeepVProvider) Name() string       { return p.config.Name }
func (p *DeepVProvider) URL() string        { return p.config.URL }
func (p *DeepVProvider) APIKey() string     { return p.config.APIKey }
func (p *DeepVProvider) UseBearer() bool    { return p.config.UseBearer }
func (p *DeepVProvider) BoundModel() string { return p.boundModel }

// Capabilities declares model metadata so /v1/models and capability-aware
// routing can tell text-only from vision-enabled DeepV models.
func (p *DeepVProvider) Capabilities() []string {
	if p.boundModel == "deepseek-v4-flash-vision-exp" {
		return []string{CapabilityText, CapabilityVision, CapabilityToolCall, CapabilityStreaming, CapabilityReasoning}
	}
	return []string{CapabilityText, CapabilityToolCall, CapabilityStreaming, CapabilityReasoning}
}

// ForwardRequest 转发请求到 DeepV Server（非流式）
func (p *DeepVProvider) ForwardRequest(ctx context.Context, req *Request) (*Response, error) {
	deepVReq, err := p.convertRequest(req)
	if err != nil {
		return nil, fmt.Errorf("convert request: %w", err)
	}

	reqBody, err := json.Marshal(deepVReq)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.config.URL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if err := p.setHeaders(httpReq); err != nil {
		return nil, fmt.Errorf("set headers: %w", err)
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, NewHTTPError(p.Name(), resp, respBody)
	}

	return p.parseResponse(respBody, req.Model)
}

// convertRequest 将 Anthropic 格式转换为 GenAI 格式
func (p *DeepVProvider) convertRequest(req *Request) (*deepVRequest, error) {
	model := p.boundModel
	if model == "" {
		model = req.Model
	}

	result := &deepVRequest{Model: model}

	// 记录 tool_use id 到 name 的映射，用于 tool_result
	toolUseIDToName := make(map[string]string)

	for _, msg := range req.Messages {
		role := msg.Role
		if role == "assistant" {
			role = "model"
		}

		content := deepVContent{Role: role}

		for _, block := range msg.Content.Blocks() {
			switch block.Type {
			case "text":
				if block.Text == "" {
					continue
				}
				content.Parts = append(content.Parts, deepVPart{Text: block.Text})
			case "tool_use":
				toolUseIDToName[block.ID] = block.Name
				var args map[string]interface{}
				if len(block.Input) > 0 {
					_ = json.Unmarshal(block.Input, &args)
				}
				if args == nil {
					args = make(map[string]interface{})
				}
				content.Parts = append(content.Parts, deepVPart{
					FunctionCall: &deepVFunctionCall{ID: block.ID, Name: block.Name, Args: args},
				})
			case "tool_result":
				toolName := toolUseIDToName[block.ToolUseID]
				if toolName == "" {
					toolName = block.ToolUseID
				}
				resultStr := block.ContentStr
				if resultStr == "" && len(block.ContentBlocks) > 0 {
					for _, cb := range block.ContentBlocks {
						if cb.Type == "text" {
							resultStr += cb.Text
						}
					}
				}
				content.Parts = append(content.Parts, deepVPart{
					FunctionResponse: &deepVFunctionResponse{
						ID:       block.ToolUseID,
						Name:     toolName,
						Response: map[string]interface{}{"result": resultStr},
					},
				})
			case "image", "image_url", "input_image":
				if part := p.convertImagePart(block); part != nil {
					content.Parts = append(content.Parts, *part)
				}
			}
		}

		// DeepV Server 要求每个 content 必须有 parts
		if len(content.Parts) == 0 {
			continue
		}
		result.Contents = append(result.Contents, content)
	}

	// 转换 system（支持字符串和数组格式）
	if systemRaw, ok := req.RawField("system"); ok {
		var systemParts []deepVPart

		var systemStr string
		if err := json.Unmarshal(systemRaw, &systemStr); err == nil && systemStr != "" {
			systemParts = append(systemParts, deepVPart{Text: systemStr})
		} else {
			var systemArray []map[string]interface{}
			if err := json.Unmarshal(systemRaw, &systemArray); err == nil {
				for _, item := range systemArray {
					if text, ok := item["text"].(string); ok && text != "" {
						systemParts = append(systemParts, deepVPart{Text: text})
					}
				}
			}
		}
		if len(systemParts) > 0 {
			result.SystemInstruction = &deepVContent{Parts: systemParts}
		}
	}

	result.Config = &deepVConfig{MaxOutputTokens: req.MaxTokens}
	if temperatureRaw, ok := req.RawField("temperature"); ok {
		var temperature float64
		if json.Unmarshal(temperatureRaw, &temperature) == nil {
			result.Config.Temperature = temperature
		}
	}
	if topPRaw, ok := req.RawField("top_p"); ok {
		var topP float64
		if json.Unmarshal(topPRaw, &topP) == nil {
			result.Config.TopP = topP
		}
	}

	// 转换 tools（从 raw 字段获取，Anthropic input_schema 格式）
	if toolsRaw, ok := req.RawField("tools"); ok {
		var tools []struct {
			Name        string                 `json:"name"`
			Description string                 `json:"description"`
			InputSchema map[string]interface{} `json:"input_schema"`
		}
		if err := json.Unmarshal(toolsRaw, &tools); err == nil && len(tools) > 0 {
			tool := deepVTool{FunctionDeclarations: make([]deepVFunctionDecl, len(tools))}
			for i, t := range tools {
				tool.FunctionDeclarations[i] = deepVFunctionDecl{
					Name:        t.Name,
					Description: t.Description,
					Parameters:  t.InputSchema,
				}
			}
			result.Config.Tools = []deepVTool{tool}
		}
	}

	return result, nil
}

// convertImagePart 把 Anthropic/OpenAI 图片块转成 GenAI inlineData / fileData。
// 本地 data URI 走 inlineData（base64），远程 URL 走 fileData。
func (p *DeepVProvider) convertImagePart(block ContentBlock) *deepVPart {
	if len(block.Source) > 0 {
		var source struct {
			Type      string `json:"type"`
			MediaType string `json:"media_type"`
			Data      string `json:"data"`
		}
		if json.Unmarshal(block.Source, &source) == nil && source.Data != "" {
			return &deepVPart{InlineData: &deepVInlineData{MimeType: source.MediaType, Data: source.Data}}
		}
	}
	if len(block.ImageURL) > 0 {
		var imageURL struct {
			URL string `json:"url"`
		}
		if json.Unmarshal(block.ImageURL, &imageURL) == nil && imageURL.URL != "" {
			if mime, data, ok := parseDataURL(imageURL.URL); ok {
				return &deepVPart{InlineData: &deepVInlineData{MimeType: mime, Data: data}}
			}
			return &deepVPart{FileData: &deepVFileData{MimeType: guessImageMime(imageURL.URL), FileURI: imageURL.URL}}
		}
	}
	return nil
}

func parseDataURL(raw string) (mime, data string, ok bool) {
	const prefix = "data:"
	if !strings.HasPrefix(raw, prefix) {
		return "", "", false
	}
	rest := strings.TrimPrefix(raw, prefix)
	comma := strings.IndexByte(rest, ',')
	if comma < 0 {
		return "", "", false
	}
	meta := rest[:comma]
	payload := rest[comma+1:]
	if !strings.Contains(meta, ";base64") {
		return "", "", false
	}
	return strings.TrimSuffix(meta, ";base64"), payload, true
}

func guessImageMime(url string) string {
	lower := strings.ToLower(url)
	switch {
	case strings.Contains(lower, ".png"):
		return "image/png"
	case strings.Contains(lower, ".gif"):
		return "image/gif"
	case strings.Contains(lower, ".webp"):
		return "image/webp"
	default:
		return "image/jpeg"
	}
}

// parseResponse 将 GenAI 响应转换为 Anthropic 格式
func (p *DeepVProvider) parseResponse(body []byte, model string) (*Response, error) {
	var genaiResp deepVResponse
	if err := json.Unmarshal(body, &genaiResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	result := &Response{
		ID:    fmt.Sprintf("deepv-%d", time.Now().UnixNano()),
		Type:  "message",
		Role:  "assistant",
		Model: model,
	}
	if len(genaiResp.Candidates) > 0 {
		result.StopReason = mapDeepVFinishReason(genaiResp.Candidates[0].FinishReason)
	}
	if genaiResp.UsageMetadata != nil {
		result.Usage.InputTokens = genaiResp.UsageMetadata.PromptTokenCount
		result.Usage.OutputTokens = genaiResp.UsageMetadata.CandidatesTokenCount
	}

	for _, candidate := range genaiResp.Candidates {
		for _, part := range candidate.Content.Parts {
			switch {
			case part.Text != "":
				result.Content = append(result.Content, ContentBlock{Type: "text", Text: part.Text})
			case part.FunctionCall != nil:
				inputJSON, _ := json.Marshal(part.FunctionCall.Args)
				result.Content = append(result.Content, ContentBlock{
					Type:  "tool_use",
					ID:    part.FunctionCall.Name + "-" + time.Now().Format("20060102150405"),
					Name:  part.FunctionCall.Name,
					Input: inputJSON,
				})
			case part.InlineData != nil:
				source, _ := json.Marshal(map[string]interface{}{
					"type":       "base64",
					"media_type": part.InlineData.MimeType,
					"data":       part.InlineData.Data,
				})
				result.Content = append(result.Content, ContentBlock{
					Type:   "image",
					Source: source,
				})
			}
		}
	}

	return result, nil
}

// setHeaders 设置请求头，包括认证和 Git 信息
func (p *DeepVProvider) setHeaders(req *http.Request) error {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "DeepVCode/CLI/1.0.338 (darwin; arm64)")
	req.Header.Set("X-Client-Version", "1.0.338")

	token, err := p.getAccessToken()
	if err != nil {
		return fmt.Errorf("get access token: %w", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	if gitInfo := p.getGitInfo(); gitInfo != nil {
		if remotesJSON, err := json.Marshal(gitInfo.Remotes); err == nil {
			req.Header.Set("X-Git-Remotes", string(remotesJSON))
		}
		if gitInfo.Branch != "" {
			req.Header.Set("X-Git-Branch", gitInfo.Branch)
		}
	}

	return nil
}

// getAccessToken 获取访问令牌，缓存到过期为止。
// 优先读取现代 Easy Code 路径，其次回退到旧版 DeepVCode 路径。
func (p *DeepVProvider) getAccessToken() (string, error) {
	nowSec := time.Now().Unix()
	if p.tokenCache != nil {
		expiresAt := p.tokenCache.ExpiresAt
		if expiresAt > 9999999999 { // 毫秒转秒
			expiresAt = expiresAt / 1000
		}
		if nowSec < expiresAt {
			return p.tokenCache.AccessToken, nil
		}
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}

	tokenPath := filepath.Join(homeDir, ".easycode-user", "jwt-token.json")
	data, err := os.ReadFile(tokenPath)
	if err != nil {
		tokenPath = filepath.Join(homeDir, ".deepv", "jwt-token.json")
		data, err = os.ReadFile(tokenPath)
		if err != nil {
			return "", fmt.Errorf("read token file (checked both ~/.easycode-user and ~/.deepv): %w", err)
		}
	}

	var token jwtToken
	if err := json.Unmarshal(data, &token); err != nil {
		return "", fmt.Errorf("parse token: %w", err)
	}

	p.tokenCache = &token
	return token.AccessToken, nil
}

// getGitInfo 获取 Git 仓库信息
func (p *DeepVProvider) getGitInfo() *gitInfo {
	info := &gitInfo{Remotes: make(map[string]string)}

	workDir := p.workDir
	if workDir == "" {
		workDir, _ = os.Getwd()
	}

	info.Remotes = p.getGitRemotes(workDir)
	info.Branch = p.getGitBranch(workDir)

	if len(info.Remotes) == 0 {
		return nil
	}
	return info
}

// getGitRemotes 获取 Git remote 列表
func (p *DeepVProvider) getGitRemotes(dir string) map[string]string {
	remotes := make(map[string]string)
	cmd := exec.Command("git", "remote", "-v")
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		return remotes
	}
	for _, line := range strings.Split(string(output), "\n") {
		if !strings.Contains(line, "(fetch)") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			remotes[parts[0]] = parts[1]
		}
	}
	return remotes
}

// getGitBranch 获取当前分支
func (p *DeepVProvider) getGitBranch(dir string) string {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

// IsHealthy 检查提供商是否健康（token 是否存在且未过期）
func (p *DeepVProvider) IsHealthy(ctx context.Context) bool {
	token, err := p.getAccessToken()
	if err != nil || token == "" {
		return false
	}
	return true
}

// ForwardStream 实现流式请求（StreamProvider 接口），把 GenAI SSE 转成 Anthropic SSE。
func (p *DeepVProvider) ForwardStream(ctx context.Context, req *Request, w io.Writer) error {
	deepVReq, err := p.convertRequest(req)
	if err != nil {
		return fmt.Errorf("convert request: %w", err)
	}

	reqBody, err := json.Marshal(deepVReq)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	streamURL := strings.Replace(p.config.URL, "/messages", "/stream", 1)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, streamURL, bytes.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	if err := p.setHeaders(httpReq); err != nil {
		return fmt.Errorf("set headers: %w", err)
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return NewHTTPError(p.Name(), resp, b)
	}

	return p.convertStream(resp.Body, w, req.Model)
}

// convertStream 转换 GenAI SSE 流为 Anthropic SSE 流
func (p *DeepVProvider) convertStream(r io.Reader, w io.Writer, model string) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	writer := bufio.NewWriter(w)
	defer writer.Flush()

	msgID := fmt.Sprintf("msg_deepv_%d", time.Now().UnixNano())
	p.writeAnthropicEvent(writer, "message_start", map[string]interface{}{
		"message": map[string]interface{}{
			"id":            msgID,
			"type":          "message",
			"role":          "assistant",
			"model":         model,
			"content":       []interface{}{},
			"stop_reason":   nil,
			"stop_sequence": nil,
			"usage": map[string]interface{}{
				"input_tokens":  0,
				"output_tokens": 0,
			},
		},
	})

	blockIndex := -1
	openTextBlock := false
	stopReason := "end_turn"
	var outputTokens int

	flushTextBlock := func() {
		if openTextBlock {
			p.writeAnthropicEvent(writer, "content_block_stop", map[string]interface{}{"index": 0})
			openTextBlock = false
		}
	}

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var genaiResp deepVResponse
		if err := json.Unmarshal([]byte(data), &genaiResp); err != nil {
			continue
		}
		if genaiResp.UsageMetadata != nil {
			outputTokens = genaiResp.UsageMetadata.CandidatesTokenCount
		}

		for _, candidate := range genaiResp.Candidates {
			if candidate.FinishReason != "" {
				if reason := mapDeepVFinishReason(candidate.FinishReason); reason != "" {
					stopReason = reason
				}
			}
			for _, part := range candidate.Content.Parts {
				switch {
				case part.Text != "":
					if !openTextBlock {
						p.writeAnthropicEvent(writer, "content_block_start", map[string]interface{}{
							"index":         0,
							"content_block": map[string]interface{}{"type": "text", "text": ""},
						})
						openTextBlock = true
					}
					p.writeAnthropicEvent(writer, "content_block_delta", map[string]interface{}{
						"index": 0,
						"delta": map[string]interface{}{"type": "text_delta", "text": part.Text},
					})
				case part.FunctionCall != nil:
					flushTextBlock()
					blockIndex++
					inputJSON, _ := json.Marshal(part.FunctionCall.Args)
					p.writeAnthropicEvent(writer, "content_block_start", map[string]interface{}{
						"index": blockIndex,
						"content_block": map[string]interface{}{
							"type":  "tool_use",
							"id":    part.FunctionCall.ID,
							"name":  part.FunctionCall.Name,
							"input": json.RawMessage(inputJSON),
						},
					})
					p.writeAnthropicEvent(writer, "content_block_stop", map[string]interface{}{"index": blockIndex})
				case part.InlineData != nil:
					flushTextBlock()
					blockIndex++
					source, _ := json.Marshal(map[string]interface{}{
						"type":       "base64",
						"media_type": part.InlineData.MimeType,
						"data":       part.InlineData.Data,
					})
					p.writeAnthropicEvent(writer, "content_block_start", map[string]interface{}{
						"index": blockIndex,
						"content_block": map[string]interface{}{
							"type":   "image",
							"source": json.RawMessage(source),
						},
					})
					p.writeAnthropicEvent(writer, "content_block_stop", map[string]interface{}{"index": blockIndex})
				}
			}
		}
	}
	flushTextBlock()

	p.writeAnthropicEvent(writer, "message_delta", map[string]interface{}{
		"delta": map[string]interface{}{"stop_reason": stopReason, "stop_sequence": nil},
		"usage": map[string]interface{}{"output_tokens": outputTokens},
	})
	p.writeAnthropicEvent(writer, "message_stop", map[string]interface{}{})

	return scanner.Err()
}

// writeAnthropicEvent 写入 Anthropic SSE 事件
func (p *DeepVProvider) writeAnthropicEvent(w *bufio.Writer, eventType string, data map[string]interface{}) {
	data["type"] = eventType
	jsonData, _ := json.Marshal(data)
	_, _ = w.WriteString("event: " + eventType + "\n")
	_, _ = w.WriteString("data: " + string(jsonData) + "\n\n")
	_ = w.Flush()
}

func mapDeepVFinishReason(genai string) string {
	switch strings.ToUpper(strings.TrimSpace(genai)) {
	case "TOOL_CALL", "FUNCTION_CALL":
		return "tool_use"
	default:
		return "end_turn"
	}
}
