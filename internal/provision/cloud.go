package provision

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"example.com/tuya-beacon-mesh-p2-controller/internal/config"
)

// Tuya OpenAPI calls are ONLY used during explicit provisioning (cloud local-key).
// Normal P2 commands and live learning never need an Internet connection.
func tuyaRegionBaseURL(region string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(region)) {
	case "eu", "central", "central-europe":
		return "https://openapi.tuyaeu.com", nil
	case "we", "western", "western-europe":
		return "https://openapi-weaz.tuyaeu.com", nil
	default:
		return "", fmt.Errorf("unsupported region %q; choose eu (Central Europe) or we (Western Europe)", region)
	}
}

func tuyaSignature(clientID, secret, token, millis, path string) string {
	// No nonce and no custom Signature-Headers. The SHA256 hash is for an empty GET body.
	emptySum := sha256.Sum256(nil)
	stringToSign := "GET\n" + hex.EncodeToString(emptySum[:]) + "\n\n" + path
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(clientID + token + millis + stringToSign))
	return strings.ToUpper(hex.EncodeToString(mac.Sum(nil)))
}

type tuyaReply struct {
	Success bool            `json:"success"`
	Code    int             `json:"code"`
	Msg     string          `json:"msg"`
	Result  json.RawMessage `json:"result"`
}

func tuyaGET(ctx context.Context, client *http.Client, baseURL, path, clientID, secret, token string) (json.RawMessage, error) {
	millis := fmt.Sprint(time.Now().UnixMilli())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("client_id", clientID)
	req.Header.Set("t", millis)
	req.Header.Set("sign_method", "HMAC-SHA256")
	req.Header.Set("sign", tuyaSignature(clientID, secret, token, millis, path))
	if token != "" {
		req.Header.Set("access_token", token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Tuya API HTTPS: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return nil, err
	}
	var reply tuyaReply
	if err := json.Unmarshal(body, &reply); err != nil {
		return nil, fmt.Errorf("Tuya API HTTP %d: invalid JSON", resp.StatusCode)
	}
	if !reply.Success {
		if reply.Code == 28841002 {
			return nil, errors.New("Tuya IoT Core subscription expired (28841002); extend the subscription in Tuya Developer Platform")
		}
		return nil, fmt.Errorf("Tuya API error %d: %s (HTTP %d)", reply.Code, reply.Msg, resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Tuya API unexpected HTTP %d", resp.StatusCode)
	}
	return reply.Result, nil
}

func queryTuyaLocalKey(ctx context.Context, client *http.Client, baseURL, clientID, secret, deviceID string) (string, error) {
	if clientID == "" || secret == "" {
		return "", errors.New("set TUYA_CLIENT_ID and TUYA_CLIENT_SECRET (project Access ID / Access Secret)")
	}
	if deviceID == "" {
		return "", errors.New("empty device_id")
	}
	// Device ID comes from the config or a manually specified -cloud-device-id.
	for _, r := range deviceID {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return "", errors.New("device_id has invalid characters")
		}
	}
	result, err := tuyaGET(ctx, client, baseURL, "/v1.0/token?grant_type=1", clientID, secret, "")
	if err != nil {
		return "", fmt.Errorf("obtain temporary Tuya token: %w", err)
	}
	var auth struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(result, &auth); err != nil || auth.AccessToken == "" {
		return "", errors.New("Tuya returned no access_token")
	}
	result, err = tuyaGET(ctx, client, baseURL, "/v2.0/cloud/thing/"+deviceID, clientID, secret, auth.AccessToken)
	if err != nil {
		return "", fmt.Errorf("fetch device details: %w", err)
	}
	var thing struct {
		ID       string `json:"id"`
		LocalKey string `json:"local_key"`
	}
	if err := json.Unmarshal(result, &thing); err != nil {
		return "", err
	}
	if thing.ID != "" && thing.ID != deviceID {
		return "", errors.New("Tuya returned a different device ID")
	}
	if _, err := config.ParseKey(thing.LocalKey); err != nil {
		return "", fmt.Errorf("Tuya response local_key invalid: %w", err)
	}
	return thing.LocalKey, nil
}

func RunCloudLocalKey(ctx context.Context, configPath, id, region string, save bool) error {
	if id == "" {
		data, err := os.ReadFile(configPath)
		if err != nil {
			return fmt.Errorf("read device ID from config: %w (or set -cloud-device-id)", err)
		}
		var cfg config.Config
		if err = json.Unmarshal(data, &cfg); err != nil {
			return err
		}
		id = cfg.Device.DeviceID
	}
	base, err := tuyaRegionBaseURL(region)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 15 * time.Second}
	key, err := queryTuyaLocalKey(ctx, client, base, os.Getenv("TUYA_CLIENT_ID"), os.Getenv("TUYA_CLIENT_SECRET"), id)
	if err != nil {
		return err
	}
	if !save {
		fmt.Printf("Tuya returned a valid local_key for %s (%d characters). To store it privately, re-run with -cloud-save.\n", id, len(key))
		return nil
	}
	changed, err := config.UpdateLocalKeyConfig(configPath, id, key)
	if err != nil {
		return err
	}
	if changed {
		fmt.Printf("Updated local_key for %s in %s (0600). Existing file backed up. If key changed, old beacon_key was cleared: run learn live.\n", id, configPath)
	} else {
		fmt.Printf("local_key for %s is unchanged in %s.\n", id, configPath)
	}
	return nil
}
