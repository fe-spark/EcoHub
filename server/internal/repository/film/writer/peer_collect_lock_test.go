package writer

import (
	"testing"

	"server/internal/model"
)

func TestSQLFilmLockNames_StableSorted(t *testing.T) {
	src := &model.FilmSource{Id: "src-a"}
	d := model.MovieDetail{Name: "斗破苍穹", RawPid: 1}
	a := sqlFilmLockNames(src, d, 9)
	b := sqlFilmLockNames(src, d, 9)
	if len(a) == 0 {
		t.Fatal("expected lock names")
	}
	if len(a) != len(b) {
		t.Fatalf("len %d != %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("lock order unstable at %d: %q vs %q", i, a[i], b[i])
		}
		if i > 0 && a[i-1] > a[i] {
			t.Fatalf("lock names not sorted: %v", a)
		}
	}
}
