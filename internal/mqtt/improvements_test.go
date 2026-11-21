package mqtt

import (
	"testing"
)

func TestMQTTCommandsRestricted(t *testing.T) {
	for _, c := range []string{"on", "rgb 100 232 111", "white 4000 80", "temp 5000"} {
		if _, err := allowedCommand(c); err != nil {
			t.Fatalf("rejected %s: %v", c, err)
		}
	}
	for _, c := range []string{"raw 011101", "seq sync 0", "learn file.cfa", "dp bool 7 on", ""} {
		if _, err := allowedCommand(c); err == nil {
			t.Fatalf("accepted %s", c)
		}
	}
}
