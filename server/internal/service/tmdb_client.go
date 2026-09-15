package service

import (
	"server/internal/utils"
)

type TmdbClient = utils.TmdbClient
type TmdbMediaDetail = utils.TmdbMediaDetail

var (
	ErrNoTmdbApiKey  = utils.ErrNoTmdbApiKey
	ErrTmdbNotFound  = utils.ErrTmdbNotFound
	ErrTmdbMismatch  = utils.ErrTmdbMismatch
	ErrTmdbRateLimit = utils.ErrTmdbRateLimit
	NewTmdbClient    = utils.NewTmdbClient
)
