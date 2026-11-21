package bluez

func DispatchHelper(argv []string) (bool, error) {
	if len(argv) == 6 && argv[0] == "__raw-hci-send" {
		return true, runRawHCISendHelper(argv[1:])
	}
	if len(argv) == 6 && argv[0] == "__mgmt-send" {
		return true, runMGMTSendHelper(argv[1:])
	}
	if len(argv) == 2 && argv[0] == "__hci-monitor" {
		return true, runHCIMonitorHelper(argv[1])
	}
	return false, nil
}
