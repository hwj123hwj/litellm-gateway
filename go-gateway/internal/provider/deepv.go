package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// DeepVProvider 实现 DeepV Server API 的提供商
// 支持自动读取本地认证 token 和 Git 仓库信息
type DeepVProvider struct {
	config     *Config
	client     *http.Client
	tokenCache *jwtToken
	workDir    string // 工作目录，用于获取 Git 信息
	boundModel string // 绑定的模型名
}

// jwtToken JWT token 结构
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
	Model             string          `json:"model"`
	Contents          []deepVContent  `json:"contents"`
	SystemInstruction *deepVContent   `json:"systemInstruction,omitempty"`
	Config            *deepVConfig    `json:"config,omitempty"`
}

// deepVContent GenAI content 格式
type deepVContent struct {
	Role  string      `json:"role"`
	Parts []deepVPart `json:"parts"`
}

// deepVPart GenAI part 格式
type deepVPart struct {
	Text string `json:"text,omitempty"`
}

// deepVConfig GenAI config 格式
type deepVConfig struct {
	MaxOutputTokens int          `json:"maxOutputTokens,omitempty"`
	Temperature     float64      `json:"temperature,omitempty"`
	TopP            float64      `json:"topP,omitempty"`
	Tools           []deepVTool  `json:"tools,omitempty"`
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
				Text string `json:"text,omitempty"`
				FunctionCall *struct {
					ID   string                 `json:"id,omitempty"`
					Name string                 `json:"name,omitempty"`
					Args map[string]interface{} `json:"args,omitempty"`
				} `json:"functionCall,omitempty"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
}

// NewDeepVProvider 创建新的 DeepV 提供商实例
func NewDeepVProvider(config *Config, workDir string, boundModel string) *DeepVProvider {
	return &DeepVProvider{
		config:     config,
		client:     &http.Client{Timeout: 300 * time.Second},
		workDir:    workDir,
		boundModel: boundModel,
	}
}

func (p *DeepVProvider) Name() string        { return p.config.Name }
func (p *DeepVProvider) URL() string         { return p.config.URL }
func (p *DeepVProvider) APIKey() string      { return p.config.APIKey }
func (p *DeepVProvider) UseBearer() bool     { return p.config.UseBearer }
func (p *DeepVProvider) BoundModel() string  { return p.boundModel }

// ForwardRequest 转发请求到 DeepV Server（非流式）
func (p *DeepVProvider) ForwardRequest(ctx context.Context, req *Request) (*Response, error) {
	// 1. 转换请求格式
	deepVReq, err := p.convertRequest(req)
	if err != nil {
		return nil, fmt.Errorf("convert request: %w", err)
	}

	reqBody, err := json.Marshal(deepVReq)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	// DEBUG: 打印请求体
	log.Printf("[DeepV] Request body: %s", string(reqBody))

	// 2. 创建 HTTP 请求
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.config.URL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	// 3. 设置 headers
	if err := p.setHeaders(httpReq); err != nil {
		return nil, fmt.Errorf("set headers: %w", err)
	}

	// 4. 发送请求
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	// 5. 读取响应
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	// DEBUG: 打印响应
	log.Printf("[DeepV] Response status: %d, body: %s", resp.StatusCode, string(respBody))

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("deepv error %d: %s", resp.StatusCode, string(respBody))
	}

	// 6. 解析响应并转换回 Anthropic 格式
	return p.parseResponse(respBody, req.Model)
}

// convertRequest 将 Anthropic 格式转换为 GenAI 格式
func (p *DeepVProvider) convertRequest(req *Request) (*deepVRequest, error) {
	// 使用绑定的模型名（如果有的话）
	model := p.boundModel
	if model == "" {
		model = req.Model
	}

	result := &deepVRequest{
		Model: model,
	}

	// 转换 contents
	for _, msg := range req.Messages {
		content := deepVContent{
			Role: msg.Role,
		}

		// 处理消息内容
		blocks := msg.Content.Blocks()
		for _, block := range blocks {
			if block.Type == "text" {
				content.Parts = append(content.Parts, deepVPart{Text: block.Text})
			}
		}

		result.Contents = append(result.Contents, content)
	}

	// 转换 system（从 raw 字段获取）
	if systemRaw, ok := req.RawField("system"); ok {
		var systemStr string
		if err := json.Unmarshal(systemRaw, &systemStr); err == nil && systemStr != "" {
			result.SystemInstruction = &deepVContent{
				Parts: []deepVPart{{Text: systemStr}},
			}
		}
	}

	// 转换 config
	result.Config = &deepVConfig{
		MaxOutputTokens: req.MaxTokens,
	}

	// 转换 tools（从 raw 字段获取）
	if toolsRaw, ok := req.RawField("tools"); ok {
		var tools []struct {
			Name        string                 `json:"name"`
			Description string                 `json:"description"`
			InputSchema map[string]interface{} `json:"input_schema"`
		}
		if err := json.Unmarshal(toolsRaw, &tools); err == nil && len(tools) > 0 {
			tool := deepVTool{
				FunctionDeclarations: make([]deepVFunctionDecl, len(tools)),
			}
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

// parseResponse 将 GenAI 响应转换为 Anthropic 格式
func (p *DeepVProvider) parseResponse(body []byte, model string) (*Response, error) {
	var genaiResp deepVResponse
	if err := json.Unmarshal(body, &genaiResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	// 转换为 Anthropic 格式
	result := &Response{
		ID:    fmt.Sprintf("deepv-%d", time.Now().UnixNano()),
		Type:  "message",
		Role:  "assistant",
		Model: model,
	}

	for _, candidate := range genaiResp.Candidates {
		for _, part := range candidate.Content.Parts {
			// 处理文本
			if part.Text != "" {
				result.Content = append(result.Content, ContentBlock{
					Type: "text",
					Text: part.Text,
				})
			}
			// 处理工具调用
			if part.FunctionCall != nil {
				inputJSON, _ := json.Marshal(part.FunctionCall.Args)
				result.Content = append(result.Content, ContentBlock{
					Type:  "tool_use",
					ID:    part.FunctionCall.Name + "-" + time.Now().Format("20060102150405"),
					Name:  part.FunctionCall.Name,
					Input: inputJSON,
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

	// 1. 设置认证 token
	token, err := p.getAccessToken()
	if err != nil {
		return fmt.Errorf("get access token: %w", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	// 2. 设置 Git headers
	gitInfo := p.getGitInfo()
	if gitInfo != nil {
		if remotesJSON, err := json.Marshal(gitInfo.Remotes); err == nil {
			req.Header.Set("X-Git-Remotes", string(remotesJSON))
		}
		if gitInfo.Branch != "" {
			req.Header.Set("X-Git-Branch", gitInfo.Branch)
		}
	}

	return nil
}

// getAccessToken 获取访问令牌
func (p *DeepVProvider) getAccessToken() (string, error) {
	// 检查缓存
	if p.tokenCache != nil && time.Now().Unix() < p.tokenCache.ExpiresAt {
		return p.tokenCache.AccessToken, nil
	}

	// 从文件读取
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}

	tokenPath := filepath.Join(homeDir, ".deepv", "jwt-token.json")
	data, err := os.ReadFile(tokenPath)
	if err != nil {
		return "", fmt.Errorf("read token file: %w", err)
	}

	var token jwtToken
	if err := json.Unmarshal(data, &token); err != nil {
		return "", fmt.Errorf("parse token: %w", err)
	}

	// 更新缓存
	p.tokenCache = &token

	return token.AccessToken, nil
}

// getGitInfo 获取 Git 仓库信息
func (p *DeepVProvider) getGitInfo() *gitInfo {
	info := &gitInfo{
		Remotes: make(map[string]string),
	}

	// 获取工作目录
	workDir := p.workDir
	if workDir == "" {
		workDir, _ = os.Getwd()
	}

	// 获取 remotes
	remotes := p.getGitRemotes(workDir)
	if len(remotes) > 0 {
		info.Remotes = remotes
	}

	// 获取 branch
	branch := p.getGitBranch(workDir)
	if branch != "" {
		info.Branch = branch
	}

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

	// 解析输出
	// 格式: origin	https://github.com/org/repo.git (fetch)
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		// 只处理 fetch 行
		if strings.Contains(line, "(fetch)") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				name := parts[0]
				url := parts[1]
				remotes[name] = url
			}
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

// IsHealthy 检查提供商是否健康
func (p *DeepVProvider) IsHealthy(ctx context.Context) bool {
	// 检查 token 是否存在
	token, err := p.getAccessToken()
	if err != nil || token == "" {
		return false
	}
	return true
}

// ForwardStream 实现流式请求（StreamProvider 接口）
func (p *DeepVProvider) ForwardStream(ctx context.Context, req *Request, w io.Writer) error {
	// 1. 转换请求格式
	deepVReq, err := p.convertRequest(req)
	if err != nil {
		return fmt.Errorf("convert request: %w", err)
	}

	reqBody, err := json.Marshal(deepVReq)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	// 2. 使用流式端点
	streamURL := strings.Replace(p.config.URL, "/messages", "/stream", 1)

	// 3. 创建 HTTP 请求
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, streamURL, bytes.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	// 4. 设置 headers
	if err := p.setHeaders(httpReq); err != nil {
		return fmt.Errorf("set headers: %w", err)
	}

	// 5. 发送请求
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("deepv stream error %d: %s", resp.StatusCode, string(body))
	}

	// 6. 转换 SSE 流：GenAI 格式 -> Anthropic 格式
	return p.convertStream(resp.Body, w, req.Model)
}

// convertStream 转换 GenAI SSE 流为 Anthropic SSE 流
func (p *DeepVProvider) convertStream(r io.Reader, w io.Writer, model string) error {
	scanner := bufio.NewScanner(r)
	writer := bufio.NewWriter(w)
	defer writer.Flush()

	var textBuffer strings.Builder
	_ = fmt.Sprintf("deepv-%d", time.Now().UnixNano()) // message ID for future use

	for scanner.Scan() {
		line := scanner.Text()

		// 跳过空行
		if line == "" {
			continue
		}

		// 解析 GenAI SSE
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				// 发送 Anthropic 结束事件
				p.writeAnthropicEvent(writer, "message_stop", map[string]interface{}{})
				break
			}

			var genaiResp deepVResponse
			if err := json.Unmarshal([]byte(data), &genaiResp); err != nil {
				continue
			}

			// 提取文本
			for _, candidate := range genaiResp.Candidates {
				for _, part := range candidate.Content.Parts {
					if part.Text != "" {
						textBuffer.WriteString(part.Text)

						// 发送 content_block_delta 事件
						p.writeAnthropicEvent(writer, "content_block_delta", map[string]interface{}{
							"index": 0,
							"delta": map[string]interface{}{
								"type": "text_delta",
								"text": part.Text,
							},
						})
					}

					// 处理 function call
					if part.FunctionCall != nil {
						inputJSON, _ := json.Marshal(part.FunctionCall.Args)
						p.writeAnthropicEvent(writer, "content_block_start", map[string]interface{}{
							"index": 0,
							"content_block": map[string]interface{}{
								"type":  "tool_use",
								"id":    part.FunctionCall.ID,
								"name":  part.FunctionCall.Name,
								"input": json.RawMessage(inputJSON),
							},
						})
					}
				}
			}
		}
	}

	// 发送最终消息
	p.writeAnthropicEvent(writer, "message_delta", map[string]interface{}{
		"delta": map[string]interface{}{
			"stop_reason": "end_turn",
		},
		"usage": map[string]interface{}{
			"output_tokens": len(textBuffer.String()) / 4, // 粗略估算
		},
	})

	return scanner.Err()
}

// writeAnthropicEvent 写入 Anthropic SSE 事件
func (p *DeepVProvider) writeAnthropicEvent(w *bufio.Writer, eventType string, data map[string]interface{}) {
	data["type"] = eventType
	jsonData, _ := json.Marshal(data)
	w.WriteString("event: " + eventType + "\n")
	w.WriteString("data: " + string(jsonData) + "\n\n")
	w.Flush()
}
