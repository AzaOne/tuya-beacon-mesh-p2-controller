package state

import (
	"example.com/tuya-beacon-mesh-p2-controller/internal/p2"
)

func SaveSentState(statePath, device, fp string, commands []p2.DPCommand, lastSent uint32) error {
	st, err := Open(statePath, device, fp)
	if err != nil {
		return err
	}
	defer st.Close()
	// Do not override a fresher phone command seen by the monitor.
	if st.State.LastReserved > lastSent {
		return nil
	}
	var all []p2.ParsedDP
	for _, cmd := range commands {
		dps, err := p2.ParseDPBlock(cmd.Plain[:])
		if err != nil {
			return err
		}
		all = append(all, dps...)
	}
	st.State.Light = applyDPs(st.State.Light, all, "cli")
	return st.Write()
}
