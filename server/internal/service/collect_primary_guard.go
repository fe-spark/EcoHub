package service

import (
	"errors"
	"strings"

	"server/internal/model"
	"server/internal/repository"
	"server/internal/spider"
)

var errPrimaryChangeWhileCollecting = errors.New("采集进行中，不能更换主站")

func sourceComesBefore(a, b model.FilmSource) bool {
	if a.Sort != b.Sort {
		return a.Sort < b.Sort
	}
	if !a.CreatedAt.Equal(b.CreatedAt) {
		return a.CreatedAt.Before(b.CreatedAt)
	}
	return a.Id < b.Id
}

func primaryIDFromSources(list []model.FilmSource) string {
	var enabled *model.FilmSource
	var anySource *model.FilmSource
	for i := range list {
		src := list[i]
		if strings.TrimSpace(src.Id) == "" {
			continue
		}
		if anySource == nil || sourceComesBefore(src, *anySource) {
			cp := src
			anySource = &cp
		}
		if !src.State {
			continue
		}
		if enabled == nil || sourceComesBefore(src, *enabled) {
			cp := src
			enabled = &cp
		}
	}
	if enabled != nil {
		return enabled.Id
	}
	if anySource != nil {
		return anySource.Id
	}
	return ""
}

func rejectIfPrimaryChanges(next []model.FilmSource) error {
	old := repository.PickPrimarySourceForCategory()
	oldID := ""
	if old != nil {
		oldID = old.Id
	}
	if primaryIDFromSources(next) == oldID {
		return nil
	}
	if spider.IsAnyTaskRunning() {
		return errPrimaryChangeWhileCollecting
	}
	return nil
}

func sourcesAfterSort(ids []string) []model.FilmSource {
	list := repository.GetCollectSourceList()
	order := make(map[string]int, len(ids))
	for i, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		order[id] = i
	}
	for i := range list {
		if n, ok := order[list[i].Id]; ok {
			list[i].Sort = n
		}
	}
	return list
}

func sourcesAfterStates(states map[string]bool) []model.FilmSource {
	list := repository.GetCollectSourceList()
	for i := range list {
		if state, ok := states[list[i].Id]; ok {
			list[i].State = state
		}
	}
	return list
}

func sourcesWithout(id string) []model.FilmSource {
	list := repository.GetCollectSourceList()
	next := make([]model.FilmSource, 0, len(list))
	for _, src := range list {
		if src.Id == id {
			continue
		}
		next = append(next, src)
	}
	return next
}
