package service

import (
	"fmt"
	"log"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	filmsnapshot "server/internal/repository/film/snapshot"
)

func storeFilmPlayInfoCache(cacheKey, payload string, ttl time.Duration, gen int64) {
	if db.Rdb == nil {
		return
	}
	if filmsnapshot.PlayInfoGeneration() != gen {
		return
	}
	_ = db.Rdb.Set(db.Cxt, cacheKey, payload, ttl).Err()
	if filmsnapshot.PlayInfoGeneration() != gen {
		_ = db.Rdb.Del(db.Cxt, cacheKey).Err()
	}
}

func skipBadPlaylist(row model.FilmSourcePlaylist, err error) {
	log.Printf("[Play] 播放线路解析失败，已跳过 mid=%d source=%s group=%d err=%v", row.Mid, row.SourceId, row.GroupIndex, err)
}

// playlistsAllBroken 这部影片的线路全部无法解析时返回错误。没有线路，或还有能解析的线路，返回 nil。
func playlistsAllBroken(mid int64, rows, failed int) error {
	if rows == 0 || failed != rows {
		return nil
	}
	log.Printf("[Play] 播放线路全部解析失败 mid=%d", mid)
	return fmt.Errorf("播放线路全部解析失败 mid=%d", mid)
}

// brokenPlaylistMids 找出线路全部解析失败的影片。调用方跳过这些影片，不让整批失败。
func brokenPlaylistMids(seen, failed map[int64]int) map[int64]struct{} {
	var broken map[int64]struct{}
	for mid, n := range seen {
		if n == 0 || failed[mid] != n {
			continue
		}
		if broken == nil {
			broken = make(map[int64]struct{})
		}
		broken[mid] = struct{}{}
		log.Printf("[Play] 播放线路全部解析失败，已跳过影片 mid=%d", mid)
	}
	return broken
}
