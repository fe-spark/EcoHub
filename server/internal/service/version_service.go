package service

import "server/internal/service/upgrade"

type AppVersionInfo = upgrade.AppVersionInfo
type VersionService = upgrade.VersionService

var VersionSvc = upgrade.VersionSvc
var RunUpgradeHelper = upgrade.RunUpgradeHelper
