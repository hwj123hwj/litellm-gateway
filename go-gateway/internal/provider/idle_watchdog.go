package provider

import (
	"context"
	"io"
	"sync/atomic"
	"time"
)

// kickingReader 包装 io.Reader，在每次成功 Read 后调用 Kick，
// 供纯透传路径（io.Copy）喂看门狗。
type kickingReader struct {
	R    io.Reader
	Kick func()
}

func (kr kickingReader) Read(p []byte) (int, error) {
	n, err := kr.R.Read(p)
	if n > 0 {
		kr.Kick()
	}
	return n, err
}

// startStreamIdleWatchdog 启动流式 body 的空闲看门狗：每收到一行数据调用一次
// kick；超过 idle 未 kick 则 cancel（中止停滞的上游连接）。
// 返回的 tripped 在看门狗触发后返回 true，调用方可据此给出明确的超时错误。
// http.Client.Timeout 会限制整个流的总时长（长生成被砍断），故用空闲超时替代。
func startStreamIdleWatchdog(ctx context.Context, idle time.Duration, cancel context.CancelFunc) (kick func(), tripped func() bool) {
	kickCh := make(chan struct{}, 1)
	var flag atomic.Bool

	go func() {
		timer := time.NewTimer(idle)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-kickCh:
				// 排掉可能已到期的 timer，避免刚 kick 就误触发
				select {
				case <-timer.C:
				default:
				}
				timer.Reset(idle)
			case <-timer.C:
				flag.Store(true)
				cancel()
				return
			}
		}
	}()

	return func() {
		select {
		case kickCh <- struct{}{}:
		default:
		}
	}, flag.Load
}
