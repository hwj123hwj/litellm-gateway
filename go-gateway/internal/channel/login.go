package channel

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// DeviceCode 是一次设备码授权的全部展示信息（minimax 与 cline 共用同一形状）。
type DeviceCode struct {
	DeviceCode              string
	UserCode                string
	VerificationURI         string
	VerificationURIComplete string
	ExpiresIn               time.Duration
	Interval                time.Duration
}

// LoginURL 返回用户应打开的地址：优先带 user_code 的完整地址。
func (d DeviceCode) LoginURL() string {
	if d.VerificationURIComplete != "" {
		return d.VerificationURIComplete
	}
	return d.VerificationURI
}

// pollOutcome 是一次轮询的结果分类，由各渠道的 pollOnce 回调返回。
type pollOutcome int

const (
	pollPending  pollOutcome = iota // 用户还没完成授权，继续等
	pollSlowDown                    // 轮询过快，拉长间隔再试
	pollDenied                      // 用户拒绝
	pollExpired                     // 设备码过期
	pollSuccess                     // 授权完成，凭据已就绪
)

// ErrLoginCancelled 表示用户拒绝或设备码过期，需要重新发起登录。
var ErrLoginCancelled = errors.New("login cancelled or device code expired")

// slowDownIncrement 是 RFC 8628 要求的 slow_down 退避增量（每次至少 +5s）。
// 作为变量是为了让测试不必真的睡 5 秒。
var slowDownIncrement = 5 * time.Second

// pollDeviceToken 以设备码轮询令牌，直到成功、被拒或超时。
//
// pollOnce 每次调用发一次令牌请求，返回结果分类与（成功时的）凭据。把 HTTP 细节
// 留在各渠道里，这里只负责节奏控制：pending 用当前间隔，slow_down 递增间隔
// （RFC 8628 要求每次至少 +5s）。
func pollDeviceToken(ctx context.Context, dc DeviceCode, pollOnce func(context.Context) (pollOutcome, *Credential, error)) (*Credential, error) {
	interval := dc.Interval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	deadline := time.Now().Add(dc.ExpiresIn)

	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("%w: 设备码已过期，请重新登录", ErrLoginCancelled)
		}

		outcome, cred, err := pollOnce(ctx)
		if err != nil {
			return nil, err
		}
		switch outcome {
		case pollPending:
			if err := sleepCtx(ctx, interval); err != nil {
				return nil, err
			}
		case pollSlowDown:
			interval += slowDownIncrement
			if err := sleepCtx(ctx, interval); err != nil {
				return nil, err
			}
		case pollDenied:
			return nil, fmt.Errorf("%w: 用户拒绝了授权", ErrLoginCancelled)
		case pollExpired:
			return nil, fmt.Errorf("%w: 设备码已过期，请重新登录", ErrLoginCancelled)
		case pollSuccess:
			if cred == nil {
				return nil, errors.New("登录返回了空凭据")
			}
			return cred, nil
		default:
			return nil, fmt.Errorf("登录轮询返回了未知结果 %d", outcome)
		}
	}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// deviceLogin 是各渠道设备码登录会话的通用实现：BeginLogin 拿到展示信息后，
// 调用方轮询 Wait 直到用户完成授权。渠道只需提供「开始」与「完成」两个函数。
type deviceLogin struct {
	channel string
	dc      DeviceCode
	wait    func(ctx context.Context) (*Credential, error)
}

func (l *deviceLogin) Channel() string                               { return l.channel }
func (l *deviceLogin) DeviceCode() DeviceCode                        { return l.dc }
func (l *deviceLogin) Wait(ctx context.Context) (*Credential, error) { return l.wait(ctx) }
