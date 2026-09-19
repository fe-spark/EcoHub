package service

import (
	"testing"
)

func TestFilterExcludeSid(t *testing.T) {
	list := []LiveRelateFilmVO{
		{Id: "100", SourceMid: "100", Name: "Movie 1"},
		{Id: "101", SourceMid: "101", Name: "Movie 2"},
		{Id: "102", SourceMid: "102", Name: "Movie 3"},
	}

	filtered := filterExcludeSid(list, 101)
	if len(filtered) != 2 {
		t.Fatalf("expected 2 items, got %d", len(filtered))
	}
	if filtered[0].Id != "100" || filtered[1].Id != "102" {
		t.Fatalf("unexpected items: %+v", filtered)
	}

	filteredEmpty := filterExcludeSid(nil, 101)
	if len(filteredEmpty) != 0 {
		t.Fatalf("expected empty, got %d", len(filteredEmpty))
	}
}
