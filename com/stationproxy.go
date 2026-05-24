package com

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

const (
	frpVersion    = "0.63.0"
	registerURL   = "https://register.onlysatellites.com/"
	frpcConfig    = "frpc.toml"
	frpcBinFolder = "frp_bin"
)

type registerRequest struct {
	Name      string `json:"name"`
	LocalPort int    `json:"localPort"`
	Secret    string `json:"secret,omitempty"`
}

type registerResponse struct {
	StationId     string `json:"stationId"`
	StationSecret string `json:"stationSecret"`
	FrpcToml      string `json:"frpcToml"`
	Frps          struct {
		Addr string `json:"addr"`
		Port int    `json:"port"`
	} `json:"frps"`
	Subdomain string `json:"subdomain"`
}

// RunStationProxy handles registration and FRPC startup
/** func RunStationProxy() error {
	 if !cfg.StationProxy.Enabled {
		fmt.Println("Station proxy disabled in config.")
		return nil
	}

	localPort := cfg.Server.Port
	if localPort[0] == ':' {
		localPort = localPort[1:]
	}

	payload := registerRequest{
		Name:      cfg.StationProxy.StationId,
		LocalPort: atoi(localPort),
	}
	if cfg.StationProxy.StationSecret != "" {
		payload.Secret = cfg.StationProxy.StationSecret
	}

	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", registerURL, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("tunnel request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		data, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("registration error %d: %s", resp.StatusCode, data)
	}

	var reg registerResponse
	if err := json.NewDecoder(resp.Body).Decode(&reg); err != nil {
		return fmt.Errorf("decode failed: %w", err)
	}

	// Save updated station config back into config.toml
	cfg.StationProxy.StationId = reg.StationId
	if reg.StationSecret != "" {
		cfg.StationProxy.StationSecret = reg.StationSecret
	}
	cfg.StationProxy.FrpsAddr = reg.Frps.Addr
	cfg.StationProxy.FrpsPort = reg.Frps.Port

	// Write frpc.toml from response
	if err := os.WriteFile(frpcConfig, []byte(reg.FrpcToml), 0644); err != nil {
		return fmt.Errorf("failed to write frpc.toml: %w", err)
	}

	// Does binary exist
	binPath, err := ensureFrpcBinary()
	if err != nil {
		return err
	}

	fmt.Printf("Starting FRPC from %s\n", binPath)
	cmd := exec.Command(binPath, "-c", frpcConfig)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start frpc: %w", err)
	}

	go cmd.Wait() // don’t block
	return nil
} */

func ensureFrpcBinary() (string, error) {
	osName := runtime.GOOS
	arch := runtime.GOARCH

	ext := "tar.gz"
	if osName == "windows" {
		ext = "zip"
	}

	base := fmt.Sprintf("frp_%s_%s_%s", frpVersion, osName, arch)
	url := fmt.Sprintf("https://github.com/fatedier/frp/releases/download/v%s/%s.%s", frpVersion, base, ext)
	binName := "frpc"
	if osName == "windows" {
		binName = "frpc.exe"
	}

	binPath := filepath.Join(frpcBinFolder, binName)
	if _, err := os.Stat(binPath); err == nil {
		return binPath, nil
	}

	if err := os.MkdirAll(frpcBinFolder, 0755); err != nil {
		return "", err
	}

	// provide URL to binary if missing
	return binPath, fmt.Errorf("frpc binary missing, expected at %s. Please download from %s", binPath, url)
}

func atoi(s string) int {
	var i int
	fmt.Sscanf(s, "%d", &i)
	return i
}
