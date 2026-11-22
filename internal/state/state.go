package state

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"example.com/tuya-beacon-mesh-p2-controller/internal/p2"
	"example.com/tuya-beacon-mesh-p2-controller/internal/xdg"
)

// Initial value is the last observed counter in the user's 2026-11-20 capture,
// NOT a guaranteed current value. The first generated sequence is this + 1.
const capturedLastSeq uint16 = 0x008e

type Snapshot struct {
	Version      int         `json:"version"`
	Device       string      `json:"device"`
	Fingerprint  string      `json:"fingerprint"`
	LastReserved uint32      `json:"last_reserved"`
	Light        *LightState `json:"light,omitempty"`
	// Most recent RAW for each scene ID, learned ONLY from verified phone traffic.
	// Allows a captured scene to override its original catalog template.
	SceneTemplates map[string]string `json:"scene_templates,omitempty"`
}

type LightState struct {
	Power          *bool        `json:"power,omitempty"`
	Mode           *byte        `json:"mode,omitempty"`
	BrightnessRaw  *uint32      `json:"brightness_raw,omitempty"`
	TemperatureRaw *uint32      `json:"temperature_raw,omitempty"`
	HSV            *[3]int      `json:"hsv,omitempty"`
	Scene          *SceneStatus `json:"scene,omitempty"`
	LastSource     string       `json:"last_source,omitempty"`
	UpdatedAt      string       `json:"updated_at,omitempty"`
	Confirmed      bool         `json:"confirmed"`
}

type SceneStatus struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	Speed      int    `json:"speed"`
	Brightness int    `json:"brightness"`
	Raw        string `json:"raw"`
	// Informs consumers that this is an observed/commanded (not ACKed) scene.
	Confirmed bool `json:"confirmed"`
}

// Record raw templates ONLY when they came from a cryptographically verified
// Smart Life frame, never from a user-supplied or inferred CLI command.
func (s *Snapshot) learnScenes(dps []p2.ParsedDP) {
	for _, dp := range dps {
		if dp.ID != 73 || dp.Type != 0 || len(dp.Value) < 7 || len(dp.Value) > 14 {
			continue
		}
		if _, ok := p2.GetScene(dp.Value[0]); !ok {
			continue
		}
		if s.SceneTemplates == nil {
			s.SceneTemplates = make(map[string]string)
		}
		s.SceneTemplates[fmt.Sprint(dp.Value[0])] = hex.EncodeToString(dp.Value)
	}
}

func applyDPs(prev *LightState, dps []p2.ParsedDP, source string) *LightState {
	var v LightState
	if prev != nil {
		v = *prev
	}
	for _, dp := range dps {
		switch {
		case dp.ID == 1 && dp.Type == 1 && len(dp.Value) == 1:
			b := dp.Value[0] != 0
			v.Power = &b
		case dp.ID == 2 && dp.Type == 4 && len(dp.Value) == 1:
			b := dp.Value[0]
			v.Mode = &b
		case dp.ID == 3 && dp.Type == 2 && len(dp.Value) == 4:
			n := binary.BigEndian.Uint32(dp.Value)
			v.BrightnessRaw = &n
		case dp.ID == 4 && dp.Type == 2 && len(dp.Value) == 4:
			n := binary.BigEndian.Uint32(dp.Value)
			v.TemperatureRaw = &n
		case dp.ID == 11 && dp.Type == 0 && len(dp.Value) == 4:
			h := [3]int{int(binary.BigEndian.Uint16(dp.Value[:2])), int(dp.Value[2]), int(dp.Value[3])}
			v.HSV = &h
		case dp.ID == 73 && dp.Type == 0 && len(dp.Value) >= 7 && len(dp.Value) <= 14:
			if p, ok := p2.GetScene(dp.Value[0]); ok {
				sceneMode := byte(2)
				v.Mode = &sceneMode // DP73 alone selects the scene; phone does not send separate DP2.
				v.Scene = &SceneStatus{ID: int(dp.Value[0]), Name: p.Name, Speed: int(dp.Value[4]), Brightness: int(dp.Value[5]), Raw: hex.EncodeToString(dp.Value), Confirmed: false}
			}
		}
	}
	v.LastSource = source
	v.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	v.Confirmed = false
	return &v
}

type Store struct {
	StatePath string
	Lock      *os.File
	State     Snapshot
	FirstRun  bool
}

func Fingerprint(deviceID string, src, dst uint16, k1, k2 []byte) string {
	h := sha256.New()
	_, _ = h.Write([]byte("Tuya-P2-seq-store-v2\x00" + deviceID))
	_, _ = h.Write([]byte{byte(src >> 8), byte(src), byte(dst >> 8), byte(dst)})
	_, _ = h.Write(k1)
	_, _ = h.Write(k2)
	return hex.EncodeToString(h.Sum(nil))[:24]
}

func CleanDeviceID(deviceID string) string {
	deviceID = strings.TrimSpace(deviceID)
	clean := make([]rune, 0, len(deviceID))
	for _, r := range deviceID {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			clean = append(clean, r)
		}
	}
	if len(clean) == 0 {
		return "lamp"
	}
	return string(clean)
}

