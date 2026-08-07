package handlers

import (
	"errors"
	"net/http"

	"github.com/weijian/go-llm-gateway/internal/provider"
)

func routingErrorStatus(err error) int {
	if err == nil {
		return http.StatusServiceUnavailable
	}
	var unsupported *provider.UnsupportedCapabilityError
	if errors.As(err, &unsupported) {
		return http.StatusBadRequest
	}
	return http.StatusServiceUnavailable
}
