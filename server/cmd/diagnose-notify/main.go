// diagnose-notify 诊断「连续批量采集更新列表反复出现相同影片」问题。
//
// 用法（在 server 目录，需已配置 .env 可连 MySQL）：
//
//	go run ./cmd/diagnose-notify              # 分析当天变更批次 + 对反复 mid 做库/源站对比
//	go run ./cmd/diagnose-notify -mids 98880,98879
//	go run ./cmd/diagnose-notify -hours 24 -limit 20 -flap
//
// 输出：
//  1. 当天 notify 批次统计与「跨批次重复 mid」
//  2. 对目标 mid：主站 DB vs 源站 live 结构对比；附属站 playlist 对比
//  3. 可选 -flap：源站连拉两次，检测结构抖动
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	filmrepo "server/internal/repository/film"
	"server/internal/spider"
	"server/internal/utils"

	"gorm.io/gorm"
)

func main() {
	hours := flag.Int("hours", 24, "分析最近 N 小时的变更批次")
	midsFlag := flag.String("mids", "", "指定 mid 列表（逗号分隔）；空则自动取跨批次重复 mid")
	limit := flag.Int("limit", 15, "最多诊断多少个 mid")
	flap := flag.Bool("flap", true, "对源站连拉两次检测结构抖动")
	flapWait := flag.Duration("flap-wait", 2*time.Second, "两次源站拉取间隔")
	skipLive := flag.Bool("skip-live", false, "只做批次/库内分析，不请求源站")
	sourcesFlag := flag.String("sources", "", "只诊断这些源（名称子串，逗号分隔，如 魔都,HD(FF)）；空=全部启用源")
	onlyWithData := flag.Bool("only-with-data", true, "附属站无 playlist/映射时跳过 live（加速）")
	flag.Parse()

	// config.init 会 Load .env；仅需 MySQL
	if err := db.InitMysql(); err != nil {
		log.Fatalf("MySQL 连接失败: %v（请在 server/ 下配置 .env）", err)
	}

	fmt.Printf("=== diagnose-notify ===\n")
	fmt.Printf("version=%s  window=%dh  time=%s\n\n",
		config.Version, *hours, time.Now().Format(time.DateTime))

	master, slaves := loadSources()
	if master == nil {
		log.Fatal("未找到启用的主站（film_sources grade=0 state=1）")
	}
	if filt := parseSourceFilter(*sourcesFlag); len(filt) > 0 {
		if master != nil && !sourceMatch(master.Name, filt) {
			log.Printf("警告: 主站 %s 不在 -sources 过滤中，仍保留主站用于结构对比", master.Name)
		}
		var filtered []model.FilmSource
		for _, s := range slaves {
			if sourceMatch(s.Name, filt) {
				filtered = append(filtered, s)
			}
		}
		slaves = filtered
	}
	fmt.Printf("[源] 主站: %s id=%s grade=%d\n", master.Name, master.Id, master.Grade)
	for _, s := range slaves {
		fmt.Printf("[源] 附属: %s id=%s\n", s.Name, s.Id)
	}
	if len(slaves) == 0 {
		fmt.Println("[源] （无附属站纳入诊断；可用 -sources 指定或检查 state=1）")
	}
	fmt.Println()

	// --- Phase 1: 批次分析 ---
	batches, err := loadBatchesSince(time.Now().Add(-time.Duration(*hours) * time.Hour))
	if err != nil {
		log.Fatalf("读取变更批次失败: %v", err)
	}
	fmt.Printf("=== 1. 最近 %dh 变更批次 (%d 个) ===\n", *hours, len(batches))
	midBatchCount := map[int64]int{}
	midNames := map[int64]string{}
	for _, b := range batches {
		mids, _ := loadBatchMids(b.ID)
		fmt.Printf("  batch=%s created=%s total=%d mids=%d\n",
			b.ID, b.CreatedAt.Format(time.DateTime), b.Total, len(mids))
		for _, mid := range mids {
			midBatchCount[mid]++
		}
	}
	// 补片名
	allMids := make([]int64, 0, len(midBatchCount))
	for mid := range midBatchCount {
		allMids = append(allMids, mid)
	}
	names := loadFilmNames(allMids)
	for mid, n := range names {
		midNames[mid] = n
	}

	type rep struct {
		mid   int64
		count int
		name  string
	}
	var repeated []rep
	for mid, c := range midBatchCount {
		if c >= 2 {
			repeated = append(repeated, rep{mid: mid, count: c, name: midNames[mid]})
		}
	}
	sort.Slice(repeated, func(i, j int) bool {
		if repeated[i].count != repeated[j].count {
			return repeated[i].count > repeated[j].count
		}
		return repeated[i].mid < repeated[j].mid
	})
	fmt.Printf("\n跨批次重复 mid（出现 ≥2 次）: %d 个\n", len(repeated))
	show := len(repeated)
	if show > 30 {
		show = 30
	}
	for i := 0; i < show; i++ {
		r := repeated[i]
		fmt.Printf("  mid=%d count=%d name=%q\n", r.mid, r.count, r.name)
	}
	if len(repeated) == 0 && len(batches) > 0 {
		fmt.Println("  （无跨批次重复：要么只采过 1 次，要么重复片未再进列表）")
	}
	fmt.Println()

	// --- Phase 2: 选定诊断 mid ---
	targets := parseMids(*midsFlag)
	if len(targets) == 0 {
		for _, r := range repeated {
			targets = append(targets, r.mid)
			if len(targets) >= *limit {
				break
			}
		}
	}
	if len(targets) == 0 {
		// 回退：最新批次前 N 个
		if len(batches) > 0 {
			mids, _ := loadBatchMids(batches[0].ID)
			for i, mid := range mids {
				if i >= *limit {
					break
				}
				targets = append(targets, mid)
			}
			fmt.Printf("无重复 mid，改诊断最新批次前 %d 个\n\n", len(targets))
		}
	}
	if len(targets) > *limit {
		targets = targets[:*limit]
	}
	if len(targets) == 0 {
		fmt.Println("没有可诊断的 mid。可手动: go run ./cmd/diagnose-notify -mids 98880,98879")
		return
	}

	fmt.Printf("=== 2. 逐 mid 诊断 (n=%d) ===\n", len(targets))
	spiderCore := &spider.JsonCollect{}

	var (
		masterNotifyN int
		slaveNotifyN  int
		flapN         int
		bizOnlyN      int
	)

	for _, mid := range targets {
		name := midNames[mid]
		if name == "" {
			name = loadFilmNames([]int64{mid})[mid]
		}
		fmt.Printf("\n---------- mid=%d %q (跨批次出现 %d 次) ----------\n",
			mid, name, midBatchCount[mid])

		// 2a 主站 DB 详情
		oldDetail, hasOld, err := loadMasterDetail(mid)
		if err != nil {
			fmt.Printf("  [主站DB] 读取失败: %v\n", err)
		} else if !hasOld {
			fmt.Printf("  [主站DB] 无 movie_detail_info 行\n")
		} else {
			fmt.Printf("  [主站DB] 结构:\n%s", filmrepo.FormatPlayStructureHuman(oldDetail))
		}

		// 2b 主站 live
		if !*skipLive && master != nil {
			sourceMid := resolveSourceMid(master.Id, mid)
			if sourceMid <= 0 {
				// 主站 mid 通常 = 源站 vod_id
				sourceMid = mid
			}
			live1, err := fetchDetail(spiderCore, master.Uri, sourceMid)
			if err != nil {
				fmt.Printf("  [主站API] 拉取失败 source_mid=%d: %v\n", sourceMid, err)
			} else {
				ex := filmrepo.ExplainMasterNotify(oldDetail, hasOld, live1)
				fmt.Printf("  [主站API] source_mid=%d name=%q\n", live1.Id, live1.Name)
				fmt.Printf("  [主站API] 结构:\n%s", filmrepo.FormatPlayStructureHuman(live1))
				fmt.Printf("  [主站判定] write=%v notify=%v | %s\n",
					ex.WouldWrite, ex.WouldNotify, ex.Reason)
				if ex.WouldNotify {
					masterNotifyN++
				} else if ex.WouldWrite {
					bizOnlyN++
				}
				if *flap {
					time.Sleep(*flapWait)
					live2, err2 := fetchDetail(spiderCore, master.Uri, sourceMid)
					if err2 != nil {
						fmt.Printf("  [主站抖动] 第二次拉取失败: %v\n", err2)
					} else {
						sameBiz := filmrepo.SameMasterBusiness(live1, live2)
						sameStr := filmrepo.SameMasterPlayStructure(live1, live2)
						fmt.Printf("  [主站抖动] 两次业务一致=%v 结构一致=%v\n", sameBiz, sameStr)
						if !sameStr {
							flapN++
							fmt.Printf("  [主站抖动] 第一次结构=%s\n", filmrepo.MasterPlayStructureSig(live1))
							fmt.Printf("  [主站抖动] 第二次结构=%s\n", filmrepo.MasterPlayStructureSig(live2))
							fmt.Printf("  [主站抖动] 第二次结构明细:\n%s", filmrepo.FormatPlayStructureHuman(live2))
						}
					}
				}
			}
		}

		// 2c 附属站 playlist
		for _, slave := range slaves {
			rows, err := loadSlavePlaylists(slave.Id, mid, oldDetail)
			if err != nil {
				fmt.Printf("  [附属DB %s] 读取失败: %v\n", slave.Name, err)
				continue
			}
			sourceMid := resolveSourceMid(slave.Id, mid)
			if len(rows) == 0 {
				if *onlyWithData && sourceMid <= 0 {
					continue // 无数据且无映射：静默跳过
				}
				fmt.Printf("  [附属DB %s] 无 playlist 行（未匹配或未采到）\n", slave.Name)
			} else {
				fmt.Printf("  [附属DB %s] playlist 组数=%d keys=%v\n",
					slave.Name, len(rows), uniqueKeys(rows))
			}

			if *skipLive {
				continue
			}
			if sourceMid <= 0 {
				// 无映射时按片名检索附属站（诊断兜底）
				if name != "" {
					if sid, err := searchSourceMidByName(spiderCore, slave.Uri, name); err == nil && sid > 0 {
						sourceMid = sid
						fmt.Printf("  [附属API %s] 无映射，按片名命中 source_mid=%d\n", slave.Name, sourceMid)
					}
				}
			}
			if sourceMid <= 0 {
				if len(rows) > 0 {
					fmt.Printf("  [附属API %s] 有 playlist 但无 source_mid，无法 live 对比\n", slave.Name)
				}
				continue
			}
			live, err := fetchDetail(spiderCore, slave.Uri, sourceMid)
			if err != nil {
				fmt.Printf("  [附属API %s] 拉取失败 source_mid=%d: %v\n", slave.Name, sourceMid, err)
				continue
			}
			incoming := filmrepo.BuildIncomingSlavePlaylists(slave.Id, live)
			// 按 movie_key 分组对比
			existingByKey := groupPlaylists(rows)
			incomingByKey := groupPlaylists(incoming)
			keys := mergeKeys(existingByKey, incomingByKey)
			anyNotify := false
			for _, key := range keys {
				diff := filmrepo.DiffSlavePlaylistGroups(key, existingByKey[key], incomingByKey[key])
				fmt.Printf("  [附属判定 %s] key=%s write=%v notify=%v first=%v | %s\n",
					slave.Name, shortKey(key), diff.WouldWrite, diff.NotifyWorthy, diff.FirstInsert, diff.Reason)
				if diff.NotifyWorthy {
					anyNotify = true
					fmt.Printf("    existing=%s\n    incoming=%s\n", diff.ExistingSig, diff.IncomingSig)
				}
			}
			if anyNotify {
				slaveNotifyN++
			}
			if *flap {
				time.Sleep(*flapWait)
				live2, err2 := fetchDetail(spiderCore, slave.Uri, sourceMid)
				if err2 != nil {
					fmt.Printf("  [附属抖动 %s] 第二次拉取失败: %v\n", slave.Name, err2)
				} else {
					g1 := groupPlaylists(filmrepo.BuildIncomingSlavePlaylists(slave.Id, live))
					g2 := groupPlaylists(filmrepo.BuildIncomingSlavePlaylists(slave.Id, live2))
					structSame := true
					linkOnly := false
					for _, key := range mergeKeys(g1, g2) {
						dd := filmrepo.DiffSlavePlaylistGroups(key, g1[key], g2[key])
						if dd.NotifyWorthy {
							structSame = false
							fmt.Printf("    抖动 key=%s: %s\n      a=%s\n      b=%s\n",
								shortKey(key), dd.Reason, dd.ExistingSig, dd.IncomingSig)
						} else if dd.WouldWrite {
							linkOnly = true
						}
					}
					fmt.Printf("  [附属抖动 %s] 两次结构一致=%v 仅链接差异=%v\n", slave.Name, structSame, linkOnly)
					if !structSame {
						flapN++
					}
				}
			}
		}
	}

	// --- Summary ---
	fmt.Printf("\n=== 3. 结论摘要 ===\n")
	fmt.Printf("诊断 mid 数: %d\n", len(targets))
	fmt.Printf("主站 live 会进更新列表: %d\n", masterNotifyN)
	fmt.Printf("主站仅业务噪声写库: %d\n", bizOnlyN)
	fmt.Printf("附属站 live 会进更新列表: %d\n", slaveNotifyN)
	fmt.Printf("源站结构抖动(两次拉取不一致): %d\n", flapN)
	fmt.Println()
	fmt.Println("判读建议:")
	fmt.Println("  · 主站 notify>0 且 flap>0 → 源站返回的线路/集数不稳定，导致反复进列表")
	fmt.Println("  · 主站 notify>0 且 flap=0 → DB 与源站结构已不一致（上次写入后源站又变，或写入路径改写了结构）")
	fmt.Println("  · 附属 notify>0 → 附属站 playlist 结构判定反复触发（匹配 key/集数标签）")
	fmt.Println("  · 两者 notify=0 但 TG 仍有片 → 看是否采的不是当前启用源，或 mid 来自其它批次窗口")
	fmt.Println("  · 跨批次重复很多但 live notify=0 → 问题可能是历史批次残留统计；请对应当天实际采集时间")
}

