package mqtt

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"example.com/tuya-beacon-mesh-p2-controller/internal/state"
)

type Options struct {
	Address, User, Password, ClientID          string
	Prefix, DiscoveryPrefix                    string
	Discovery                                  bool
	DeviceName, DeviceModel, LightName         string
	Firmware, ProductID, Hardware              string
	StatePath, DeviceID, Fingerprint           string
	Adapter, Backend                           string
	TxDuration, Debounce, MaxWait, DedupWindow time.Duration
	KelvinMin, KelvinMax                       int
	InvertTemp                                 bool
}

func DevicePrefix(root, deviceID string) string {
	root = strings.Trim(strings.TrimSpace(root), "/")
	if root == "" {
		root = "tuya-beacon-mesh-p2-controller"
	}
	id := state.CleanDeviceID(deviceID)
	if root == id || strings.HasSuffix(root, "/"+id) {
		return root
	}
	return root + "/" + id
}

type inboundPacket struct {
	kind    byte
	payload []byte
	err     error
}

type transmitResult struct {
	job queuedCommand
	err error
}

func connectSession(ctx context.Context, opt Options, execute func(context.Context, []string) error) error {
	addr, err := addressHost(opt.Address)
	if err != nil {
		return err
	}
	conn, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	return serveSession(ctx, conn, addr, opt, execute)
}

