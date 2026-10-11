package channel

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Read installation metadata only; never read or import the IDE's account tokens.
type traeLocalClient struct {
	ClientID, AppID, AuthFrom, Version, PluginVersion, Quality string
}

func discoverTraeLocalClient() *traeLocalClient {
	if path := os.Getenv("TRAE_CHANNEL_CLIENT_PRODUCT"); path != "" {
		return readTraeLocalClient(path)
	}
	if runtime.GOOS != "darwin" {
		return nil
	}
	home, _ := os.UserHomeDir()
	for _, root := range []string{"/Applications", filepath.Join(home, "Applications")} {
		for _, name := range []string{"Trae CN.app", "TRAE SOLO CN.app"} {
			if config := readTraeLocalClient(filepath.Join(root, name, "Contents", "Resources", "app", "product.json")); config != nil {
				return config
			}
		}
	}
	return nil
}

func readTraeLocalClient(path string) *traeLocalClient {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()
	var product struct {
		ProviderCode     string `json:"providerCode"`
		PackageType      string `json:"packageType"`
		Quality          string `json:"quality"`
		AppVersion       string `json:"appVersion"`
		TronBuildVersion string `json:"tronBuildVersion"`
		ICubeApp         struct {
			AuthConfig map[string]map[string]string `json:"authConfig"`
		} `json:"iCubeApp"`
		BootConfig struct {
			Agent struct {
				AppID string `json:"appId"`
			} `json:"agent"`
		} `json:"bootConfig"`
	}
	if json.NewDecoder(io.LimitReader(file, 4<<20)).Decode(&product) != nil || product.ProviderCode != "cn" {
		return nil
	}
	productName, authFrom := "TRAE", "trae"
	switch product.PackageType {
	case "TRAE_CN":
	case "SOLO_CN":
		productName, authFrom = "SOLO", "solo"
	default:
		return nil
	}
	quality := product.Quality
	if quality == "" {
		quality = "stable"
	}
	id, app := product.ICubeApp.AuthConfig[productName][quality], product.BootConfig.Agent.AppID
	if strings.TrimSpace(id) == "" || strings.TrimSpace(app) == "" || strings.ContainsAny(id+app, "\r\n") {
		return nil
	}
	return &traeLocalClient{ClientID: id, AppID: app, AuthFrom: authFrom, Version: product.AppVersion, PluginVersion: product.TronBuildVersion, Quality: quality}
}
