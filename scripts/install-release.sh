#!/usr/bin/env sh
set -eu

repo_ref="${ECOHUB_REPO_REF:-main}"
install_dir="${ECOHUB_INSTALL_DIR:-$HOME/ecohub}"
raw_base="${ECOHUB_RAW_BASE:-https://raw.githubusercontent.com/fe-spark/EcoHub/${repo_ref}}"

download() {
  source_url="$1"
  target_path="$2"

  if command -v curl >/dev/null 2>&1; then
    curl -fsSL -o "$target_path" "$source_url"
    return
  fi

  if command -v wget >/dev/null 2>&1; then
    wget -qO "$target_path" "$source_url"
    return
  fi

  echo "错误：需要安装 curl 或 wget 后再执行安装脚本。" >&2
  exit 1
}

mkdir -p "$install_dir"
cd "$install_dir"
mkdir -p data/mysql data/redis data/uploads

download "${raw_base}/deploy/release/compose.yml" "docker-compose.yml"
download "${raw_base}/.env.example" ".env.example"

if [ ! -f ".env" ]; then
  cp ".env.example" ".env"
  echo "已创建 ${install_dir}/.env，首次部署建议按需修改 JWT_SECRET、管理员初始密码 (ECOHUB_ADMIN_PASSWORD) 与数据库密码。"
else
  echo "检测到已有 ${install_dir}/.env，已保留原配置。"
fi

echo "已安装发布版 Docker Compose 编排：${install_dir}/docker-compose.yml"
if ! command -v docker >/dev/null 2>&1; then
  echo "提示：宿主机尚未检测到 Docker，请先安装 Docker 与 Compose 插件后再启动。"
fi
echo "启动命令：cd ${install_dir} && docker compose up -d"
