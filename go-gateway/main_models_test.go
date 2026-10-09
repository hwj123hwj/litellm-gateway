package main

import (
	"io"
	"log"
	"testing"

	"github.com/weijian/go-llm-gateway/internal/config"
	"github.com/weijian/go-llm-gateway/internal/provider"
)

func TestDeepVNewModelRoutes(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	router := provider.NewRouter(logger)
	setupDeepVProviders(router, &config.Config{DeepVWorkDir: t.TempDir()}, logger)
	for _, id := range []string{"claude-haiku-5-5", "mimo-v2.6-pro"} {
		routes, err := router.Route(id)
		if err != nil || len(routes) != 1 {
			t.Fatalf("%s routes = %v, error = %v", id, routes, err)
		}
		bound, ok := routes[0].(interface{ BoundModel() string })
		if !ok || bound.BoundModel() != id {
			t.Fatalf("%s not bound to its upstream model", id)
		}
		found := false
		for _, info := range router.ListModelInfos() {
			if info.ID == id {
				found = true
				if info.Provider != id || info.MaxInputTokens+info.MaxOutputTokens > 200000 {
					t.Fatalf("%s invalid directory metadata: %+v", id, info)
				}
			}
		}
		if !found {
			t.Fatalf("%s missing from model directory", id)
		}
	}
}
