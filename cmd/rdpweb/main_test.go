package main

import (
	"testing"
	"time"
)

func TestBoolEnvAutoAndOverrides(t *testing.T) {
	t.Setenv("RDPWEB_BOOL_TEST", "auto")
	if !boolEnv("RDPWEB_BOOL_TEST", true) {
		t.Fatal("auto must preserve true fallback")
	}
	if boolEnv("RDPWEB_BOOL_TEST", false) {
		t.Fatal("auto must preserve false fallback")
	}

	t.Setenv("RDPWEB_BOOL_TEST", "true")
	if !boolEnv("RDPWEB_BOOL_TEST", false) {
		t.Fatal("explicit true not honored")
	}
	t.Setenv("RDPWEB_BOOL_TEST", "false")
	if boolEnv("RDPWEB_BOOL_TEST", true) {
		t.Fatal("explicit false not honored")
	}
}

func TestDurationEnvRejectsUnsafeShortValues(t *testing.T) {
	fallback := 24 * time.Hour
	t.Setenv("RDPWEB_DURATION_TEST", "30m")
	if got := durationEnv("RDPWEB_DURATION_TEST", fallback); got != fallback {
		t.Fatalf("short duration accepted: %s", got)
	}
	t.Setenv("RDPWEB_DURATION_TEST", "2h")
	if got := durationEnv("RDPWEB_DURATION_TEST", fallback); got != 2*time.Hour {
		t.Fatalf("valid duration rejected: %s", got)
	}
}
