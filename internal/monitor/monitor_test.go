package monitor

import (
	"errors"
	"path/filepath"
	"testing"

	"example.com/tuya-beacon-mesh-p2-controller/internal/p2"
	"example.com/tuya-beacon-mesh-p2-controller/internal/state"
	"example.com/tuya-beacon-mesh-p2-controller/internal/testutil"
)

func TestP2MonitorDecodesAndFilters(t *testing.T) {
	k1, k2 := testutil.SampleKeys()
	cmds := []p2.DPCommand{p2.BoolDP(true), p2.BoolDP(false), p2.ModeDP(2), p2.ValueDP("bright", 3, 710), p2.ValueDP("temp", 4, 340), p2.HsvDP(193, 74, 85)}
	for i, cmd := range cmds {
		frame, err := p2.BuildFrame(0x7a59, 0x0003, uint16(0x100+i), cmd, k1, k2, 0x01)
		if err != nil {
			t.Fatal(err)
		}
		for _, test := range []struct {
			name    string
			payload []byte
		}{
			{"legacy", testutil.EncodeLegacyEvent(frame.Adv[:], -45)},
			{"extended", testutil.EncodeExtendedEvent(frame.Adv[:], -45)},
		} {
			t.Run(test.name+"/"+cmd.Label, func(t *testing.T) {
				packet := testutil.EncodeMonitorEvent(0, test.payload)
				found := observedFromMonitorPacket(packet, 0, 0x7a59, 0x0003, k1, k2)
				if len(found) != 1 || found[0].Seq != uint16(0x100+i) || found[0].Plain != cmd.Plain || found[0].RSSI != -45 {
					t.Fatalf("decoded=%+v want plain=%x seq=%04x", found, cmd.Plain, 0x100+i)
				}
				if got := observedFromMonitorPacket(packet, 1, 0x7a59, 0x0003, k1, k2); len(got) != 0 {
					t.Fatalf("wrong adapter accepted: %+v", got)
				}
				if got := observedFromMonitorPacket(packet, 0, 0x7a59, 0x0005, k1, k2); len(got) != 0 {
					t.Fatalf("wrong dst accepted: %+v", got)
				}
			})
		}
	}
}

func TestP2MonitorRejectsTampering(t *testing.T) {
	k1, k2 := testutil.SampleKeys()
	frame, err := p2.BuildFrame(0x7a59, 0x0003, 0x290, p2.BoolDP(true), k1, k2, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p2.DecodeObservedCommand(frame.Frame[:], k1, k2, 0x7a59, 0x0003, 0); !ok {
		t.Fatal("valid frame rejected")
	}
	for _, pos := range []int{0, 5, 7, 8, 9, 15, 24, 25} {
		p := frame.Frame
		p[pos] ^= 1
		if _, ok := p2.DecodeObservedCommand(p[:], k1, k2, 0x7a59, 0x0003, 0); ok {
			t.Fatalf("tampered byte %d passed verification", pos)
		}
	}
	bad := append([]byte(nil), k1...)
	bad[0] ^= 0xff
	if _, ok := p2.DecodeObservedCommand(frame.Frame[:], bad, k2, 0x7a59, 0x0003, 0); ok {
		t.Fatal("wrong local key accepted")
	}
	bad2 := append([]byte(nil), k2...)
	bad2[0] ^= 0xff
	if _, ok := p2.DecodeObservedCommand(frame.Frame[:], k1, bad2, 0x7a59, 0x0003, 0); ok {
		t.Fatal("wrong beacon key accepted")
	}
}

func TestP2MonitorUpdatesStateMonotonically(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state-device.json")
	for _, tc := range []struct {
		seq      uint16
		want     bool
		wantLast uint32
		older    bool
	}{
		{0x0100, true, 0x0100, false},
		{0x0100, false, 0x0100, false},
		{0x00ff, false, 0x0100, true},
		{0x0104, true, 0x0104, false},
	} {
		changed, last, err := state.SyncObservedSeq(path, "device", "fingerprint", tc.seq)
		if changed != tc.want || last != tc.wantLast || (errors.Is(err, state.ErrObservedOlder) != tc.older) {
			t.Fatalf("seq %04x: changed=%t last=%04x err=%v", tc.seq, changed, last, err)
		}
	}
	st, err := state.Open(path, "device", "fingerprint")
	if err != nil {
		t.Fatal(err)
	}
	seqs, err := st.Reserve(4)
	if err != nil || seqs[0] != 0x0105 || seqs[3] != 0x0108 {
		t.Fatalf("bad next send: %x %v", seqs, err)
	}
	st.Close()
}

func TestP2MonitorParserRejectsBadTruncation(t *testing.T) {
	k1, k2 := testutil.SampleKeys()
	frame, _ := p2.BuildFrame(0x7a59, 0x0003, 0x0100, p2.BoolDP(true), k1, k2, 1)
	packet := testutil.EncodeMonitorEvent(0, testutil.EncodeLegacyEvent(frame.Adv[:], -50))
	for i := 0; i < len(packet); i++ {
		if got := observedFromMonitorPacket(packet[:i], 0, 0x7a59, 0x0003, k1, k2); len(got) != 0 {
			t.Fatalf("partial monitor packet accepted at %d", i)
		}
	}
	for i := 0; i < len(frame.Adv); i++ {
		_ = p2.P2FramesFromAdvertisement(frame.Adv[:i])
	}
}
