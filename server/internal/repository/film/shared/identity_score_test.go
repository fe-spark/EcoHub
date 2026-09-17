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

func TestCompatibleWorkShape_StructuralSignals(t *testing.T) {
	serial := IdentityProfile{Remarks: "第287集"}
	serialPartial := IdentityProfile{Episodes: []model.MovieUrlInfo{{Episode: "第1集"}, {Episode: "第9集"}}}
	movie := IdentityProfile{Episodes: []model.MovieUrlInfo{{Episode: "正片", Link: "https://x/1.m3u8"}}}
	packed := IdentityProfile{Episodes: []model.MovieUrlInfo{
		{Episode: "第1-20集"},
		{Episode: "第21-40集"},
		{Episode: "第41-60集"},
		{Episode: "第61-80集"},
		{Episode: "第81-106集完结"},
	}}
	anime100 := IdentityProfile{Remarks: "第100集"}
	shortPacked := IdentityProfile{Remarks: "第81-106集完结"}
	complete := IdentityProfile{Remarks: "全集完结"}
	firstEp := IdentityProfile{Episodes: []model.MovieUrlInfo{{Episode: "第1集"}}}
	bundle := IdentityProfile{Episodes: []model.MovieUrlInfo{{Episode: "合全集", Link: "https://x/all.m3u8"}}}

	if CompatibleWorkShape(serial, movie) {
		t.Fatal("unnumbered single title must not attach to a long serial")
	}
	if CompatibleWorkShape(serial, bundle) {
		t.Fatal("unnumbered bundle must not attach to a long serial")
	}
	if CompatibleWorkShape(anime100, packed) {
		t.Fatal("packed range ending far from 第100集 must not attach to the serial")
	}
	if !CompatibleWorkShape(shortPacked, packed) {
		t.Fatal("packed short drama must stay compatible with the packed master")
	}
	if !CompatibleWorkShape(serial, serialPartial) {
		t.Fatal("partial serial episodes must still attach to the same serial")
	}
	if !CompatibleWorkShape(complete, firstEp) {
		t.Fatal("第1集 must remain compatible with a finished short so category can still bind")
	}
}

func TestPickUniqueIdentityMid_DoubanMismatchDoesNotBindSingle(t *testing.T) {
	slave := IdentityProfile{
		DbID: 27056187, Name: "完美世界", RootPid: 9, CName: "剧情片", Year: 2021,
		Episodes: []model.MovieUrlInfo{{Episode: "正片", Link: "https://ly/movie.m3u8"}},
	}
	anime := IdentityProfile{DbID: 35312003, Name: "完美世界", RootPid: 20, CName: "中国动漫", Year: 2021, Remarks: "第287集"}
	got := PickUniqueIdentityMid(slave, map[int64]IdentityProfile{32115: anime})
	if got != 0 {
		t.Fatalf("different douban ids must not bind even when the title is unique, got %d", got)
	}
}

func TestPickUniqueIdentityMid_YearMismatchDoesNotBindWhenNoDouban(t *testing.T) {
	slave := IdentityProfile{Name: "完美世界", RootPid: 9, CName: "爱情片", Year: 2018}
	anime := IdentityProfile{Name: "完美世界", RootPid: 20, CName: "中国动漫", Year: 2021, Remarks: "第287集"}
	got := PickUniqueIdentityMid(slave, map[int64]IdentityProfile{32115: anime})
	if got != 0 {
		t.Fatalf("year 2018 vs 2021 with no douban must not bind the unique title, got %d", got)
	}
}

func TestPickUniqueIdentityMid_DoubanMatchIgnoresYearDrift(t *testing.T) {
	slave := IdentityProfile{DbID: 35312003, Name: "完美世界", RootPid: 20, CName: "动漫", Year: 2020}
	anime := IdentityProfile{DbID: 35312003, Name: "完美世界", RootPid: 20, CName: "中国动漫", Year: 2021, Remarks: "第287集"}
	got := PickUniqueIdentityMid(slave, map[int64]IdentityProfile{32115: anime})
	if got != 32115 {
		t.Fatalf("same douban must still bind when year is off by 1+, got %d", got)
	}
}

func TestCompatibleIdentity_EmptyMasterFieldsAreSkipped(t *testing.T) {
	slave := IdentityProfile{DbID: 27056187, Year: 2021, Director: "柴山健次"}
	master := IdentityProfile{Name: "完美世界", RootPid: 20, Remarks: "第287集"}
	if !CompatibleIdentity(master, slave) {
		t.Fatal("empty master douban/year/director must not count as a conflict")
	}
}

func TestCompatibleIdentity_DirectorMismatchRejectsWhenBothFilled(t *testing.T) {
	slave := IdentityProfile{Name: "完美世界", Director: "柴山健次"}
	master := IdentityProfile{Name: "完美世界", Director: "袁洁,自在天", Remarks: "第287集"}
	if CompatibleIdentity(master, slave) {
		t.Fatal("both sides have directors and they do not overlap, must reject")
	}
	if !CompatibleIdentity(IdentityProfile{Director: ""}, slave) {
		t.Fatal("empty master director must skip")
	}
	if !CompatibleIdentity(master, IdentityProfile{Director: "袁洁"}) {
		t.Fatal("overlapping director token must pass")
	}
}

func TestCompatibleIdentity_TagsDoNotReject(t *testing.T) {
	if !CompatibleIdentity(IdentityProfile{ClassTag: "动画,奇幻"}, IdentityProfile{ClassTag: "爱情"}) {
		t.Fatal("tags are too noisy to veto a bind")
	}
}

