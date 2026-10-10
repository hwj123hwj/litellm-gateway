package channel

import (
	"context"
	"errors"
	"testing"
	"time"
)

// withFastPoll 把退避增量调小，让 slow_down 分支不必真睡 5 秒。
func withFastPoll(t *testing.T) {
	t.Helper()
	original := slowDownIncrement
	slowDownIncrement = time.Millisecond
	t.Cleanup(func() { slowDownIncrement = original })
}

func TestPollDeviceTokenSuccessAfterPending(t *testing.T) {
	withFastPoll(t)
	dc := DeviceCode{DeviceCode: "dev", Interval: time.Millisecond, ExpiresIn: 2 * time.Second}

	calls := 0
	cred, err := pollDeviceToken(context.Background(), dc, func(context.Context) (pollOutcome, *Credential, error) {
		calls++
		if calls < 3 {
			return pollPending, nil, nil
		}
		return pollSuccess, &Credential{AccessToken: "token-value"}, nil
	})
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if cred == nil || cred.AccessToken != "token-value" {
		t.Fatalf("unexpected credential: %+v", cred)
	}
	if calls != 3 {
		t.Fatalf("expected 3 polls, got %d", calls)
	}
}

func TestPollDeviceTokenSlowDownEnlargesInterval(t *testing.T) {
	withFastPoll(t)
	dc := DeviceCode{Interval: time.Millisecond, ExpiresIn: 2 * time.Second}

	var observed []time.Duration
	// 用返回值把「两次调用之间的间隔」间接测出来：这里改为直接验证退避后的
	// interval 生效——第一次 slow_down 后必须等待更久，因此记录时间差。
	start := time.Now()
	calls := 0
	_, err := pollDeviceToken(context.Background(), dc, func(context.Context) (pollOutcome, *Credential, error) {
		calls++
		observed = append(observed, time.Since(start))
		switch calls {
		case 1:
			// 第一次就要求 slow_down，退避增量会被加到 interval 上。
			slowDownIncrement = 40 * time.Millisecond
			return pollSlowDown, nil, nil
		default:
			return pollSuccess, &Credential{AccessToken: "ok"}, nil
		}
	})
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if calls != 2 {
		t.Fatalf("expected 2 polls, got %d", calls)
	}
	// 第二次轮询前应已等待「原 interval(1ms) + 增量(40ms)」；给出宽松下限避免抖动。
	if observed[1] < 40*time.Millisecond {
		t.Fatalf("slow_down 未拉长间隔：第二次轮询仅间隔 %v", observed[1])
	}
}

func TestPollDeviceTokenDenied(t *testing.T) {
	withFastPoll(t)
	dc := DeviceCode{Interval: time.Millisecond, ExpiresIn: time.Second}

	_, err := pollDeviceToken(context.Background(), dc, func(context.Context) (pollOutcome, *Credential, error) {
		return pollDenied, nil, nil
	})
	if !errors.Is(err, ErrLoginCancelled) {
		t.Fatalf("expected ErrLoginCancelled, got %v", err)
	}
}

func TestPollDeviceTokenExpired(t *testing.T) {
	withFastPoll(t)
	dc := DeviceCode{Interval: time.Millisecond, ExpiresIn: time.Second}

	_, err := pollDeviceToken(context.Background(), dc, func(context.Context) (pollOutcome, *Credential, error) {
		return pollExpired, nil, nil
	})
	if !errors.Is(err, ErrLoginCancelled) {
		t.Fatalf("expected ErrLoginCancelled, got %v", err)
	}
}

func TestPollDeviceTokenDeadline(t *testing.T) {
	withFastPoll(t)
	// ExpiresIn 为 0 会让 deadline 立刻过期，请求还没发出就应以「设备码已过期」结束。
	dc := DeviceCode{Interval: time.Millisecond, ExpiresIn: 0}

	polled := false
	_, err := pollDeviceToken(context.Background(), dc, func(context.Context) (pollOutcome, *Credential, error) {
		polled = true
		return pollPending, nil, nil
	})
	if !errors.Is(err, ErrLoginCancelled) {
		t.Fatalf("expected ErrLoginCancelled on deadline, got %v", err)
	}
	if polled {
		t.Fatalf("deadline 已过时不应再发起轮询")
	}
}

func TestPollDeviceTokenContextCancelled(t *testing.T) {
	withFastPoll(t)
	dc := DeviceCode{Interval: time.Millisecond, ExpiresIn: time.Minute}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := pollDeviceToken(ctx, dc, func(context.Context) (pollOutcome, *Credential, error) {
		return pollPending, nil, nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestPollDeviceTokenErrorPropagates(t *testing.T) {
	withFastPoll(t)
	dc := DeviceCode{Interval: time.Millisecond, ExpiresIn: time.Minute}

	want := errors.New("网络炸了")
	_, err := pollDeviceToken(context.Background(), dc, func(context.Context) (pollOutcome, *Credential, error) {
		return pollPending, nil, want
	})
	if !errors.Is(err, want) {
		t.Fatalf("expected wrapped poll error, got %v", err)
	}
}

func TestPollDeviceTokenNilCredentialIsError(t *testing.T) {
	withFastPoll(t)
	dc := DeviceCode{Interval: time.Millisecond, ExpiresIn: time.Minute}

	// 成功结果但凭据为空，属于实现错误，必须报错而不是返回空凭据。
	_, err := pollDeviceToken(context.Background(), dc, func(context.Context) (pollOutcome, *Credential, error) {
		return pollSuccess, nil, nil
	})
	if err == nil {
		t.Fatalf("expected error for nil credential, got nil")
	}
}

func TestDeviceCodeLoginURL(t *testing.T) {
	withComplete := DeviceCode{VerificationURI: "https://x/device", VerificationURIComplete: "https://x/device?code=ABCD"}
	if got := withComplete.LoginURL(); got != "https://x/device?code=ABCD" {
		t.Fatalf("应优先返回 complete 地址，得到 %s", got)
	}
	withoutComplete := DeviceCode{VerificationURI: "https://x/device"}
	if got := withoutComplete.LoginURL(); got != "https://x/device" {
		t.Fatalf("无 complete 地址时应回退到 verification_uri，得到 %s", got)
	}
}