// ---------- data access ----------

func loadSources() (*model.FilmSource, []model.FilmSource) {
	var sources []model.FilmSource
	_ = db.Mdb.Where("state = ?", true).Find(&sources).Error
	var master *model.FilmSource
	var slaves []model.FilmSource
	for i := range sources {
		s := sources[i]
		if s.Grade == model.MasterCollect {
			cp := s
			master = &cp
		} else if s.Grade == model.SlaveCollect {
			slaves = append(slaves, s)
		}
	}
	return master, slaves
}

func loadBatchesSince(since time.Time) ([]model.NotifyChangeBatch, error) {
	var batches []model.NotifyChangeBatch
	err := db.Mdb.Where("created_at >= ?", since).
		Order("created_at DESC").
		Find(&batches).Error
	return batches, err
}

func loadBatchMids(batchID string) ([]int64, error) {
	var rows []model.NotifyChangeMid
	err := db.Mdb.Where("batch_id = ?", batchID).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	mids := make([]int64, 0, len(rows))
	for _, r := range rows {
		mids = append(mids, r.Mid)
	}
	sort.Slice(mids, func(i, j int) bool { return mids[i] < mids[j] })
	return mids, nil
}

func loadFilmNames(mids []int64) map[int64]string {
	out := make(map[int64]string)
	if len(mids) == 0 {
		return out
	}
	var rows []model.FilmIndex
	_ = db.Mdb.Select("mid, name").Where("mid IN ?", mids).Find(&rows).Error
	for _, r := range rows {
		out[r.Mid] = r.Name
	}
	return out
}