func DefaultStatePath(deviceID string) string {
	return filepath.Join(xdg.StateDir(), "state-"+CleanDeviceID(deviceID)+".json")
}

// Old v2 location, used solely to import the latest seq when upgrading.
func legacyStatePath(deviceID string) (string, error) {
	base := os.Getenv("XDG_STATE_HOME")
	if !filepath.IsAbs(base) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "tuya-p2", CleanDeviceID(deviceID)+".json"), nil
}

// MigrateLegacyState imports the complete snapshot once. Installations may
// have run binaries from both the working directory and build/.
func MigrateLegacyState(newPath, device, fp string) (bool, error) {
	if _, err := os.Stat(newPath); err == nil {
		return false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	name := filepath.Base(DefaultStatePath(device))
	paths := []string{name, filepath.Join("state", name)}
	exe, err := os.Executable()
	if err != nil {
		return false, err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	paths = append(paths, filepath.Join(filepath.Dir(exe), name))
	var best *Snapshot
	for _, path := range paths {
		snapshot, err := previousSnapshot(path, device, fp)
		if err != nil {
			return false, err
		}
		if snapshot != nil && (best == nil || snapshot.LastReserved > best.LastReserved) {
			best = snapshot
		}
	}
	// XDG was superseded by the executable-relative store in earlier releases.
	if best == nil {
		path, err := legacyStatePath(device)
		if err != nil {
			return false, err
		}
		best, err = previousSnapshot(path, device, fp)
		if err != nil {
			return false, err
		}
	}
	if best == nil {
		return false, nil
	}
	store, err := Open(newPath, device, fp)
	if err != nil {
		return false, err
	}
	defer store.Close()
	if !store.FirstRun {
		return false, nil
	}
	store.State = *best
	if err := store.Write(); err != nil {
		return false, fmt.Errorf("migrate previous state: %w", err)
	}
	return true, nil
}

func previousSnapshot(path, device, fp string) (*Snapshot, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	snapshot, err := Read(path, device, fp)
	if err != nil {
		return nil, fmt.Errorf("read previous state %s: %w", path, err)
	}
	return &snapshot, nil
}

func Open(path, device, fp string) (*Store, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("create state dir: %w", err)
	}
	lockFile, err := os.OpenFile(path+".lock", os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX); err != nil {
		lockFile.Close()
		return nil, err
	}
	st := &Store{StatePath: path, Lock: lockFile, State: Snapshot{Version: 1, Device: device, Fingerprint: fp, LastReserved: uint32(capturedLastSeq)}, FirstRun: true}
	b, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			st.Close()
			return nil, err
		}
		return st, nil
	}
	if err := json.Unmarshal(b, &st.State); err != nil {
		st.Close()
		return nil, fmt.Errorf("state file malformed: %w", err)
	}
	if st.State.Version != 1 || st.State.Fingerprint != fp || st.State.Device != device {
		st.Close()
		return nil, fmt.Errorf("state file belongs to different key/device/src/dst: %s; use a separate --state file after re-pairing", path)
	}
	if st.State.LastReserved > 0xffff {
		st.Close()
		return nil, errors.New("state last_reserved out of range")
	}
	st.FirstRun = false
	return st, nil
}

func (s *Store) Close() {
	if s.Lock != nil {
		_ = syscall.Flock(int(s.Lock.Fd()), syscall.LOCK_UN)
		_ = s.Lock.Close()
		s.Lock = nil
	}
}

func (s *Store) Write() error {
	data, err := json.MarshalIndent(s.State, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	dir := filepath.Dir(s.StatePath)
	tmp, err := os.CreateTemp(dir, ".seq-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err = tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if _, err = tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Rename(tmp.Name(), s.StatePath); err != nil {
		return err
	}
	d, err := os.Open(dir)
	if err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	s.FirstRun = false
	return nil
}

// reserve persists the new HIGH WATER before any packet is sent.
// Even after a crash or radio failure we never reuse a reserved counter.
func (s *Store) Reserve(n int) ([]uint16, error) {
	if n <= 0 {
		return nil, errors.New("no frames to reserve")
	}
	if s.State.LastReserved+uint32(n) > 0xffff {
		return nil, errors.New("16-bit seq counter would wrap: stop; do not reuse old values; re-pair/re-synchronize")
	}
	start := s.State.LastReserved
	s.State.LastReserved += uint32(n)
	if err := s.Write(); err != nil {
		return nil, fmt.Errorf("persist seq before send: %w", err)
	}
	seqs := make([]uint16, n)
	for i := range seqs {
		seqs[i] = uint16(start) + uint16(i) + 1
	}
	return seqs, nil
}

// Sync only advances the counter. No accidental rollback is allowed.
func (s *Store) SyncTo(value uint16) (bool, error) {
	if uint32(value) < s.State.LastReserved {
		return false, fmt.Errorf("refusing to decrease last seq from 0x%04x to 0x%04x", s.State.LastReserved, value)
	}
	if uint32(value) == s.State.LastReserved && !s.FirstRun {
		return false, nil
	}
	s.State.LastReserved = uint32(value)
	return true, s.Write()
}
