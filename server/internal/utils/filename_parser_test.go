package utils

import (
	"testing"
)

func TestParseMediaFilename_GoldenCases(t *testing.T) {
	tests := []struct {
		name      string
		relPath   string
		mediaType string
		wantTitle string
		wantYear  int64
		wantS     int
		wantE     int
		wantIsTV  bool
		wantHint  string
		wantTmdb  int64
		wantErr   bool
	}{
		{
			name:      "庆余年 S01E01",
			relPath:   "庆余年.2019.S01E01.1080p.mkv",
			mediaType: "tv",
			wantTitle: "庆余年",
			wantYear:  2019,
			wantS:     1,
			wantE:     1,
			wantIsTV:  true,
		},
		{
			name:      "前缀站点标签",
			relPath:   "[电影天堂]庆余年.2019.S01E01.mkv",
			mediaType: "tv",
			wantTitle: "庆余年",
			wantYear:  2019,
			wantS:     1,
			wantE:     1,
			wantIsTV:  true,
		},
		{
			name:      "祖父目录含年份与片名，父级Season目录",
			relPath:   "庆余年 (2019)/Season 1/E01.mkv",
			mediaType: "tv",
			wantTitle: "庆余年",
			wantYear:  2019,
			wantS:     1,
			wantE:     1,
			wantIsTV:  true,
		},
		{
			name:      "祖父目录片名，父级S02，中文集数",
			relPath:   "庆余年 (2019)/S02/第03集.mkv",
			mediaType: "tv",
			wantTitle: "庆余年",
			wantYear:  2019,
			wantS:     2,
			wantE:     3,
			wantIsTV:  true,
		},
		{
			name:      "绝命毒师英文名多季与清晰度",
			relPath:   "Breaking.Bad.S05E16.720p.BluRay.x264.mkv",
			mediaType: "tv",
			wantTitle: "Breaking Bad",
			wantYear:  0,
			wantS:     5,
			wantE:     16,
			wantIsTV:  true,
		},
		{
			name:      "父目录含年，子级Season与纯集数",
			relPath:   "父目录2019/Season 01/01.mp4",
			mediaType: "tv",
			wantTitle: "父目录",
			wantYear:  2019,
			wantS:     1,
			wantE:     1,
			wantIsTV:  true,
		},
		{
			name:      "沙丘 电影",
			relPath:   "Dune.2021.2160p.WEB-DL.mkv",
			mediaType: "movie",
			wantTitle: "Dune",
			wantYear:  2021,
			wantS:     0,
			wantE:     0,
			wantIsTV:  false,
		},
		{
			name:      "沙丘2 电影",
			relPath:   "沙丘2.2024.1080p.mp4",
			mediaType: "movie",
			wantTitle: "沙丘2",
			wantYear:  2024,
			wantS:     0,
			wantE:     0,
			wantIsTV:  false,
		},
		{
			name:      "鬼灭之刃 中文连词清洗",
			relPath:   "鬼灭之刃.无限列车篇.2020.mkv",
			mediaType: "movie",
			wantTitle: "鬼灭之刃无限列车篇",
			wantYear:  2020,
			wantS:     0,
			wantE:     0,
			wantIsTV:  false,
		},
		{
			name:      "特殊符号 SPY×FAMILY",
			relPath:   "SPY×FAMILY.S01E01.mkv",
			mediaType: "tv",
			wantTitle: "SPY×FAMILY",
			wantYear:  0,
			wantS:     1,
			wantE:     1,
			wantIsTV:  true,
		},
		{
			name:      "拼接盘 E01-E03",
			relPath:   "Show.Name.E01-E03.mkv",
			mediaType: "tv",
			wantTitle: "Show Name",
			wantYear:  0,
			wantS:     1,
			wantE:     1,
			wantIsTV:  true,
			wantHint:  "range_disc",
		},
		{
			name:      "电影分盘 CD1",
			relPath:   "Movie.2010.CD1.mkv",
			mediaType: "movie",
			wantTitle: "Movie",
			wantYear:  2010,
			wantS:     0,
			wantE:     0,
			wantIsTV:  false,
			wantHint:  "disc_split",
		},
		{
			name:      "合集噪音目录下的文件",
			relPath:   "合集/Complete/xx.S01E02.mkv",
			mediaType: "tv",
			wantTitle: "xx",
			wantYear:  0,
			wantS:     1,
			wantE:     2,
			wantIsTV:  true,
		},
		{
			name:      "纯数字集数回溯单层父目录",
			relPath:   "我的剧/01.mp4",
			mediaType: "tv",
			wantTitle: "我的剧",
			wantYear:  0,
			wantS:     1,
			wantE:     1,
			wantIsTV:  true,
		},
		{
			name:      "仅技术规格导致空标题",
			relPath:   "1080p.mkv",
			mediaType: "movie",
			wantErr:   true,
		},
		{
			name:      "非视频文件",
			relPath:   "test.txt",
			mediaType: "movie",
			wantErr:   true,
		},
		{
			name:      "ISO 镜像标记不可直接播放容器",
			relPath:   "Avatar.2009.iso",
			mediaType: "movie",
			wantTitle: "Avatar",
			wantYear:  2009,
			wantHint:  "unplayable_container",
		},
		{
			name:      "发布组标签与 MOVIE(BD 1080P) 噪音",
			relPath:   "FATE剧场版/命运之夜/[LowPower-Raws] Fate Grand Order -終局特異点- - MOVIE (BD 1080P x264 FLACx3).mkv",
			mediaType: "movie",
			wantTitle: "命运之夜",
			wantYear:  0,
		},
		{
			name:      "父目录中文名优于英文压制文件名",
			relPath:   "青之驱魔师剧场版/[J.X&MGRT]Blue_Exorcist_The_Movie[sc&tc&jp][BDrip][1080P_Hi10_FLAC].mkv",
			mediaType: "movie",
			wantTitle: "青之驱魔师剧场版",
			wantYear:  0,
		},
		{
			name:      "文件名内嵌 tmdb id 与中英双字",
			relPath:   "机动战士高达闪光的哈萨维喀耳刻的魔女(2026)-1080p.Amazon.WEB-DL.{tmdb-910850}.mkv",
			mediaType: "movie",
			wantTitle: "机动战士高达闪光的哈萨维喀耳刻的魔女",
			wantYear:  2026,
			wantTmdb:  910850,
		},
		{
			name:      "中英双字与父目录",
			relPath:   "正义联盟：无限地球危机（上）/正义联盟：无限地球危机(上).2024.1080P.中英双字.mp4",
			mediaType: "movie",
			wantTitle: "正义联盟：无限地球危机（上）",
			wantYear:  2024,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseMediaFilename(tt.relPath, tt.mediaType)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseMediaFilename() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got.Title != tt.wantTitle {
				t.Errorf("Title = %q, want %q", got.Title, tt.wantTitle)
			}
			if tt.wantTmdb > 0 && got.TmdbID != tt.wantTmdb {
				t.Errorf("TmdbID = %d, want %d", got.TmdbID, tt.wantTmdb)
			}
			if got.Year != tt.wantYear {
				t.Errorf("Year = %d, want %d", got.Year, tt.wantYear)
			}
			if got.Season != tt.wantS {
				t.Errorf("Season = %d, want %d", got.Season, tt.wantS)
			}
			if got.Episode != tt.wantE {
				t.Errorf("Episode = %d, want %d", got.Episode, tt.wantE)
			}
			if got.IsTV != tt.wantIsTV {
				t.Errorf("IsTV = %v, want %v", got.IsTV, tt.wantIsTV)
			}
			if tt.wantHint != "" && got.Hint != tt.wantHint {
				t.Errorf("Hint = %q, want %q", got.Hint, tt.wantHint)
			}
		})
	}
}
