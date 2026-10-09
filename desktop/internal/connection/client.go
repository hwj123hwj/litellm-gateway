package connection

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Client snapshots a connection so an in-flight operation cannot switch hosts.
// Credentials stay inside Go and are never part of the UI's profile state.
type Client struct {
	profile storedProfile
	http    *http.Client
}

func (s *Store) Client() (*Client, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, p := range s.state.Profiles {
		if p.ID == s.state.ActiveID {
			return &Client{profile: p, http: s.client}, nil
		}
	}
	return nil, errors.New("请先添加并选择网关连接")
}

func (c *Client) request(ctx context.Context, method, endpoint string, body any) (*http.Response, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.IsAbs() || u.Host != "" || u.Fragment != "" || !strings.HasPrefix(u.Path, "/") || strings.Contains(u.Path, "\\") {
		return nil, errors.New("管理请求路径无效")
	}
	for _, part := range strings.Split(u.Path, "/") {
		if part == "." || part == ".." {
			return nil, errors.New("管理请求路径无效")
		}
	}
	var data []byte
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return nil, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, c.profile.URL+"/admin"+u.String(), bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("管理请求无效")
	}
	req.Header.Set("Authorization", "Bearer "+c.profile.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("无法连接网关，请检查地址、网络和证书")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		resp.Body.Close()
		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			return nil, errors.New("管理 Token 无效或没有访问权限")
		}
		if resp.StatusCode >= 300 && resp.StatusCode < 400 {
			return nil, errors.New("网关返回重定向，请使用最终网关地址")
		}
		return nil, fmt.Errorf("网关请求失败（HTTP %d）", resp.StatusCode)
	}
	return resp, nil
}

func (c *Client) JSON(ctx context.Context, method, endpoint string, body, out any) error {
	resp, err := c.request(ctx, method, endpoint, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if out == nil {
		return nil
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, (16<<20)+1))
	if err != nil {
		return err
	}
	if len(b) > 16<<20 {
		return errors.New("网关响应过大，请缩小查询范围")
	}
	if err := json.Unmarshal(b, out); err != nil {
		return errors.New("网关返回的数据格式无效")
	}
	return nil
}

// Stream consumes SSE frames incrementally. Returning false stops the stream.
func (c *Client) Stream(ctx context.Context, endpoint string, body any, event func([]byte) bool) error {
	resp, err := c.request(ctx, "POST", endpoint, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		return errors.New("网关未返回流式响应")
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	var data []string
	emit := func() bool {
		if len(data) == 0 {
			return true
		}
		b := []byte(strings.Join(data, "\n"))
		data = nil
		return event(b)
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if !emit() {
				return nil
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if err := scanner.Err(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("流式连接中断，请重试")
	}
	emit()
	return nil
}
