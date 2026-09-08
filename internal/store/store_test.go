package store

import "testing"

func TestValidateDeviceRejectsRDPInjection(t *testing.T) {
	base := Device{ID: "1", Name: "server", Host: "example.com", Port: 3389, Username: "alice", RedirectClipboard: true}
	if err := ValidateDevice(base); err != nil {
		t.Fatal(err)
	}
	bad := base
	bad.Username = "alice\r\nredirectdrives:i:1"
	if err := ValidateDevice(bad); err == nil {
		t.Fatal("expected CRLF injection to be rejected")
	}
	bad = base
	bad.Host = "https://example.com"
	if err := ValidateDevice(bad); err == nil {
		t.Fatal("expected URL host to be rejected")
	}
}

func TestAddressIPv6(t *testing.T) {
	got := Address(Device{Host: "2001:db8::1", Port: 3390})
	if got != "[2001:db8::1]:3390" {
		t.Fatalf("unexpected address %q", got)
	}
}
