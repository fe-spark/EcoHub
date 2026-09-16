package access

import (
	"strings"

	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func persistDaily(stats model.AccessDailyStats, tops []model.AccessDailyTop) error {
	if db.Mdb == nil {
		return nil
	}
	return db.Mdb.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{UpdateAll: true}).Create(&stats).Error; err != nil {
			return err
		}
		if err := tx.Where("day = ?", stats.Day).Delete(&model.AccessDailyTop{}).Error; err != nil {
			return err
		}
		if len(tops) == 0 {
			return nil
		}
		return tx.Create(&tops).Error
	})
}

func loadDailyStats(day string) (model.AccessDailyStats, bool) {
	var row model.AccessDailyStats
	if db.Mdb == nil {
		return row, false
	}
	err := db.Mdb.Where("day = ?", day).First(&row).Error
	if err != nil {
		return row, false
	}
	return row, true
}

// HasPersistedData 检查数据库中是否存在历史分析落库数据及总行数
func HasPersistedData() (bool, int64) {
	if db.Mdb == nil {
		return false, 0
	}
	var count int64
	if err := db.Mdb.Model(&model.AccessDailyStats{}).Count(&count).Error; err != nil {
		return false, 0
	}
	if count > 0 {
		return true, count
	}
	var topCount int64
	if err := db.Mdb.Model(&model.AccessDailyTop{}).Limit(1).Count(&topCount).Error; err == nil && topCount > 0 {
		return true, topCount
	}
	return false, 0
}

func loadDailyTops(day, kind string, limit int) []TopItem {
	if db.Mdb == nil {
		return nil
	}
	if limit <= 0 {
		limit = accessTopKeep
	}
	var rows []model.AccessDailyTop
	if err := db.Mdb.Where("day = ? AND kind = ?", day, kind).
		Order("rank ASC").Limit(limit).Find(&rows).Error; err != nil {
		return nil
	}
	items := make([]TopItem, 0, len(rows))
	for _, r := range rows {
		items = append(items, TopItem{
			Key:      r.ItemKey,
			Count:    r.Count,
			Title:    r.Title,
			Category: r.Category,
			Poster:   r.Poster,
			Year:     r.Year,
		})
	}
	return items
}

func overviewFromDaily(row model.AccessDailyStats) *Overview {
	return overviewFromDailyScope(row, "", "")
}

func overviewFromDailyScope(row model.AccessDailyStats, module, platform string) *Overview {
	module = strings.ToLower(strings.TrimSpace(module))
	platform = strings.ToLower(strings.TrimSpace(platform))
	out := &Overview{
		Day:     row.Day,
		PV:      row.PV,
		UV:      row.UV,
		Err4:    row.Err4,
		Err5:    row.Err5,
		P95Ms:   row.P95Ms,
		Dropped: row.Dropped,
		Provide: ProvideStats{
			PV:   row.ProvidePV,
			Err4: row.ProvideErr4,
			Err5: row.ProvideErr5,
		},
		Client:    unmarshalIntMap(row.ClientJSON),
		Action:    unmarshalIntMap(row.ActionJSON),
		Hist:      unmarshalIntMap(row.HistJSON),
		Series:    unmarshalSeries(row.SeriesJSON),
		Platforms: unmarshalIntMap(row.PlatformJSON),
		Versions:  versionsFromDaily(row.VersionJSON, platform),
		Browsers:  unmarshalIntMap(row.BrowserJSON),
		OS:        unmarshalIntMap(row.OSJSON),
		Models:    unmarshalIntMap(row.ModelsJSON),
	}
	if module == "web" {
		out.PV = row.WebPV
		out.UV = row.WebUV
		for i := range out.Series {
			out.Series[i].PV = out.Series[i].WebPV
		}
	} else if module == "app" {
		if platform != "" && platform != "all" {
			platMap := unmarshalIntMap(row.PlatformJSON)
			out.PV = platMap[platform]
			out.UV = unmarshalIntMap(row.PlatformUVJSON)[platform]
			for i := range out.Series {
				switch platform {
				case "android":
					out.Series[i].PV = out.Series[i].AndroidPV
				case "harmony":
					out.Series[i].PV = out.Series[i].HarmonyPV
				case "ios":
					out.Series[i].PV = out.Series[i].IosPV
				default:
					out.Series[i].PV = out.Series[i].AppPV
				}
			}
		} else {
			out.PV = row.AppPV
			out.UV = row.AppUV
			for i := range out.Series {
				out.Series[i].PV = out.Series[i].AppPV
			}
		}
	} else if module == "tvbox" {
		out.PV = row.ProvidePV
		out.UV = row.ProvideUV
		for i := range out.Series {
			out.Series[i].PV = out.Series[i].ProvidePV
		}
	}
	return out
}