func serveSession(ctx context.Context, conn net.Conn, addr string, opt Options, execute func(context.Context, []string) error) error {
	stopDeadline := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now().Add(time.Second)) })
	defer stopDeadline()
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	reader, err := connect(conn, opt)
	_ = conn.SetReadDeadline(time.Time{})
	if err != nil {
		return err
	}
	commandTopics := []string{opt.Prefix + "/set", opt.Prefix + "/power/set", opt.Prefix + "/brightness/set", opt.Prefix + "/color/set", opt.Prefix + "/temperature/set", opt.Prefix + "/effect/set", opt.Prefix + "/effect/speed/set", opt.Prefix + "/ha/set"}
	if err := subscribe(conn, append(append([]string{}, commandTopics...), "homeassistant/status")); err != nil {
		return err
	}
	if err := publishDiscovery(conn, opt); err != nil {
		return err
	}
	if err := publishBridgeSnapshot(conn, opt); err != nil {
		return err
	}
	// Keep the previous per-device status for external MQTT clients. HA uses
	// bridge/state, whose retained offline LWT works after unexpected exit.
	if err := publish(conn, opt.Prefix+"/availability", []byte("online"), true, 0, 0); err != nil {
		return err
	}
	fmt.Printf("MQTT connected: %s; prefix=%s; HA discovery=%v; transport=%s adapter=%s; debounce=%s max_wait=%s\n", addr, opt.Prefix, opt.Discovery, opt.Backend, opt.Adapter, opt.Debounce, opt.MaxWait)
	cache := &snapshotCache{}
	if err := publishSnapshot(conn, opt, cache); err != nil {
		return err
	}

	// Reader always consumes PUBLISH packets, even while the BLE transmitter
	// is occupied. An MQTT burst is compressed to one latest value per control.
	sessionCtx, cancel := context.WithCancel(ctx)
	var workers sync.WaitGroup
	defer func() { cancel(); _ = conn.Close(); workers.Wait() }()
	workers.Add(2)
	packets := make(chan inboundPacket, 64)
	go func() {
		defer workers.Done()
		for {
			kind, payload, err := readPacket(reader)
			select {
			case packets <- inboundPacket{kind, payload, err}:
			case <-sessionCtx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()
	jobs := make(chan queuedCommand)
	results := make(chan transmitResult, 1)
	go func() {
		defer workers.Done()
		for {
			select {
			case <-sessionCtx.Done():
				return
			case job := <-jobs:
				var args []string
				var err error
				if job.Topic == opt.Prefix+"/ha/set" {
					args, err = haCommand(opt, job.Payload)
				} else {
					args, err = commandForTopic(opt.Prefix, job.Topic, job.Payload)
				}
				if err == nil && job.Topic == opt.Prefix+"/brightness/set" {
					args, err = brightnessCommand(opt, job.Payload)
				}
				if err == nil && job.Topic == opt.Prefix+"/effect/speed/set" {
					args, err = effectSpeedCommand(opt, job.Payload)
				}
				if err == nil {
					err = execute(ctx, args)
				}
				select {
				case results <- transmitResult{job, err}:
				case <-sessionCtx.Done():
					return
				}
			}
		}
	}()
	queue := newMQTTCommandQueue(opt.Debounce, opt.MaxWait, opt.DedupWindow)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	lastPing := time.Now()
	for {
		select {
		case <-ctx.Done():
			_ = publish(conn, bridgeStateTopic(opt), bridgeStateJSON("offline"), true, 0, 0)
			_ = publish(conn, opt.Prefix+"/availability", []byte("offline"), true, 0, 0)
			_ = writePacket(conn, 0xe0, nil)
			return nil
		case event := <-packets:
			if event.err != nil {
				return event.err
			}
			kind, p := event.kind, event.payload
			if kind&0xf0 != 0x30 {
				continue
			}
			qos := (kind >> 1) & 3
			if qos > 1 {
				return errors.New("MQTT QoS2 command not supported")
			}
			if len(p) < 2 {
				continue
			}
			n := int(binary.BigEndian.Uint16(p[:2]))
			if len(p) < 2+n {
				continue
			}
			topic := string(p[2 : 2+n])
			off := 2 + n
			if qos == 1 {
				if len(p) < off+2 {
					continue
				}
				packetID := p[off : off+2]
				off += 2
				if err := writePacket(conn, 0x40, packetID); err != nil {
					return err
				}
			}
			// HA restart: resend discovery, availability and the latest
			// snapshot, as Zigbee2MQTT does. Never treat status as a command.
			if topic == "homeassistant/status" {
				if kind&1 == 0 && strings.EqualFold(strings.TrimSpace(string(p[off:])), "online") {
					if err := publishDiscovery(conn, opt); err != nil {
						return err
					}
					if err := publishBridgeSnapshot(conn, opt); err != nil {
						return err
					}
					cache.initialized = false
					if err := publishSnapshot(conn, opt, cache); err != nil {
						return err
					}
				}
				continue
			}
			// Ignore retained commands when reconnecting to avoid stale actions.
			if kind&1 != 0 {
				continue
			}
			found := false
			for _, t := range commandTopics {
				if t == topic {
					found = true
					break
				}
			}
			if !found {
				continue
			}
			raw := string(p[off:])
			var validateErr error
			if topic == opt.Prefix+"/ha/set" {
				validateErr = validateHACommand(opt, raw)
			} else {
				_, validateErr = commandForTopic(opt.Prefix, topic, raw)
			}
			if err := validateErr; err != nil {
				result, _ := json.Marshal(map[string]any{"command": raw, "topic": topic, "ok": false, "confirmed": false, "error": err.Error()})
				if err = publish(conn, opt.Prefix+"/result", result, false, 0, 0); err != nil {
					return err
				}
				continue
			}
			queue.offer(topic, raw, time.Now())
		case result := <-results:
			queue.complete(result.job, result.err, time.Now())
			response := map[string]any{"command": result.job.Payload, "topic": result.job.Topic, "ok": result.err == nil, "confirmed": false}
			if result.err != nil {
				fmt.Fprintf(os.Stderr, "MQTT command failed on %s (%s/%s): %v\n", result.job.Topic, opt.Backend, opt.Adapter, result.err)
				response["error"] = result.err.Error()
			}
			msg, _ := json.Marshal(response)
			if err := publish(conn, opt.Prefix+"/result", msg, false, 0, 0); err != nil {
				return err
			}
			if err := publishSnapshot(conn, opt, cache); err != nil {
				return err
			}
		case <-ticker.C:
			if !queue.busy() {
				if job, ok := queue.nextReady(time.Now()); ok {
					select {
					case jobs <- job:
					case <-sessionCtx.Done():
						return nil
					}
				}
			}
			if err := publishSnapshot(conn, opt, cache); err != nil {
				return err
			}
			if time.Since(lastPing) >= 10*time.Second {
				if err := writePacket(conn, 0xc0, nil); err != nil {
					return err
				}
				lastPing = time.Now()
			}
		}
	}
}

func Run(ctx context.Context, opt Options, execute func(context.Context, []string) error) error {
	if strings.ContainsAny(opt.Prefix, "+#\x00") || strings.TrimSpace(opt.Prefix) == "" {
		return errors.New("invalid MQTT topic prefix")
	}
	if opt.Discovery && (strings.ContainsAny(opt.DiscoveryPrefix, "+#\x00") || strings.Trim(opt.DiscoveryPrefix, "/") == "") {
		return errors.New("invalid HA discovery prefix")
	}
	if opt.Password != "" && opt.User == "" {
		return errors.New("MQTT password needs username")
	}
	if _, err := addressHost(opt.Address); err != nil {
		return err
	}
	for {
		if ctx.Err() != nil {
			return nil
		}
		if err := connectSession(ctx, opt, execute); err != nil {
			fmt.Fprintf(os.Stderr, "MQTT disconnected: %v\n", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(5 * time.Second):
		}
	}
}
