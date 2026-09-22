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
	// Reasoning 是思考模式的思维链。上游（easyrouterio Responses API）要求
	// 带工具调用的历史轮次把 reasoning 原样回传，缺失会让整个请求被拒。
	Reasoning        string                 `json:"reasoning,omitempty"`
	Text             string                 `json:"text,omitempty"`
	InlineData       *deepVInlineData       `json:"inlineData,omitempty"`
	FileData         *deepVFileData         `json:"fileData,omitempty"`
	FunctionCall     *deepVFunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *deepVFunctionResponse `json:"functionResponse,omitempty"`
}

// placeholderReasoning 补进"缺思维链的工具调用历史轮"的最小占位文本。
// 实测非空短文本即可通过上游思考模式校验。
const placeholderReasoning = "."

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
				Reasoning    string             `json:"reasoning,omitempty"`
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

// Capabilities declares model metadata for /v1/models and capability-aware
// routing. The bound DeepV model is natively multimodal, so vision is always
// advertised -- image requests must never land on a text-only view of it.
func (p *DeepVProvider) Capabilities() []string {
	return []string{CapabilityText, CapabilityVision, CapabilityToolCall, CapabilityStreaming, CapabilityReasoning}
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

// normalizeToolResultTurns 把"整轮只含 tool_result"的连续 user 消息合成一轮。
// 上游 GenAI 格式要求同一 model 轮的并行工具调用、其结果必须落在同一个 user 轮里；
// 三种客户端协议都可能把 N 个结果发成 N 个独立轮次（Responses 的多个
// function_call_output、OpenAI 的多个 role=tool、Anthropic 的多个 tool_result
// user 轮），原样透传会被上游判成 "Duplicate tool output for call_id" 并整单拒绝。
// 只聚合纯 tool_result 轮，遇到普通 user 轮立即停止，避免吞掉后续用户输入。
func normalizeToolResultTurns(msgs []Message) []Message {
	if len(msgs) <= 1 {
		return msgs
	}
	isToolResultOnly := func(m Message) bool {
		if m.Role != "user" {
			return false
		}
		blocks := m.Content.Blocks()
		if len(blocks) == 0 {
			return false
		}
		for _, b := range blocks {
			if b.Type != "tool_result" {
				return false
			}
		}
		return true
	}

	var result []Message
	i := 0
	for i < len(msgs) {
		if isToolResultOnly(msgs[i]) {
			j := i
			var merged []ContentBlock
			for j < len(msgs) && isToolResultOnly(msgs[j]) {
				merged = append(merged, msgs[j].Content.Blocks()...)
				j++
			}
			if j-i > 1 {
				result = append(result, Message{
					Role:    "user",
					Content: NewBlocksContent(merged),
				})
				i = j
				continue
			}
		}
		result = append(result, msgs[i])
		i++
	}
	return result
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

	for _, msg := range normalizeToolResultTurns(req.Messages) {
		role := msg.Role
		if role == "assistant" {
			role = "model"
		}

		content := deepVContent{Role: role}
		var reasoningParts []deepVPart
		hasFunctionCall := false

		for _, block := range msg.Content.Blocks() {
			switch block.Type {
			case "thinking", "reasoning":
				// 思考模式的历史轮次必须回传思维链，且只对 model 轮有意义。
				if block.Thinking == "" || role != "model" {
					break
				}
				reasoningParts = append(reasoningParts, deepVPart{Reasoning: block.Thinking})
			case "text":
				if block.Text == "" {
					continue
				}
				content.Parts = append(content.Parts, deepVPart{Text: block.Text})
			case "tool_use":
				hasFunctionCall = true
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
				var toolImageParts []deepVPart
				if resultStr == "" || len(block.ContentBlocks) > 0 {
					for _, cb := range block.ContentBlocks {
						switch cb.Type {
						case "text":
							resultStr += cb.Text
						case "image", "image_url", "input_image", "output_image":
							// 工具结果带图（zcode Read 图片文件）时把图片随
							// functionResponse 一起上送，多模态模型才能真正看到。
							if part := p.convertImagePart(cb); part != nil {
								toolImageParts = append(toolImageParts, *part)
							}
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
				content.Parts = append(content.Parts, toolImageParts...)
			case "image", "image_url", "input_image", "output_image":
				if part := p.convertImagePart(block); part != nil {
					content.Parts = append(content.Parts, *part)
				}
			}
		}

		// 上游思考模式要求带工具调用的 model 轮必须带 reasoning part，缺失
		// 会让整个请求被拒。部分客户端（如 zcode 走 Responses 协议回传历史）
		// 不回传思维链，此时注入最小占位文本，上游校验的是存在性。
		if role == "model" && hasFunctionCall && len(reasoningParts) == 0 {
			reasoningParts = append(reasoningParts, deepVPart{Reasoning: placeholderReasoning})
		}

		// 上游要求 reasoning 排在同一轮其他 part 之前。
		if len(reasoningParts) > 0 {
			content.Parts = append(reasoningParts, content.Parts...)
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

// deepVToolCallID 返回回给客户端的 tool_use id。优先用上游自带 id；缺失时
// 本地生成，并用 seen/seq 保证同一响应内多个并行调用不会重号。
func deepVToolCallID(call *deepVFunctionCall, seen map[string]bool, seq *int) string {
	if call.ID != "" && !seen[call.ID] {
		seen[call.ID] = true
		return call.ID
	}
	for {
		*seq++
		candidate := fmt.Sprintf("%s-%d", call.Name, *seq)
		if !seen[candidate] {
			seen[candidate] = true
			return candidate
		}
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

	// 上游的 functionCall 自带唯一 id，必须原样保留：同一轮并行调用如果回给
	// 客户端两个相同 id，客户端后续的 tool_result 就无法区分是哪一个的结果。
	// 个别响应不带 id 时才本地生成，并保证同一响应内不重号。
	seenToolCallIDs := make(map[string]bool)
	toolCallSeq := 0

	for _, candidate := range genaiResp.Candidates {
		for _, part := range candidate.Content.Parts {
			switch {
			case part.Reasoning != "":
				result.Content = append(result.Content, ContentBlock{Type: "thinking", Thinking: part.Reasoning})
			case part.Text != "":
				result.Content = append(result.Content, ContentBlock{Type: "text", Text: part.Text})
			case part.FunctionCall != nil:
				inputJSON, _ := json.Marshal(part.FunctionCall.Args)
				result.Content = append(result.Content, ContentBlock{
					Type:  "tool_use",
					ID:    deepVToolCallID(part.FunctionCall, seenToolCallIDs, &toolCallSeq),
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
	openTextIndex := -1
	openThinkingIndex := -1
	stopReason := "end_turn"
	var outputTokens int

	openBlock := func(block map[string]interface{}) int {
		blockIndex++
		p.writeAnthropicEvent(writer, "content_block_start", map[string]interface{}{
			"index":         blockIndex,
			"content_block": block,
		})
		return blockIndex
	}
	closeBlock := func(index int) {
		p.writeAnthropicEvent(writer, "content_block_stop", map[string]interface{}{"index": index})
	}
	flushTextBlock := func() {
		if openTextIndex >= 0 {
			closeBlock(openTextIndex)
			openTextIndex = -1
		}
	}
	flushThinkingBlock := func() {
		if openThinkingIndex >= 0 {
			closeBlock(openThinkingIndex)
			openThinkingIndex = -1
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
				case part.Reasoning != "":
					if openThinkingIndex < 0 {
						openThinkingIndex = openBlock(map[string]interface{}{"type": "thinking", "thinking": ""})
					}
					p.writeAnthropicEvent(writer, "content_block_delta", map[string]interface{}{
						"index": openThinkingIndex,
						"delta": map[string]interface{}{"type": "thinking_delta", "thinking": part.Reasoning},
					})
				case part.Text != "":
					flushThinkingBlock()
					if openTextIndex < 0 {
						openTextIndex = openBlock(map[string]interface{}{"type": "text", "text": ""})
					}
					p.writeAnthropicEvent(writer, "content_block_delta", map[string]interface{}{
						"index": openTextIndex,
						"delta": map[string]interface{}{"type": "text_delta", "text": part.Text},
					})
				case part.FunctionCall != nil:
					flushTextBlock()
					flushThinkingBlock()
					inputJSON, _ := json.Marshal(part.FunctionCall.Args)
					closeBlock(openBlock(map[string]interface{}{
						"type":  "tool_use",
						"id":    part.FunctionCall.ID,
						"name":  part.FunctionCall.Name,
						"input": json.RawMessage(inputJSON),
					}))
				case part.InlineData != nil:
					flushTextBlock()
					flushThinkingBlock()
					source, _ := json.Marshal(map[string]interface{}{
						"type":       "base64",
						"media_type": part.InlineData.MimeType,
						"data":       part.InlineData.Data,
					})
					closeBlock(openBlock(map[string]interface{}{
						"type":   "image",
						"source": json.RawMessage(source),
					}))
				}
			}
		}
	}
	flushTextBlock()
	flushThinkingBlock()

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
