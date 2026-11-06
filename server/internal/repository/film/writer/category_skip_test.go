package writer

import (
	"errors"
	"testing"
)

func TestCategorySkipKind(t *testing.T) {
	if categorySkipKind(nil) != skipKindNone {
		t.Fatal("nil")
	}
	if categorySkipKind(ErrCategoryNotMapped) != skipKindUnmapped {
		t.Fatal("unmapped")
	}
	if categorySkipKind(ErrCategoryDisabled) != skipKindDisabled {
		t.Fatal("disabled")
	}
	if categorySkipKind(errors.New("影片分类已停用")) != skipKindDisabled {
		t.Fatal("disabled text")
	}
}

func TestCountCategorySkips(t *testing.T) {
	unmapped, disabled := countCategorySkips([]peerPageItem{
		{skipKind: skipKindUnmapped},
		{skipKind: skipKindDisabled},
		{disabled: true},
		{skipKind: skipKindDisabled, disabled: true},
	})
	if unmapped != 1 || disabled != 3 {
		t.Fatalf("unmapped=%d disabled=%d", unmapped, disabled)
	}
}
