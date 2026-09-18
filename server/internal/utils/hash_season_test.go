package utils

import (
	"strings"
	"testing"
)

func TestBuildCollectionDbIdentity_RequiresTitle(t *testing.T) {
	threeD := BuildCollectionDbIdentity(36117912, "沧元图3D动漫版")
	donghua := BuildCollectionDbIdentity(36117912, "沧元图 动态漫画")
	if threeD == "" || donghua == "" {
		t.Fatal("douban identity with a title must not be empty")
	}
	if threeD == donghua {
		t.Fatalf("same douban different titles must not share identity, got %q", threeD)
	}
	if threeD == "dbid_36117912" || !strings.Contains(threeD, "沧元图3d动漫版") {
		t.Fatalf("douban identity must include the title, got %q", threeD)
	}
	if BuildCollectionDbIdentity(36117912, "沧元图3D动漫版更新至95集") != threeD {
		t.Fatal("noisy 更新至 title must share the clean douban identity")
	}
	if BuildCollectionDbIdentity(36117912, "沧元图动态漫画") != donghua {
		t.Fatal("space-only 动态漫画 variants must share identity")
	}
	if got := BuildCollectionDbIdentity(36117912, ""); got != "dbid_36117912" {
		t.Fatalf("empty title should fall back to bare douban id, got %q", got)
	}
	if BuildCollectionDbIdentity(0, "沧元图3D动漫版") != "" {
		t.Fatal("missing douban id must not build a douban identity")
	}
}

func TestNormalizeIdentityTitle_StripsProgressNotVersion(t *testing.T) {
	if NormalizeIdentityTitle("沧元图 动态漫画") != NormalizeIdentityTitle("沧元图动态漫画") {
		t.Fatal("space-only title difference must match")
	}
	if NormalizeIdentityTitle("沧元图3D动漫版更新至95集") != NormalizeIdentityTitle("沧元图3D动漫版") {
		t.Fatal("更新至 suffix must not change identity title")
	}
	if NormalizeIdentityTitle("沧元图3D动漫版第95集") != NormalizeIdentityTitle("沧元图3D动漫版") {
		t.Fatal("trailing 第N集 must not change identity title")
	}
	if NormalizeIdentityTitle("沧元图3D动漫版国语") != NormalizeIdentityTitle("沧元图3D动漫版") {
		t.Fatal("language suffix must not change identity title")
	}
	if NormalizeIdentityTitle("沧元图3D动漫版國語") != NormalizeIdentityTitle("沧元图3D动漫版") {
		t.Fatal("traditional 國語 must strip after simplification")
	}
	if NormalizeIdentityTitle("沧元图3D动漫版普通话") != NormalizeIdentityTitle("沧元图3D动漫版") {
		t.Fatal("普通话 suffix must not change identity title")
	}
	if NormalizeIdentityTitle("沧元图3d动漫版") != NormalizeIdentityTitle("沧元图3D动漫版") {
		t.Fatal("3d and 3D must match")
	}
	if NormalizeIdentityTitle("沧元图3D动漫版1080P") != NormalizeIdentityTitle("沧元图3D动漫版") {
		t.Fatal("quality suffix must not change identity title")
	}
	if NormalizeIdentityTitle("2012") == "" {
		t.Fatal("year-only title must not strip to empty")
	}
	if NormalizeIdentityTitle("沧元图3D动漫版") == NormalizeIdentityTitle("沧元图 动态漫画") {
		t.Fatal("3D and 动态漫画 must stay different works")
	}
	if NormalizeIdentityTitle("某某剧场版") == NormalizeIdentityTitle("某某") {
		t.Fatal("剧场版 must not be stripped for identity titles")
	}
}

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
