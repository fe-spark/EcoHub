package shared

import (
	"testing"

	"server/internal/model"
)

func TestScoreIdentity_DoubanOutweighsCategory(t *testing.T) {
	slave := IdentityProfile{DbID: 123, Name: "仙逆", RootPid: 34, Year: 2023}
	anime := IdentityProfile{DbID: 123, Name: "仙逆", RootPid: 20, Year: 2023}
	short := IdentityProfile{Name: "仙逆", RootPid: 34}

	if ScoreIdentity(slave, anime).Douban <= ScoreIdentity(slave, short).Category {
		t.Fatalf("douban match must outrank category-only")
	}
	got := PickUniqueIdentityMid(slave, map[int64]IdentityProfile{47014: anime, 126574: short})
	if got != 47014 {
		t.Fatalf("douban unique hit should bind anime, got %d", got)
	}
}

func TestPickUniqueIdentityMid_SerialRemarksOverridesWrongCategory(t *testing.T) {
	slave := IdentityProfile{
		Name:     "仙逆",
		RootPid:  34,
		CName:    "短剧",
		Year:     2023,
		Remarks:  "第158集",
		Episodes: []model.MovieUrlInfo{{Episode: "第01集"}, {Episode: "第158集"}},
	}
	anime := IdentityProfile{Name: "仙逆", RootPid: 20, CName: "中国动漫", Year: 2023, Remarks: "第158集"}
	short := IdentityProfile{Name: "仙逆", RootPid: 34, CName: "古装仙侠", Remarks: "全集完结"}

	got := PickUniqueIdentityMid(slave, map[int64]IdentityProfile{47014: anime, 126574: short})
	if got != 47014 {
		t.Fatalf("year+serial remarks should bind 动漫仙逆 even when slave is labeled 短剧, got %d", got)
	}
}

func TestPickUniqueIdentityMid_CompleteRemarksBindsShortDrama(t *testing.T) {
	slave := IdentityProfile{
		Name:     "仙逆",
		RootPid:  20,
		CName:    "动漫",
		Year:     2023,
		Episodes: []model.MovieUrlInfo{{Episode: "合全集"}},
	}
	anime := IdentityProfile{Name: "仙逆", RootPid: 20, CName: "中国动漫", Year: 2023, Remarks: "第158集"}
	short := IdentityProfile{Name: "仙逆", RootPid: 34, CName: "古装仙侠", Remarks: "全集完结"}

	got := PickUniqueIdentityMid(slave, map[int64]IdentityProfile{47014: anime, 126574: short})
	if got != 126574 {
		t.Fatalf("complete-collection label should bind 短剧仙逆 even when slave is labeled 动漫, got %d", got)
	}
}

func TestPickUniqueIdentityMid_CategoryBindsWhenConfirmTied(t *testing.T) {
	slave := IdentityProfile{Name: "仙逆", RootPid: 20, CName: "动漫"}
	anime := IdentityProfile{Name: "仙逆", RootPid: 20, CName: "中国动漫"}
	short := IdentityProfile{Name: "仙逆", RootPid: 34, CName: "古装仙侠"}

	got := PickUniqueIdentityMid(slave, map[int64]IdentityProfile{47014: anime, 126574: short})
	if got != 47014 {
		t.Fatalf("correct 动漫 label with no extra confirm should still bind anime, got %d", got)
	}
}

func TestPickUniqueIdentityMid_FirstEpisodeShortDramaStaysShort(t *testing.T) {
	slave := IdentityProfile{
		Name:     "仙逆",
		RootPid:  34,
		CName:    "短剧",
		Year:     2023,
		Episodes: []model.MovieUrlInfo{{Episode: "第1集"}},
	}
	anime := IdentityProfile{Name: "仙逆", RootPid: 20, CName: "中国动漫", Year: 2023, Remarks: "第158集"}
	short := IdentityProfile{Name: "仙逆", RootPid: 34, CName: "古装仙侠", Remarks: "全集完结"}

	got := PickUniqueIdentityMid(slave, map[int64]IdentityProfile{47014: anime, 126574: short})
	if got != 126574 {
		t.Fatalf("第1集 short drama must not bind 158-ep anime just because both look serial, got %d", got)
	}
}

