package provision

import (
	"bufio"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"example.com/tuya-beacon-mesh-p2-controller/internal/config"
)

const (
	sharingClientID  = "HA_3y9q4ak7g4ephrvke" // published HA Tuya integration registration, NOT an account secret
	sharingSchema    = "haauthorize"
	sharingLoginHost = "https://apigw.iotbing.com"
)

// sharingToken is ephemeral; no refresh/session token is written to disk.
type sharingToken struct {
	Endpoint     string `json:"endpoint"`
	TerminalID   string `json:"terminal_id"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

type sharingResponse struct {
	Success bool            `json:"success"`
	Code    json.RawMessage `json:"code"`
	Msg     string          `json:"msg"`
	T       int64           `json:"t"`
	Result  json.RawMessage `json:"result"`
}

func sharingHTTP(ctx context.Context, client *http.Client, method, uri string, headers http.Header) (sharingResponse, error) {
	var out sharingResponse
	req, err := http.NewRequestWithContext(ctx, method, uri, nil)
	if err != nil {
		return out, err
	}
	if headers != nil {
		req.Header = headers
	}
	resp, err := client.Do(req)
	if err != nil {
		return out, fmt.Errorf("Tuya sharing HTTPS: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, fmt.Errorf("unexpected sharing API response (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return out, fmt.Errorf("Tuya sharing API HTTP %d", resp.StatusCode)
	}
	return out, nil
}

func sharingError(resp sharingResponse) error {
	// Tuya errors are safe to show after limiting and avoiding raw HTTP responses.
	message := resp.Msg
	if len(message) > 160 {
		message = message[:160]
	}
	return fmt.Errorf("Tuya sharing API refused request (code %s): %s", string(resp.Code), message)
}

func qrTokenRequest(ctx context.Context, client *http.Client, origin, userCode string) (string, error) {
	params := url.Values{"clientid": {sharingClientID}, "schema": {sharingSchema}, "usercode": {userCode}}
	reply, err := sharingHTTP(ctx, client, http.MethodPost, origin+"/v1.0/m/life/home-assistant/qrcode/tokens?"+params.Encode(), nil)
	if err != nil {
		return "", err
	}
	if !reply.Success {
		return "", sharingError(reply)
	}
	var result struct {
		QRCode string `json:"qrcode"`
	}
	if err := json.Unmarshal(reply.Result, &result); err != nil || result.QRCode == "" {
		return "", errors.New("Tuya QR token missing")
	}
	return result.QRCode, nil
}

func qrPollOnce(ctx context.Context, client *http.Client, origin, token, userCode string) (sharingToken, bool, error) {
	params := url.Values{"clientid": {sharingClientID}, "usercode": {userCode}}
	reply, err := sharingHTTP(ctx, client, http.MethodGet, origin+"/v1.0/m/life/home-assistant/qrcode/tokens/"+url.PathEscape(token)+"?"+params.Encode(), nil)
	if err != nil {
		return sharingToken{}, false, err
	}
	if !reply.Success {
		return sharingToken{}, false, nil
	} // pending until user explicitly authorizes
	var result sharingToken
	if err := json.Unmarshal(reply.Result, &result); err != nil {
		return result, false, err
	}
	if result.Endpoint == "" || result.RefreshToken == "" || result.AccessToken == "" {
		return result, false, errors.New("Tuya confirmed QR but returned incomplete session data")
	}
	return result, true, nil
}

func validateSharingEndpoint(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Port() != "" || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", errors.New("unexpected Tuya QR sharing endpoint")
	}
	host := strings.ToLower(u.Hostname())
	accepted := []string{"tuyaeu.com", "tuyaus.com", "tuyacn.com", "tuyain.com", "tuya.com", "iotbing.com", "tuyawe.com", "tuyaap.com"}
	for _, suffix := range accepted {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return "https://" + host, nil
		}
	}
	return "", errors.New("Tuya returned an unrecognized API host; refusing to send session credentials")
}

// The Tuya Device Sharing SDK derives an ASCII 16-byte AES-GCM key from a
// per-request UUID and the session refresh_token. Note: hex characters are
// used as bytes for AES, not decoded into binary.
func sharingKey(rid, refreshToken string) (hashKey, aesKey string) {
	md5Sum := md5.Sum([]byte(rid + refreshToken))
	hashKey = hex.EncodeToString(md5Sum[:])
	mac := hmac.New(sha256.New, []byte(rid))
	_, _ = mac.Write([]byte(hashKey))
	aesKey = hex.EncodeToString(mac.Sum(nil))[:16]
	return hashKey, aesKey
}

func sharingSign(hashKey, appKey, requestID, unixMS, token, encQuery, encBody string) string {
	fields := []string{"X-appKey=" + appKey, "X-requestId=" + requestID, "X-time=" + unixMS}
	if token != "" {
		fields = append(fields, "X-token="+token)
	}
	message := strings.Join(fields, "||") + encQuery + encBody
	mac := hmac.New(sha256.New, []byte(hashKey))
	_, _ = mac.Write([]byte(message))
	return hex.EncodeToString(mac.Sum(nil))
}

func sharingAESSeal(aesKey string, plain []byte) (string, error) {
	block, err := aes.NewCipher([]byte(aesKey))
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	// The official Python SDK uses 12 printable ASCII characters for the
	// nonce, not 12 arbitrary bytes. The nonce is not an encryption secret.
	const alphabet = "ABCDEFGHJKMNPQRSTWXYZabcdefhijkmnprstwxyz2345678"
	nonce := make([]byte, 12)
	random := make([]byte, len(nonce))
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	for i, v := range random {
		nonce[i] = alphabet[int(v)%len(alphabet)]
	}
	ciphertext := gcm.Seal(nil, nonce, plain, nil)
	// Tuya signs these two independently Base64-encoded parts concatenated.
	return base64.StdEncoding.EncodeToString(nonce) + base64.StdEncoding.EncodeToString(ciphertext), nil
}

func sharingAESOpen(aesKey, encoded string) ([]byte, error) {
	packed, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}
	if len(packed) < 12+16 {
		return nil, errors.New("encrypted sharing response too short")
	}
	block, err := aes.NewCipher([]byte(aesKey))
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return gcm.Open(nil, packed[:12], packed[12:], nil)
}

func newSharingRequestID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

type sharingAPI struct {
	endpoint string
	client   *http.Client
	token    sharingToken
}

// GET paths and headers reproduce CustomerApi in the official Sharing SDK.
func (s *sharingAPI) get(ctx context.Context, path string, params map[string]string) (json.RawMessage, error) {
	rid, err := newSharingRequestID()
	if err != nil {
		return nil, err
	}
	hashKey, aesKey := sharingKey(rid, s.token.RefreshToken)
	encQuery := ""
	endpoint := s.endpoint + path
	if len(params) > 0 {
		body, err := json.Marshal(params)
		if err != nil {
			return nil, err
		}
		encQuery, err = sharingAESSeal(aesKey, body)
		if err != nil {
			return nil, err
		}
		endpoint += "?" + url.Values{"encdata": {encQuery}}.Encode()
	}
	unixMS := fmt.Sprint(time.Now().UnixMilli())
	header := make(http.Header)
	header.Set("X-appKey", sharingClientID)
	header.Set("X-requestId", rid)
	header.Set("X-sid", "")
	header.Set("X-time", unixMS)
	header.Set("X-token", s.token.AccessToken)
	header.Set("X-sign", sharingSign(hashKey, sharingClientID, rid, unixMS, s.token.AccessToken, encQuery, ""))
	reply, err := sharingHTTP(ctx, s.client, http.MethodGet, endpoint, header)
	if err != nil {
		return nil, err
	}
	if !reply.Success {
		return nil, sharingError(reply)
	}
	if len(reply.Result) == 0 || string(reply.Result) == "null" {
		return nil, nil
	}
	var cipherText string
	if err := json.Unmarshal(reply.Result, &cipherText); err != nil {
		return nil, errors.New("sharing result not an encrypted Base64 string")
	}
	decrypted, err := sharingAESOpen(aesKey, cipherText)
	if err != nil {
		return nil, fmt.Errorf("sharing AES-GCM decryption failed: %w", err)
	}
	if !json.Valid(decrypted) {
		return nil, errors.New("sharing reply decrypted but is not JSON")
	}
	return decrypted, nil
}

func findLocalKeyInSharingResult(payload json.RawMessage, deviceID string) (string, bool, error) {
	if len(payload) == 0 || string(payload) == "null" {
		return "", false, nil
	}
	var devices []struct {
		ID       string `json:"id"`
		LocalKey string `json:"local_key"`
	}
	if err := json.Unmarshal(payload, &devices); err != nil {
		return "", false, fmt.Errorf("sharing devices format: %w", err)
	}
	for _, d := range devices {
		if d.ID != deviceID {
			continue
		}
		if _, err := config.ParseKey(d.LocalKey); err != nil {
			return "", true, errors.New("Smart Life returned this device without a usable local_key; try Developer API or re-pairing")
		}
		return d.LocalKey, true, nil
	}
	return "", false, nil
}

func querySharingLocalKey(ctx context.Context, api *sharingAPI, deviceID string) (string, error) {
	// Fast path: match exact ID, never select just by product name.
	direct, directErr := api.get(ctx, "/v1.0/m/life/ha/devices/detail", map[string]string{"devIds": deviceID})
	if directErr == nil {
		if key, match, err := findLocalKeyInSharingResult(direct, deviceID); match || err != nil {
			return key, err
		}
	}
	// Same discovery path as tuya-beacon-linux's Manager.update_device_cache.
	homesJSON, err := api.get(ctx, "/v1.0/m/life/users/homes", nil)
	if err != nil {
		if directErr != nil {
			return "", fmt.Errorf("device lookup: %v; home lookup: %w", directErr, err)
		}
		return "", err
	}
	var homes []struct {
		OwnerID json.Number `json:"ownerId"`
	}
	if err := json.Unmarshal(homesJSON, &homes); err != nil {
		return "", fmt.Errorf("parse home list: %w", err)
	}
	for _, home := range homes {
		if home.OwnerID.String() == "" {
			continue
		}
		list, err := api.get(ctx, "/v1.0/m/life/ha/home/devices", map[string]string{"homeId": home.OwnerID.String()})
		if err != nil {
			return "", err
		}
		if key, match, err := findLocalKeyInSharingResult(list, deviceID); match || err != nil {
			return key, err
		}
	}
	return "", fmt.Errorf("device %s not found in authorized Smart Life account", deviceID)
}

func printLoginQR(qrValue string) error {
	exe, err := exec.LookPath("qrencode")
	if err != nil {
		return errors.New("QR renderer not installed; on Arch Linux run: sudo pacman -S qrencode")
	}
	cmd := exec.Command(exe, "-t", "ANSIUTF8", "-m", "1")
	cmd.Stdin = strings.NewReader(qrValue)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("qrencode: %w", err)
	}
	return nil
}

func readConfiguredDeviceID(path, override string) (string, error) {
	if override != "" {
		return override, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read device ID from %s: %w (or pass -cloud-device-id)", path, err)
	}
	var cfg config.Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return "", err
	}
	if cfg.Device.DeviceID == "" {
		return "", errors.New("config lacks device.device_id; set -cloud-device-id")
	}
	return cfg.Device.DeviceID, nil
}

func RunQRLocalKey(ctx context.Context, configPath, deviceID, userCode, app string, timeout time.Duration, save bool) error {
	deviceID, err := readConfiguredDeviceID(configPath, deviceID)
	if err != nil {
		return err
	}
	switch app {
	case "smartlife", "tuyaSmart":
	default:
		return errors.New("-qr-app must be smartlife|tuyaSmart")
	}
	// Obtain user code without requesting a password or developer credentials.
	if strings.TrimSpace(userCode) == "" {
		fmt.Fprint(os.Stderr, "Smart Life → Me → Settings → Account and Security → User Code: ")
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		userCode = strings.TrimSpace(line)
	}
	userCode = strings.TrimSpace(userCode)
	if len(userCode) < 2 || len(userCode) > 200 || strings.ContainsAny(userCode, "\r\n") {
		return errors.New("invalid Smart Life user code")
	}
	if _, err := exec.LookPath("qrencode"); err != nil {
		return errors.New("install the QR renderer first: sudo pacman -S qrencode")
	}
	client := &http.Client{Timeout: 15 * time.Second}
	token, err := qrTokenRequest(ctx, client, sharingLoginHost, userCode)
	if err != nil {
		return err
	}
	qrValue := app + "--qrLogin?token=" + token
	fmt.Fprintln(os.Stderr, "Scan this QR with the Smart Life / Tuya app and confirm the Home Assistant authorization:")
	if err := printLoginQR(qrValue); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	fmt.Fprintln(os.Stderr, "Reading only the key of the selected lamp. Login session will not be stored.")
	var session sharingToken
	for {
		var ok bool
		session, ok, err = qrPollOnce(ctx, client, sharingLoginHost, token, userCode)
		if err != nil {
			return err
		}
		if ok {
			break
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("QR login timed out or cancelled: %w", ctx.Err())
		case <-time.After(3 * time.Second):
		}
	}
	endpoint, err := validateSharingEndpoint(session.Endpoint)
	if err != nil {
		return err
	}
	api := &sharingAPI{endpoint: endpoint, client: client, token: session}
	key, err := querySharingLocalKey(ctx, api, deviceID)
	if err != nil {
		return err
	}
	if !save {
		fmt.Printf("Successfully obtained a valid local_key for %s (%d characters). Use -qr-save=true to store it in your config.\n", deviceID, len(key))
		return nil
	}
	// Do not follow symlinks for the config; update routine backs it up on change.
	target, err := filepath.Abs(configPath)
	if err != nil {
		return err
	}
	changed, err := config.UpdateLocalKeyConfig(target, deviceID, key)
	if err != nil {
		return err
	}
	if changed {
		fmt.Printf("Updated local_key for %s in %s (0600, private backup created if previous file existed). If K1 changed, relearn beacon_key using: tuya-beacon-mesh-p2-controller learn live\n", deviceID, target)
	} else {
		fmt.Printf("local_key for %s is unchanged. Config: %s\n", deviceID, target)
	}
	return nil
}
