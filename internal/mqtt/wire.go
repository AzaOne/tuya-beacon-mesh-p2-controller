package mqtt

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"

	"example.com/tuya-beacon-mesh-p2-controller/internal/state"
)

func addressHost(broker string) (string, error) {
	broker = strings.TrimSpace(broker)
	if broker == "" {
		return "", errors.New("MQTT broker is empty")
	}
	if !strings.Contains(broker, "://") {
		broker = "tcp://" + broker
	}
	u, err := url.Parse(broker)
	if err != nil {
		return "", err
	}
	if u.Scheme != "tcp" {
		return "", fmt.Errorf("MQTT scheme %q is unsupported (tcp:// only)", u.Scheme)
	}
	if u.User != nil || u.RawQuery != "" || u.Path != "" || u.Fragment != "" {
		return "", errors.New("MQTT broker must contain only tcp://host:port")
	}
	if _, _, err := net.SplitHostPort(u.Host); err != nil {
		return "", fmt.Errorf("MQTT broker host:port: %w", err)
	}
	return u.Host, nil
}

func encodeString(s string) []byte {
	if len(s) > 65535 {
		return nil
	}
	b := make([]byte, 2+len(s))
	binary.BigEndian.PutUint16(b[:2], uint16(len(s)))
	copy(b[2:], s)
	return b
}

func encodeRemainingLength(n int) []byte {
	var b []byte
	for {
		v := byte(n % 128)
		n /= 128
		if n > 0 {
			v |= 128
		}
		b = append(b, v)
		if n == 0 {
			return b
		}
	}
}

func writePacket(w io.Writer, kind byte, payload []byte) error {
	hdr := append([]byte{kind}, encodeRemainingLength(len(payload))...)
	if _, err := w.Write(hdr); err != nil {
		return err
	}
	if len(payload) == 0 {
		return nil
	}
	_, err := w.Write(payload)
	return err
}

func readPacket(r *bufio.Reader) (byte, []byte, error) {
	kind, err := r.ReadByte()
	if err != nil {
		return 0, nil, err
	}
	n, mult := 0, 1
	for i := 0; i < 4; i++ {
		v, e := r.ReadByte()
		if e != nil {
			return 0, nil, e
		}
		n += int(v&0x7f) * mult
		if v&0x80 == 0 {
			break
		}
		if i == 3 {
			return 0, nil, errors.New("malformed MQTT remaining length")
		}
		mult *= 128
	}
	if n > 65536 {
		return 0, nil, errors.New("MQTT packet exceeds 64 KiB")
	}
	buf := make([]byte, n)
	_, err = io.ReadFull(r, buf)
	return kind, buf, err
}

func publish(conn net.Conn, topic string, data []byte, retained bool, qos byte, id uint16) error {
	if strings.ContainsAny(topic, "+#\x00") || topic == "" {
		return errors.New("invalid MQTT topic")
	}
	p := append(encodeString(topic), data...)
	kind := byte(0x30)
	if retained {
		kind |= 1
	}
	if qos == 1 {
		kind |= 0x02
		p = append(encodeString(topic), byte(id>>8), byte(id))
		p = append(p, data...)
	}
	return writePacket(conn, kind, p)
}

func connect(conn net.Conn, opt Options) (*bufio.Reader, error) {
	if opt.User == "" && opt.Password != "" {
		return nil, errors.New("MQTT password needs username")
	}
	// clean session + retained last will, QoS0; username and password as needed
	flags := byte(0x02 | 0x04 | 0x20)
	if opt.User != "" {
		flags |= 0x80
		if opt.Password != "" {
			flags |= 0x40
		}
	}
	id := opt.ClientID
	if id == "" {
		id = "tuya-p2-" + state.CleanDeviceID(opt.DeviceID)
	}
	if len(id) > 65535 {
		return nil, errors.New("client ID too long")
	}
	payload := append(encodeString("MQTT"), 4, flags, 0, 30)
	payload = append(payload, encodeString(id)...)
	payload = append(payload, encodeString(bridgeStateTopic(opt))...)
	payload = append(payload, encodeString(string(bridgeStateJSON("offline")))...)
	if opt.User != "" {
		payload = append(payload, encodeString(opt.User)...)
		if opt.Password != "" {
			payload = append(payload, encodeString(opt.Password)...)
		}
	}
	if err := writePacket(conn, 0x10, payload); err != nil {
		return nil, err
	}
	rd := bufio.NewReader(conn)
	kind, ack, err := readPacket(rd)
	if err != nil {
		return nil, err
	}
	if kind != 0x20 || len(ack) != 2 || ack[1] != 0 {
		return nil, fmt.Errorf("MQTT CONNACK rejected: %x %x", kind, ack)
	}
	return rd, nil
}

func subscribe(conn net.Conn, topics []string) error {
	payload := []byte{0, 1} // packet ID
	for _, t := range topics {
		payload = append(payload, encodeString(t)...)
		payload = append(payload, 1)
	}
	return writePacket(conn, 0x82, payload)
}