func loadMasterDetail(mid int64) (model.MovieDetail, bool, error) {
	var info model.MovieDetailInfo
	err := db.Mdb.Where("mid = ?", mid).First(&info).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return model.MovieDetail{}, false, nil
		}
		return model.MovieDetail{}, false, err
	}
	var detail model.MovieDetail
	if err := json.Unmarshal([]byte(info.Content), &detail); err != nil {
		return model.MovieDetail{}, false, err
	}
	return detail, true, nil
}

func loadSlavePlaylists(sourceID string, mid int64, masterDetail model.MovieDetail) ([]model.MoviePlaylist, error) {
	// 优先用主站详情构造 match keys；否则用 movie_match_key 表
	keys := filmrepo.BuildPlaylistMovieKeys(masterDetail)
	if len(keys) == 0 {
		var mk []model.MovieMatchKey
		_ = db.Mdb.Where("mid = ?", mid).Find(&mk).Error
		for _, k := range mk {
			if strings.TrimSpace(k.MatchKey) != "" {
				keys = append(keys, k.MatchKey)
			}
		}
	}
	if len(keys) == 0 {
		return nil, nil
	}
	var rows []model.MoviePlaylist
	err := db.Mdb.Where("source_id = ? AND movie_key IN ?", sourceID, keys).
		Order("movie_key ASC, group_index ASC").
		Find(&rows).Error
	return rows, err
}

