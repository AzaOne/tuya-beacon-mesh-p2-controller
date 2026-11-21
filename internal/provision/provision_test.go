package provision

import (
	"context"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"example.com/tuya-beacon-mesh-p2-controller/internal/bluez"
	"example.com/tuya-beacon-mesh-p2-controller/internal/p2"
	"example.com/tuya-beacon-mesh-p2-controller/internal/testutil"
)

func TestTuyaRegionHosts(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"eu", "https://openapi.tuyaeu.com"}, {"central", "https://openapi.tuyaeu.com"},
		{"we", "https://openapi-weaz.tuyaeu.com"}, {"western", "https://openapi-weaz.tuyaeu.com"},
	} {
		got, err := tuyaRegionBaseURL(tc.in)
		if err != nil || got != tc.want {
			t.Fatalf("%s: %s %v", tc.in, got, err)
		}
	}
	if _, err := tuyaRegionBaseURL("https://example.com"); err == nil {
		t.Fatal("should reject arbitrary host")
	}
}

func TestTuyaCloudSignedLocalKey(t *testing.T) {
	const cid = "example-id"
	const secret = "example-secret"
	const key = "0123456789abcdef"
	var calls int
	client := clientFor(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "GET" {
			t.Errorf("method=%s", r.Method)
		}
		token := ""
		if calls == 2 {
			token = "temporary-access-token"
			if r.Header.Get("access_token") != token {
				t.Errorf("missing temporary token")
			}
		}
		if actual, expected := r.Header.Get("sign"), tuyaSignature(cid, secret, token, r.Header.Get("t"), r.URL.RequestURI()); actual != expected {
			t.Errorf("invalid HMAC signature: %q != %q", actual, expected)
		}
		if r.Header.Get("client_id") != cid {
			t.Error("bad client_id")
		}
		switch r.URL.RequestURI() {
		case "/v1.0/token?grant_type=1":
			fmt.Fprint(w, `{"success":true,"result":{"access_token":"temporary-access-token"}}`)
		case "/v2.0/cloud/thing/thisdevice":
			fmt.Fprintf(w, `{"success":true,"result":{"id":"thisdevice","local_key":%q}}`, key)
		default:
			t.Errorf("unknown endpoint %s", r.URL.RequestURI())
			http.Error(w, "bad", 404)
		}
	})
	got, err := queryTuyaLocalKey(context.Background(), client, testOrigin, cid, secret, "thisdevice")
	if err != nil || got != key || calls != 2 {
		t.Fatalf("got=%q err=%v calls=%d", got, err, calls)
	}
}

func TestCloudFailureAndNoSecretLeak(t *testing.T) {
	client := clientFor(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"success":false,"code":28841002,"msg":"IoT Core service subscription has expired."}`)
	})
	_, err := queryTuyaLocalKey(context.Background(), client, testOrigin, "id", "secret", "lamp")
	if err == nil || !strings.Contains(err.Error(), "28841002") {
		t.Fatalf("unexpected error %v", err)
	}
	if strings.Contains(err.Error(), "secret") {
		t.Fatal("secret leaked")
	}
}

func TestRecoverAutoAddressFromKnownDP(t *testing.T) {
	k1 := []byte("0123456789abcdef")
	k2, _ := hex.DecodeString("00112233445566778899aabbccddeeff")
	var frames [][]byte
	for i, cmd := range []p2.DPCommand{p2.BoolDP(true), p2.BoolDP(false), p2.BoolDP(true), p2.BoolDP(false)} {
		fr, err := p2.BuildFrame(0x2233, 0x1122, uint16(100+i), cmd, k1, k2, 1)
		if err != nil {
			t.Fatal(err)
		}
		frames = append(frames, append([]byte(nil), fr.Frame[:]...))
	}
	_, err := recoverKeyAuto(frames, k1, 0x7a59, 0x0003, false)
	if err == nil {
		t.Fatal("disabled auto detect must not find unrelated pair")
	}
	found, err := recoverKeyAuto(frames, k1, 0x7a59, 0x0003, true)
	if err != nil {
		t.Fatal(err)
	}
	if found.BeaconKey != "00112233445566778899aabbccddeeff" || found.Source != "0x2233" || found.Destination != "0x1122" || found.VerifiedFrames != 4 {
		t.Fatalf("got %+v", found)
	}
}

func TestRecoverKeyFromSimulatedPCAdvertising(t *testing.T) {
	k1, k2 := testutil.SampleKeys()
	var sniffed [][]byte
	for i, cmd := range []p2.DPCommand{p2.BoolDP(true), p2.BoolDP(true), p2.BoolDP(false), p2.BoolDP(false)} {
		frame, err := p2.BuildFrame(0x4321, 0x0015, uint16(0x201+i), cmd, k1, k2, 1)
		if err != nil {
			t.Fatal(err)
		}
		packet := testutil.EncodeMonitorEvent(0, testutil.EncodeLegacyEvent(frame.Adv[:], -42))
		if len(packet) < 6 {
			t.Fatal("short packet")
		}
		for _, ad := range bluez.AdvertisementsFromHCIEvent(packet[6:]) {
			sniffed = append(sniffed, p2.P2FramesFromAdvertisement(ad.Data)...)
		}
	}
	if len(sniffed) != 4 {
		t.Fatalf("received %d frames, expected 4", len(sniffed))
	}
	learned, err := recoverKeyAuto(sniffed, k1, 0x7a59, 0x0003, true)
	if err != nil {
		t.Fatal(err)
	}
	if learned.Source != "0x4321" || learned.Destination != "0x0015" || learned.VerifiedFrames != 4 {
		t.Fatalf("learned %+v", learned)
	}
}