func TestPickUniqueIdentityMid_UnnumberedSingleDoesNotBindSerial(t *testing.T) {
	slave := IdentityProfile{
		Name:     "完美世界",
		RootPid:  9,
		CName:    "电影",
		Episodes: []model.MovieUrlInfo{{Episode: "正片", Link: "https://ly/movie.m3u8"}},
	}
	anime := IdentityProfile{Name: "完美世界", RootPid: 20, CName: "中国动漫", Year: 2021, Remarks: "第287集"}
	got := PickUniqueIdentityMid(slave, map[int64]IdentityProfile{32115: anime})
	if got != 0 {
		t.Fatalf("single-file title must not bind the 287-ep serial just because it is the only name hit, got %d", got)
	}
}

func TestPickUniqueIdentityMid_PackedShortBindsEvenWithAnimeTags(t *testing.T) {
	slave := IdentityProfile{
		Name:     "牧神记",
		RootPid:  20,
		CName:    "动漫",
		ClassTag: "玄幻,热血,战斗",
		Episodes: []model.MovieUrlInfo{
			{Episode: "第1-20集"},
			{Episode: "第21-40集"},
			{Episode: "第41-60集"},
			{Episode: "第61-80集"},
			{Episode: "第81-106集完结"},
		},
	}
	anime := IdentityProfile{Name: "牧神记", RootPid: 20, CName: "中国动漫", ClassTag: "玄幻,热血,战斗", Remarks: "第100集"}
	short := IdentityProfile{Name: "牧神记", RootPid: 34, CName: "反转爽剧", Remarks: "第81-106集完结"}
	got := PickUniqueIdentityMid(slave, map[int64]IdentityProfile{67651: anime, 144250: short})
	if got != 144250 {
		t.Fatalf("packed 106-ep short must bind 短剧 even when labeled 动漫 with overlapping tags, got %d", got)
	}
}

func TestFilterPlayGroupsByWorkShape(t *testing.T) {
	master := IdentityProfile{Remarks: "第100集"}
	groups := []model.PlayLinkVo{
		{Name: "逐集", LinkList: []model.MovieUrlInfo{{Episode: "第1集"}, {Episode: "第100集"}}},
		{Name: "打包", LinkList: []model.MovieUrlInfo{{Episode: "第1-20集"}, {Episode: "第81-106集完结"}}},
		{Name: "单条", LinkList: []model.MovieUrlInfo{{Episode: "正片"}}},
	}
	got := FilterPlayGroupsByWorkShape(master, groups)
	if len(got) != 1 || got[0].Name != "逐集" {
		t.Fatalf("serial film must keep episode-by-episode sources only, got %+v", got)
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

func TestCompatibleWorkShape_MoviesWithResolutionStayCompatible(t *testing.T) {
	master1080 := IdentityProfile{Remarks: "1080P"}
	master4k60 := IdentityProfile{Remarks: "4K 60帧"}
	masterFeature := IdentityProfile{Episodes: []model.MovieUrlInfo{{Episode: "正片", Link: "https://x/1.m3u8"}}}
	slaveFeature := IdentityProfile{Episodes: []model.MovieUrlInfo{{Episode: "正片", Link: "https://y/1.m3u8"}}}
	slave1080 := IdentityProfile{Episodes: []model.MovieUrlInfo{{Episode: "1080P", Link: "https://y/1080.m3u8"}}}

	if !CompatibleWorkShape(master1080, slaveFeature) {
		t.Fatal("1080P movie remarks must remain compatible with 正片 slave")
	}
	if !CompatibleWorkShape(master4k60, slaveFeature) {
		t.Fatal("4K 60帧 movie remarks must remain compatible with 正片 slave")
	}
	if !CompatibleWorkShape(masterFeature, slave1080) {
		t.Fatal("正片 master must remain compatible with 1080P slave link")
	}
}

func TestCompatibleWorkShape_SerialWithTotalEpisodesStaysCompatible(t *testing.T) {
	masterWithTotal := IdentityProfile{Remarks: "更新至10集/共30集"}
	slave10 := IdentityProfile{Episodes: []model.MovieUrlInfo{{Episode: "第1集"}, {Episode: "第10集"}}}
	if !CompatibleWorkShape(masterWithTotal, slave10) {
		t.Fatal("更新至10集/共30集 must remain compatible with 第10集 slave")
	}

	master15 := IdentityProfile{Remarks: "更新至15集"}
	slave12 := IdentityProfile{Episodes: []model.MovieUrlInfo{{Episode: "第1集"}, {Episode: "第12集"}}}
	if !CompatibleWorkShape(master15, slave12) {
		t.Fatal("serial lagging by a few episodes must remain compatible")
	}
}

func TestCompatibleIdentity_DirectorFormatTolerance(t *testing.T) {
	masterDot := IdentityProfile{Director: "詹姆斯·卡梅隆"}
	slaveNoDot := IdentityProfile{Director: "詹姆斯卡梅隆"}
	if !CompatibleIdentity(masterDot, slaveNoDot) {
		t.Fatal("director with middot should match director without middot")
	}

	masterPrefix := IdentityProfile{Director: "导演：张艺谋"}
	slavePlain := IdentityProfile{Director: "张艺谋"}
	if !CompatibleIdentity(masterPrefix, slavePlain) {
		t.Fatal("director with prefix should match plain director name")
	}
}

