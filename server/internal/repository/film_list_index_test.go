package repository

import (
	"testing"

	"server/internal/infra/db"
)

func TestEnsureFilmDeletedIndexes_Guards(t *testing.T) {
	origMdb := db.Mdb
	t.Cleanup(func() { db.Mdb = origMdb })

	db.Mdb = nil
	if err := EnsureFilmCategoryDeletedIndex(); err != nil {
		t.Fatalf("nil db should return nil, got: %v", err)
	}
	if err := EnsureFilmSourceDeletedIndex(); err != nil {
		t.Fatalf("nil db should return nil, got: %v", err)
	}

	setupCategoryActiveTreeTestDB(t)
	if err := EnsureFilmCategoryDeletedIndex(); err != nil {
		t.Fatalf("sqlite guard should return nil, got: %v", err)
	}
	if err := EnsureFilmSourceDeletedIndex(); err != nil {
		t.Fatalf("sqlite guard should return nil, got: %v", err)
	}
}
