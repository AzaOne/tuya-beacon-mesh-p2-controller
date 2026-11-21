package state

func Read(path, device, fp string) (Snapshot, error) {
	st, err := Open(path, device, fp)
	if err != nil {
		return Snapshot{}, err
	}
	defer st.Close()
	return st.State, nil
}
