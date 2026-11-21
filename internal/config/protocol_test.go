package config

import (
	"testing"
)

func TestKeyParsing(t *testing.T) {
	k, err := ParseKey("0123456789ABCDEF")
	if err != nil || string(k) != "0123456789ABCDEF" {
		t.Fatal("expected 16 ASCII")
	}
	k, err = ParseKey("000102030405060708090a0b0c0d0e0f")
	if err != nil || len(k) != 16 || k[1] != 1 || k[15] != 15 {
		t.Fatal("expected hex32")
	}
	if _, err := ParseKey("short"); err == nil {
		t.Fatal("short key accepted")
	}
}
