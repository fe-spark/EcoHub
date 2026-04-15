package support

import (
	"fmt"
	"log"

	"server/internal/infra/db"
	"server/internal/model"
)

func GetCollectSourceList() []model.FilmSource {
	var list []model.FilmSource
	if err := db.Mdb.Order("grade ASC").Find(&list).Error; err != nil {
		log.Println("GetCollectSourceList Error:", err)
		return nil
	}
	return list
}

func TruncateRecordTable() {
	err := db.Mdb.Exec(fmt.Sprintf("TRUNCATE Table %s", model.TableFailureRecord)).Error
	if err != nil {
		log.Println("TRUNCATE TABLE Error: ", err)
	}
}
