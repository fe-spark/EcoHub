package config

import "testing"

func TestLoadAccessRuntimeConfig(t *testing.T) {
	JwtSecret = "test-secret"
	loadAccessRuntimeConfig()
	if len(AnalyticsIPSalt) == 0 {
		t.Fatal("expected AnalyticsIPSalt to be generated")
	}
}
