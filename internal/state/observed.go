package state

import (
	"errors"

	"example.com/tuya-beacon-mesh-p2-controller/internal/p2"
)

var ErrObservedOlder = errors.New("observed seq lower than stored high-water")

func SyncObservedSeq(path, device, fp string, observed uint16) (changed bool, last uint32, err error) {
	st, err := Open(path, device, fp)
	if err != nil {
		return false, 0, err
	}
	defer st.Close()
	if uint32(observed) < st.State.LastReserved {
		return false, st.State.LastReserved, ErrObservedOlder
	}
	changed, err = st.SyncTo(observed)
	return changed, st.State.LastReserved, err
}

// syncObservedFull advances the counter and saves an unconfirmed logical state.
// It does not overwrite a newer state with a delayed/retransmitted packet.
func SyncObservedFull(path, device, fp string, observed p2.ObservedCommand, source string) (bool, uint32, error) {
	st, err := Open(path, device, fp)
	if err != nil {
		return false, 0, err
	}
	defer st.Close()
	if uint32(observed.Seq) < st.State.LastReserved {
		return false, st.State.LastReserved, ErrObservedOlder
	}
	advanced := uint32(observed.Seq) > st.State.LastReserved
	if !advanced && !st.FirstRun {
		return false, st.State.LastReserved, nil
	}
	st.State.LastReserved = uint32(observed.Seq)
	st.State.Light = applyDPs(st.State.Light, observed.DPs, source)
	st.State.learnScenes(observed.DPs)
	err = st.Write()
	return true, st.State.LastReserved, err
}
