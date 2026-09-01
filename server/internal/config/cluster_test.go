package config

import (
	"os"
	"testing"
)

func TestParseClusterRole(t *testing.T) {
	tests := []struct {
		envRole string
		want    string
	}{
		{"", ClusterRoleMaster},
		{"master", ClusterRoleMaster},
		{"worker", ClusterRoleWorker},
		{"WORKER", ClusterRoleWorker},
		{"other", ClusterRoleMaster},
	}

	for _, tt := range tests {
		os.Unsetenv("CLUSTER_ROLE")
		if tt.envRole != "" {
			os.Setenv("CLUSTER_ROLE", tt.envRole)
		}

		got := parseClusterRole()
		if got != tt.want {
			t.Errorf("parseClusterRole() role=%q = %q, want %q", tt.envRole, got, tt.want)
		}
	}
	os.Unsetenv("CLUSTER_ROLE")
}

func TestIsCronEnabled(t *testing.T) {
	origRole := ClusterRole
	t.Cleanup(func() { ClusterRole = origRole })

	ClusterRole = ClusterRoleMaster
	if !IsCronEnabled() {
		t.Fatal("master role should have cron enabled")
	}
	if IsClusterWorker() {
		t.Fatal("master role should not be cluster worker")
	}

	ClusterRole = ClusterRoleWorker
	if IsCronEnabled() {
		t.Fatal("worker role should have cron disabled")
	}
	if !IsClusterWorker() {
		t.Fatal("worker role should be cluster worker")
	}
}
