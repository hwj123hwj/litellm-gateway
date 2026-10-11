package handlers

import (
	"context"
	"errors"
	"io"

	"github.com/gin-gonic/gin"
	"github.com/weijian/go-llm-gateway/internal/provider"
)

func runGuardedStream(c *gin.Context, router *provider.Router, req *provider.Request, p provider.Provider, w io.Writer, forward func(context.Context, *provider.Request, io.Writer) error) error {
	return runGuardedStreamContext(c, c.Request.Context(), router, req, p, w, forward)
}

func runGuardedStreamContext(c *gin.Context, ctx context.Context, router *provider.Router, req *provider.Request, p provider.Provider, w io.Writer, forward func(context.Context, *provider.Request, io.Writer) error) error {
	timing, err := router.RunStreamAttempt(ctx, req, p, w, func(ctx context.Context, dst io.Writer) error { return forward(ctx, req, dst) })
	c.Set("attempt_stream_timing", timing)
	return err
}

func nativeFallbackError(c *gin.Context, err error) error {
	var unavailable *provider.NoAvailableProvidersError
	if err != nil && !errors.As(err, &unavailable) {
		return err
	}
	if value, ok := c.Get("native_passthrough_error"); ok {
		if previous, ok := value.(error); ok {
			return previous
		}
	}
	return err
}
