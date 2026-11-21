package provision

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestSharingCryptoMatchesUpstreamPythonVector(t *testing.T) {
	rid := "123e4567-e89b-42d3-a456-426614174000"
	hk, aeskey := sharingKey(rid, "refresh-token-for-test")
	if hk != "177955d1feafc3072d2469135177de37" || aeskey != "1603d89941845b2a" {
		t.Fatalf("SDK key derivation mismatch: %s %s", hk, aeskey)
	}
	signature := sharingSign(hk, sharingClientID, rid, "1700000000000", "test-access", "", "")
	if signature != "4ac1a4007e6520c6b9609e62357e4b4e24845abfbe2a1d23fe114cb6a2b1087a" {
		t.Fatalf("SDK signing mismatch: %s", signature)
	}
	cipher, err := sharingAESSeal(aeskey, []byte(`{"devIds":"lamp"}`))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := sharingAESOpen(aeskey, cipher)
	if err != nil || string(plain) != `{"devIds":"lamp"}` {
		t.Fatalf("AES-GCM roundtrip %q %v", plain, err)
	}
	if _, err := sharingAESOpen(aeskey, cipher[:len(cipher)-3]+"AAA"); err == nil {
		t.Fatal("tampered encrypted data accepted")
	}
}

func TestQRCodeLoginAPIPaths(t *testing.T) {
	var calls int
	client := clientFor(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("clientid") != sharingClientID || r.URL.Query().Get("usercode") != "my-user-code" {
			t.Error("missing user identity parameters")
		}
		if r.Method == http.MethodPost && r.URL.Path == "/v1.0/m/life/home-assistant/qrcode/tokens" {
			if r.URL.Query().Get("schema") != sharingSchema {
				t.Error("wrong schema")
			}
			fmt.Fprint(w, `{"success":true,"result":{"qrcode":"qr-token-1"}}`)
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == "/v1.0/m/life/home-assistant/qrcode/tokens/qr-token-1" {
			fmt.Fprint(w, `{"success":true,"result":{"terminal_id":"terminal","endpoint":"https://apigw.tuyaeu.com","access_token":"access","refresh_token":"refresh"}}`)
			return
		}
		t.Errorf("unexpected request %s %s", r.Method, r.URL)
		http.Error(w, "not found", 404)
	})
	token, err := qrTokenRequest(context.Background(), client, testOrigin, "my-user-code")
	if err != nil || token != "qr-token-1" {
		t.Fatalf("QR token %q %v", token, err)
	}
	session, ok, err := qrPollOnce(context.Background(), client, testOrigin, token, "my-user-code")
	if err != nil || !ok || session.Endpoint != "https://apigw.tuyaeu.com" || calls != 2 {
		t.Fatalf("login %v %v %d", session, err, calls)
	}
}

func TestSharingDeviceQueryWithoutDeveloperCredentials(t *testing.T) {
	// Fake Tuya Device Sharing API: verifies BOTH signed headers and encrypted
	// query, then sends a GCM-encrypted response as the actual SDK expects.
	const deviceID = "lamp123"
	var requests int
	client := clientFor(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("X-appKey") != sharingClientID || r.Header.Get("X-token") != "fake-access" {
			t.Errorf("wrong sharing auth headers")
		}
		rid := r.Header.Get("X-requestId")
		hk, key := sharingKey(rid, "fake-refresh")
		encQuery := r.URL.Query().Get("encdata")
		wantSig := sharingSign(hk, sharingClientID, rid, r.Header.Get("X-time"), "fake-access", encQuery, "")
		if r.Header.Get("X-sign") != wantSig {
			t.Errorf("invalid signature")
		}
		var plain json.RawMessage
		switch r.URL.Path {
		case "/v1.0/m/life/ha/devices/detail":
			decoded, err := sharingAESOpen(key, encQuery)
			if err != nil {
				t.Errorf("query decrypt: %v", err)
			}
			if !strings.Contains(string(decoded), deviceID) {
				t.Errorf("wrong queried id")
			}
			plain = json.RawMessage(`[{"id":"lamp123","local_key":"abcdefghijklmnop"},{"id":"other","local_key":"9999999999999999"}]`)
		default:
			t.Errorf("unexpected query path: %s", r.URL.Path)
			http.Error(w, "not found", 404)
			return
		}
		encrypted, err := sharingAESSeal(key, plain)
		if err != nil {
			t.Error(err)
		}
		result, _ := json.Marshal(encrypted)
		fmt.Fprintf(w, `{"success":true,"result":%s}`, result)
	})
	api := &sharingAPI{endpoint: testOrigin, client: client, token: sharingToken{AccessToken: "fake-access", RefreshToken: "fake-refresh"}}
	key, err := querySharingLocalKey(context.Background(), api, deviceID)
	if err != nil || key != "abcdefghijklmnop" || requests != 1 {
		t.Fatalf("wrong key result %q %v (calls=%d)", key, err, requests)
	}
}

func TestSharingFallbackHomes(t *testing.T) {
	client := clientFor(func(w http.ResponseWriter, r *http.Request) {
		_, key := sharingKey(r.Header.Get("X-requestId"), "refresh")
		var data string
		switch r.URL.Path {
		case "/v1.0/m/life/ha/devices/detail":
			data = `[]`
		case "/v1.0/m/life/users/homes":
			data = `[{"ownerId":304020044,"name":"Home"}]`
		case "/v1.0/m/life/ha/home/devices":
			raw, err := sharingAESOpen(key, r.URL.Query().Get("encdata"))
			if err != nil || !strings.Contains(string(raw), "304020044") {
				t.Errorf("home param %s %v", raw, err)
			}
			data = `[{"id":"lamp123","local_key":"abcdefghijklmnop"}]`
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		encrypted, _ := sharingAESSeal(key, []byte(data))
		result, _ := json.Marshal(encrypted)
		fmt.Fprintf(w, `{"success":true,"result":%s}`, result)
	})
	api := &sharingAPI{endpoint: testOrigin, client: client, token: sharingToken{AccessToken: "access", RefreshToken: "refresh"}}
	key, err := querySharingLocalKey(context.Background(), api, "lamp123")
	if err != nil || key != "abcdefghijklmnop" {
		t.Fatalf("fallback %q %v", key, err)
	}
}

func TestSharingEndpointRestrictionsAndMissingLocalKey(t *testing.T) {
	for _, s := range []string{"http://apigw.tuyaeu.com", "https://badtuyaeu.com", "https://apigw.tuyaeu.com.evil.test", "https://user@apigw.tuyaeu.com", "https://apigw.tuyaeu.com/path"} {
		if _, err := validateSharingEndpoint(s); err == nil {
			t.Errorf("accepted %s", s)
		}
	}
	for _, s := range []string{"https://apigw.tuyaeu.com", "https://openapi-weaz.tuyaeu.com"} {
		if _, err := validateSharingEndpoint(s); err != nil {
			t.Errorf("rejected %s: %v", s, err)
		}
	}
	_, present, err := findLocalKeyInSharingResult(json.RawMessage(`[{"id":"lamp","local_key":""}]`), "lamp")
	if !present || err == nil {
		t.Fatalf("should fail missing local_key, %v", err)
	}
}
