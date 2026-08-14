package config

// Version 产品版本号；可用 -ldflags "-X server/internal/config.Version=..." 在构建时覆盖。
// 与 web/package.json / git tag 对齐（如 2.0.2）。
var Version = "2.0.2"