func resolveSourceMid(sourceID string, globalMid int64) int64 {
	var m model.MovieSourceMapping
	err := db.Mdb.Where("source_id = ? AND global_mid = ?", sourceID, globalMid).
		Order("id DESC").First(&m).Error
	if err != nil {
		return 0
	}
	return m.SourceMid
}

func fetchDetail(core *spider.JsonCollect, uri string, sourceMid int64) (model.MovieDetail, error) {
	r := utils.RequestInfo{Uri: uri, Params: url.Values{}}
	r.Params.Set("pg", "1")
	r.Params.Set("ids", strconv.FormatInt(sourceMid, 10))
	list, err := core.GetFilmDetail(r)
	if err != nil {
		return model.MovieDetail{}, err
	}
	if len(list) == 0 {
		return model.MovieDetail{}, fmt.Errorf("empty detail list")
	}
	return list[0], nil
}

// searchSourceMidByName 用 ac=detail&wd= 在附属站搜片名，返回第一条 id。
func searchSourceMidByName(core *spider.JsonCollect, uri, name string) (int64, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, fmt.Errorf("empty name")
	}
	r := utils.RequestInfo{Uri: uri, Params: url.Values{}}
	r.Params.Set("pg", "1")
	r.Params.Set("wd", name)
	list, err := core.GetFilmDetail(r)
	if err != nil {
		return 0, err
	}
	// 优先精确片名
	for _, d := range list {
		if strings.TrimSpace(d.Name) == name && d.Id > 0 {
			return d.Id, nil
		}
	}
	if len(list) > 0 && list[0].Id > 0 {
		return list[0].Id, nil
	}
	return 0, fmt.Errorf("no result for %q", name)
}

