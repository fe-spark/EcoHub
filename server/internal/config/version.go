package config

// Version 产品版本号；可用 -ldflags "-X server/internal/config.Version=..." 在构建时覆盖。
// 与 web/package.json / git tag 对齐（如 2.0.4）。
var Version = "2.0.4"

// ProjectURL 开源仓库地址，Telegram 欢迎 / 帮助文案中的项目地址跳转。
const ProjectURL = "https://github.com/fe-spark/EcoHub"
