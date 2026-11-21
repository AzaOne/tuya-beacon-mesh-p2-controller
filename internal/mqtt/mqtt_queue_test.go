package mqtt

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"example.com/tuya-beacon-mesh-p2-controller/internal/state"
)

func TestMQTTBurstCoalescedAndLastValueWins(t *testing.T) {
	q := newMQTTCommandQueue(300*time.Millisecond, 1200*time.Millisecond, 2*time.Second)
	base := time.Unix(12345, 0)
	topic := "tuya-beacon-mesh-p2-controller/device/brightness/set"
	var current queuedCommand
	var completed []queuedCommand
	var endAt time.Time
	active := false
	for tick := 0; tick < 350; tick++ {
		now := base.Add(time.Duration(tick) * 20 * time.Millisecond)
		// Simulate a continuous drag generating 135 MQTT publications.
		if tick < 135 {
			value := strconv.Itoa(1 + tick%100)
			q.offer(topic, value, now)
		}
		if active && !now.Before(endAt) {
			q.complete(current, nil, now)
			completed = append(completed, current)
			active = false
		}
		if !active {
			if job, ok := q.nextReady(now); ok {
				current = job
				endAt = now.Add(500 * time.Millisecond)
				active = true
			}
		}
	}
	if active {
		completed = append(completed, current)
	}
	if len(completed) == 0 || len(completed) > 5 {
		t.Fatalf("135 incoming slider values became %d radio transmissions; want <=5", len(completed))
	}
	if completed[len(completed)-1].Payload != "35" {
		t.Fatalf("last value = %q, want 35", completed[len(completed)-1].Payload)
	}
	if len(q.pending) != 0 {
		t.Fatalf("pending commands after burst: %+v", q.pending)
	}
}

func TestMQTTQueueDedupPriorityAndReset(t *testing.T) {
	base := time.Unix(111, 0)
	q := newMQTTCommandQueue(300*time.Millisecond, time.Second, 2*time.Second)
	topic := "p/brightness/set"
	if !q.offer(topic, "20", base) {
		t.Fatal("initial command ignored")
	}
	if q.offer(topic, "20", base.Add(20*time.Millisecond)) {
		t.Fatal("duplicate extended debounce")
	}
	if job, ok := q.nextReady(base.Add(100 * time.Millisecond)); ok {
		t.Fatalf("premature send %+v", job)
	}
	if !q.offer(topic, "50", base.Add(200*time.Millisecond)) {
		t.Fatal("replacement ignored")
	}
	job, ok := q.nextReady(base.Add(550 * time.Millisecond))
	if !ok || job.Payload != "50" {
		t.Fatalf("coalescing: %+v %v", job, ok)
	}
	if q.offer(topic, "50", base.Add(600*time.Millisecond)) {
		t.Fatal("same as in-flight should be ignored")
	}
	if !q.offer(topic, "60", base.Add(650*time.Millisecond)) {
		t.Fatal("new in-flight value ignored")
	}
	q.complete(job, nil, base.Add(700*time.Millisecond))
	next, ok := q.nextReady(base.Add(1 * time.Second))
	if !ok || next.Payload != "60" {
		t.Fatalf("next value %+v %v", next, ok)
	}
	q.complete(next, nil, base.Add(1100*time.Millisecond))
	if q.offer(topic, "60", base.Add(1500*time.Millisecond)) {
		t.Fatal("repeated recent value not deduped")
	}
	if !q.offer(topic, "60", base.Add(4000*time.Millisecond)) {
		t.Fatal("retry outside dedup window was lost")
	}
	q.offer("p/color/set", "10,20,30", base.Add(4100*time.Millisecond))
	q.offer("p/power/set", "OFF", base.Add(4150*time.Millisecond))
	if _, ok := q.pending["p/color/set"]; ok {
		t.Fatal("old color change survived OFF")
	}
	// POWER should be sent immediately rather than held behind slider debounce.
	power, ok := q.nextReady(base.Add(4150 * time.Millisecond))
	if !ok || power.Payload != "OFF" {
		t.Fatalf("power dispatch %+v %t", power, ok)
	}
}

// Buffer-based net.Conn lets us inspect MQTT PUBLISH packets without opening a broker.
type bufferConn struct{ bytes.Buffer }

func (c *bufferConn) Read(_ []byte) (int, error) { return 0, io.EOF }

func (c *bufferConn) Close() error { return nil }

func (c *bufferConn) LocalAddr() net.Addr { return &net.TCPAddr{} }

func (c *bufferConn) RemoteAddr() net.Addr { return &net.TCPAddr{} }

func (c *bufferConn) SetDeadline(time.Time) error { return nil }

func (c *bufferConn) SetReadDeadline(time.Time) error { return nil }

func (c *bufferConn) SetWriteDeadline(time.Time) error { return nil }

func TestMQTTStateNoRepublishOnSeqOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	opts := Options{Prefix: "lamp/test", StatePath: path, DeviceID: "test", Fingerprint: "fp", KelvinMin: 2700, KelvinMax: 6500}
	st, err := state.Open(path, "test", "fp")
	if err != nil {
		t.Fatal(err)
	}
	on := true
	brightness := uint32(700)
	st.State.Light = &state.LightState{Power: &on, BrightnessRaw: &brightness}
	if err = st.Write(); err != nil {
		t.Fatal(err)
	}
	st.Close()
	conn := &bufferConn{}
	cache := &snapshotCache{}
	if err := publishSnapshot(conn, opts, cache); err != nil {
		t.Fatal(err)
	}
	before := conn.Len()
	if before == 0 {
		t.Fatal("no initial state sent")
	}
	st, err = state.Open(path, "test", "fp")
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.Reserve(3)
	st.Close()
	if err != nil {
		t.Fatal(err)
	}
	if err := publishSnapshot(conn, opts, cache); err != nil {
		t.Fatal(err)
	}
	if conn.Len() != before {
		t.Fatalf("seq-only update wrote %d additional bytes", conn.Len()-before)
	}
	st, err = state.Open(path, "test", "fp")
	if err != nil {
		t.Fatal(err)
	}
	brightness = 850
	st.State.Light.BrightnessRaw = &brightness
	if err = st.Write(); err != nil {
		t.Fatal(err)
	}
	st.Close()
	if err := publishSnapshot(conn, opts, cache); err != nil {
		t.Fatal(err)
	}
	if got := bytes.Count(conn.Bytes(), []byte("lamp/test/power/state")); got != 1 {
		t.Fatalf("power state republished %d times", got)
	}
	if got := bytes.Count(conn.Bytes(), []byte("lamp/test/brightness/state")); got != 2 {
		t.Fatalf("brightness updated %d times", got)
	}
	if !strings.Contains(conn.String(), fmt.Sprint("85")) {
		t.Fatal("new brightness missing")
	}
}
