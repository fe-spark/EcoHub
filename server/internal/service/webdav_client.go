package service

import (
	"context"

	"server/internal/model"
	"server/internal/utils"
)

type WebdavFileInfo = utils.WebdavFileInfo

var (
	CanonicalizeWebDAVRelPath = utils.CanonicalizeWebDAVRelPath
	NormalizeWebDAVUri        = utils.NormalizeWebDAVUri
	BuildWebDAVCollectionURL  = utils.BuildWebDAVCollectionURL
	BuildWebDAVFileURL        = utils.BuildWebDAVFileURL
	NewWebDAVHttpClient       = utils.NewWebDAVHttpClient
	ListWebDAVFiles           = utils.ListWebDAVFiles
	VideoExtMap               = utils.VideoExtMap
	IgnoredDirMap             = utils.IgnoredDirMap
)

func TestWebDAVConnection(cfg model.WebdavConfig) error {
	return utils.TestWebDAVConnection(cfg.ServerURL, cfg.RootPath, cfg.Username, cfg.Password)
}

func ListWebDAVFilesWithCfg(ctx context.Context, cfg model.WebdavConfig) ([]WebdavFileInfo, bool, error) {
	return utils.ListWebDAVFiles(ctx, cfg.ServerURL, cfg.RootPath, cfg.Username, cfg.Password, cfg.MinFileBytes)
}