func parseSourceFilter(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, strings.ToLower(p))
		}
	}
	return out
}

func sourceMatch(name string, filters []string) bool {
	if len(filters) == 0 {
		return true
	}
	n := strings.ToLower(name)
	for _, f := range filters {
		if strings.Contains(n, f) {
			return true
		}
	}
	return false
}

func parseMids(s string) []int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	var out []int64
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		n, err := strconv.ParseInt(p, 10, 64)
		if err != nil {
			log.Printf("忽略无效 mid: %s", p)
			continue
		}
		out = append(out, n)
	}
	return out
}

func groupPlaylists(rows []model.MoviePlaylist) map[string][]model.MoviePlaylist {
	out := make(map[string][]model.MoviePlaylist)
	for _, r := range rows {
		out[r.MovieKey] = append(out[r.MovieKey], r)
	}
	return out
}

func mergeKeys(a, b map[string][]model.MoviePlaylist) []string {
	seen := map[string]struct{}{}
	var keys []string
	for k := range a {
		seen[k] = struct{}{}
		keys = append(keys, k)
	}
	for k := range b {
		if _, ok := seen[k]; !ok {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

func uniqueKeys(rows []model.MoviePlaylist) []string {
	seen := map[string]struct{}{}
	var keys []string
	for _, r := range rows {
		if _, ok := seen[r.MovieKey]; ok {
			continue
		}
		seen[r.MovieKey] = struct{}{}
		keys = append(keys, shortKey(r.MovieKey))
	}
	return keys
}

func shortKey(k string) string {
	if len(k) <= 12 {
		return k
	}
	return k[:8] + "…"
}