func TestPickUniqueIdentityMid_NoBindWhenTVCategoryAndNoConfirm(t *testing.T) {
	slave := IdentityProfile{
		Name:     "仙逆",
		RootPid:  1,
		CName:    "电视剧",
		Episodes: []model.MovieUrlInfo{{Episode: "第1集"}},
	}
	anime := IdentityProfile{Name: "仙逆", RootPid: 20, CName: "中国动漫"}
	short := IdentityProfile{Name: "仙逆", RootPid: 34, CName: "古装仙侠"}

	got := PickUniqueIdentityMid(slave, map[int64]IdentityProfile{47014: anime, 126574: short})
	if got != 0 {
		t.Fatalf("colliding titles with unrelated category and no confirm must not bind, got %d", got)
	}
}

func TestPickUniqueIdentityMid_SingleCandidateKeepsMislabeledBind(t *testing.T) {
	slave := IdentityProfile{Name: "一斩苍穹", RootPid: 1, CName: "电视剧"}
	anime := IdentityProfile{Name: "一斩苍穹", RootPid: 20, CName: "中国动漫"}

	got := PickUniqueIdentityMid(slave, map[int64]IdentityProfile{116429: anime})
	if got != 116429 {
		t.Fatalf("single candidate must still bind when slave category differs, got %d", got)
	}
}

func TestPickUniqueIdentityMid_ProductionXianNiReplay(t *testing.T) {
	anime := IdentityProfile{Name: "仙逆", RootPid: 20, CName: "中国动漫", Year: 2023, Remarks: "第158集"}
	short := IdentityProfile{Name: "仙逆", RootPid: 34, CName: "古装仙侠", Remarks: "全集完结"}
	cands := map[int64]IdentityProfile{47014: anime, 126574: short}

	cases := []struct {
		name   string
		slave  IdentityProfile
		want   int64
		reason string
	}{
		{
			name: "HD(BF) 158集标成短剧",
			slave: IdentityProfile{
				Name: "仙逆", RootPid: 34, CName: "短剧", Year: 2023, Remarks: "第158集",
				Episodes: []model.MovieUrlInfo{{Episode: "第01集"}, {Episode: "第158集"}},
			},
			want:   47014,
			reason: "年份+连载备注应绑动漫",
		},
		{
			name: "HD(LY) 合全集标成动漫",
			slave: IdentityProfile{
				Name: "仙逆", RootPid: 20, CName: "动漫", Year: 2023,
				Episodes: []model.MovieUrlInfo{{Episode: "合全集"}},
			},
			want:   126574,
			reason: "完结形态应绑短剧",
		},
		{
			name: "速博等正确动漫158集",
			slave: IdentityProfile{
				Name: "仙逆", RootPid: 20, CName: "动漫", Year: 2023, Remarks: "第158集",
				Episodes: []model.MovieUrlInfo{{Episode: "第01集"}, {Episode: "第158集"}},
			},
			want:   47014,
			reason: "分类+年份+连载都应绑动漫",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := PickUniqueIdentityMid(tc.slave, cands)
			sa := ScoreIdentity(tc.slave, anime)
			ss := ScoreIdentity(tc.slave, short)
			t.Logf("动漫47014 total=%d confirm=%d (db=%d name=%d cat=%d tag=%d year=%d remarks=%d)",
				sa.Total, sa.Confirm(), sa.Douban, sa.Name, sa.Category, sa.Tag, sa.Year, sa.Remarks)
			t.Logf("短剧126574 total=%d confirm=%d (db=%d name=%d cat=%d tag=%d year=%d remarks=%d)",
				ss.Total, ss.Confirm(), ss.Douban, ss.Name, ss.Category, ss.Tag, ss.Year, ss.Remarks)
			if got != tc.want {
				t.Fatalf("%s: got mid=%d want %d", tc.reason, got, tc.want)
			}
		})
	}
}

func TestClassifyRemarkKind(t *testing.T) {
	serial := classifyRemarkKind(IdentityProfile{Remarks: "第158集"})
	if serial != remarkSerial {
		t.Fatalf("第158集 should be serial, got %d", serial)
	}
	complete := classifyRemarkKind(IdentityProfile{Episodes: []model.MovieUrlInfo{{Episode: "合全集"}}})
	if complete != remarkComplete {
		t.Fatalf("合全集 should be complete, got %d", complete)
	}
	if classifyRemarkKind(IdentityProfile{Episodes: []model.MovieUrlInfo{{Episode: "HD合集"}}}) != remarkUnknown {
		t.Fatalf("bare 合集 in a line name must not count as complete")
	}
	if ParseIdentityYear("2023") != 2023 || ParseIdentityYear("年份:2024年") != 2024 {
		t.Fatalf("ParseIdentityYear failed")
	}
}
