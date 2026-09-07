package config

import "testing"

func TestParseEnvBool(t *testing.T) {
	t.Setenv("TEST_BOOL_VAR", "true")
	if !parseEnvBool("TEST_BOOL_VAR", false) {
		t.Fatal("expected true for 'true'")
	}

	t.Setenv("TEST_BOOL_VAR", "0")
	if parseEnvBool("TEST_BOOL_VAR", true) {
		t.Fatal("expected false for '0'")
	}

	t.Setenv("TEST_BOOL_VAR", "invalid")
	if !parseEnvBool("TEST_BOOL_VAR", true) {
		t.Fatal("expected fallback true for 'invalid'")
	}

	if parseEnvBool("UNSET_BOOL_VAR", false) {
		t.Fatal("expected fallback false for unset var")
	}
}
