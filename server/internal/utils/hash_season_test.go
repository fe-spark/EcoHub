package utils

import "testing"

func TestSeasonHashCollapse(t *testing.T) {
	cases := []string{"烬九州：第二季", "烬九州：第五季", "烬九州第四季", "烬九州第二季"}
	hashes := map[string]string{}
	for _, c := range cases {
		n := NormalizeCollectionTitle(c)
		h := GenerateHashKey(n)
		t.Logf("%q -> norm=%q hash=%s", c, n, h)
		hashes[c] = h
	}
	if hashes["烬九州：第二季"] == hashes["烬九州：第五季"] {
		t.Errorf("第二季 and 第五季 collapsed to same hash %s", hashes["烬九州：第二季"])
	}
}

func TestDualAudioAndSegments(t *testing.T) {
	pairs := [][2]string{
		{"1991! 神秘学对策部英语", "1991! 神秘学对策部国语"},
		{"某某剧场版", "某某"},
	}
	for _, p := range pairs {
		n1, n2 := NormalizeCollectionTitle(p[0]), NormalizeCollectionTitle(p[1])
		h1, h2 := GenerateHashKey(n1), GenerateHashKey(n2)
		t.Logf("%q -> %q hash=%s", p[0], n1, h1)
		t.Logf("%q -> %q hash=%s", p[1], n2, h2)
		if h1 == h2 {
			t.Errorf("COLLAPSE %q and %q -> same hash %s (norm %q / %q)", p[0], p[1], h1, n1, n2)
		}
	}
}
