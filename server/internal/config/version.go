package config

// Version 产品版本号；可用 -ldflags "-X server/internal/config.Version=..." 在构建时覆盖。
// 与 web/package.json / git tag 对齐（如 1.1.5-beta.3）。
var Version = "1.1.5-beta.3"
