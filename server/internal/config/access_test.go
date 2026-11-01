package config

import "testing"

func TestLoadAccessRuntimeConfig(t *testing.T) {
	JwtSecret = "test-secret"
	loadAccessRuntimeConfig()
	if len(AccessIPSalt) == 0 {
		t.Fatal("expected AccessIPSalt to be generated")
	}
}
