package provider

import (
	"context"
	"sync"
	"testing"
)

// 管理端轮询可与启停、探测、重置和渠道刷新同时发生。
func TestRuntimeStatusConcurrentControls(t *testing.T) {
	r := NewRouter(nil)
	r.RegisterProvider("mock", NewOpenAIProvider(&Config{Name: "mock"}))
	r.RegisterChain("mock", []string{"mock"})
	var wg sync.WaitGroup
	for _, action := range []func(int){
		func(i int) { _ = r.SetProviderEnabled("mock", i%2 == 0) },
		func(int) { r.ProviderStatus("mock"); r.ListRouteStatuses() },
		func(int) { _ = r.ResetProvider("mock") },
		func(int) { r.CheckProviderHealth(context.Background(), "mock") },
		func(int) {
			r.RegisterProvider("mock", NewOpenAIProvider(&Config{Name: "mock"}))
			r.SetProviderRateLimitGroup("mock", "quota")
		},
		func(int) { r.RecordProviderSuccess("mock") },
	} {
		wg.Add(1)
		go func(action func(int)) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				action(i)
			}
		}(action)
	}
	wg.Wait()
}
