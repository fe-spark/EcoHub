package admin

import "server/internal/admin/upgrade"

type AppVersionInfo = upgrade.AppVersionInfo
type VersionService = upgrade.VersionService

var VersionSvc = upgrade.VersionSvc
var RunUpgradeHelper = upgrade.RunUpgradeHelper
