package config

import "testing"

func TestWriteSlotMax(t *testing.T) {
	if got := WriteSlotMax(); got != MysqlMaxOpenConns-MysqlForegroundConns {
		t.Fatalf("WriteSlotMax = %d, want %d", got, MysqlMaxOpenConns-MysqlForegroundConns)
	}
}
