#!/usr/bin/env sh
# EcoHub 命令行。
# 首次：curl -fsSL .../install-release.sh | sh
# 之后：ecohub status|logs|update|restart|stop|start|uninstall
set -eu

repo_ref="${ECOHUB_REPO_REF:-main}"
raw_base="${ECOHUB_RAW_BASE:-https://raw.githubusercontent.com/fe-spark/EcoHub/${repo_ref}}"

die() {
  printf '错误：%s\n' "$1" >&2
  exit 1
}

resolve_install_dir() {
  if [ -n "${ECOHUB_INSTALL_DIR:-}" ]; then
    printf '%s' "$ECOHUB_INSTALL_DIR"
    return
  fi
  marker="${HOME}/.ecohub/install-dir"
  if [ -f "$marker" ]; then
    saved=$(sed -n '1p' "$marker" | tr -d '\r')
    if [ -n "$saved" ]; then
      printf '%s' "$saved"
      return
    fi
  fi
  printf '%s' "${HOME}/ecohub"
}

install_dir=$(resolve_install_dir)
cli_bin=""

download() {
  source_url=$1
  target_path=$2
  tmp="${target_path}.tmp"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL -o "$tmp" "$source_url"
  elif command -v wget >/dev/null 2>&1; then
    wget -qO "$tmp" "$source_url"
  else
    die "需要 curl 或 wget"
  fi
  mv "$tmp" "$target_path"
}

cli_dest() {
  if [ -n "${ECOHUB_BIN:-}" ]; then
    printf '%s' "$ECOHUB_BIN"
    return
  fi
  case "${0:-}" in
    */ecohub)
      dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
      printf '%s' "${dir}/ecohub"
      return
      ;;
  esac
  if command -v ecohub >/dev/null 2>&1; then
    found=$(command -v ecohub)
    case "$found" in
      /*) printf '%s' "$found"; return ;;
    esac
  fi
  if [ -d /usr/local/bin ] && [ -w /usr/local/bin ]; then
    printf '%s' "/usr/local/bin/ecohub"
    return
  fi
  mkdir -p "${HOME}/.local/bin"
  printf '%s' "${HOME}/.local/bin/ecohub"
}

install_cli() {
  dest=$(cli_dest)
  mkdir -p "$(dirname "$dest")"
  download "${raw_base}/scripts/install-release.sh" "$dest"
  chmod +x "$dest"
  mkdir -p "${HOME}/.ecohub"
  printf '%s\n' "$install_dir" > "${HOME}/.ecohub/install-dir"
  rm -f "${install_dir}/ecohub.sh"
  cli_bin=$dest
}

have_compose() {
  if docker compose version >/dev/null 2>&1; then
    return 0
  fi
  if command -v docker-compose >/dev/null 2>&1; then
    return 0
  fi
  return 1
}

# 用户自己的覆盖编排。install / update 只覆盖 docker-compose.yml，从不写这个文件；
# 存在时每条 compose 命令都叠加它，用来保留端口绑定等自定义。
override_file="docker-compose.override.yml"

compose() {
  if docker compose version >/dev/null 2>&1; then
    if [ -f "$install_dir/$override_file" ]; then
      docker compose --project-directory "$install_dir" -f "$install_dir/docker-compose.yml" -f "$install_dir/$override_file" "$@"
    else
      docker compose --project-directory "$install_dir" -f "$install_dir/docker-compose.yml" "$@"
    fi
    return
  fi
  if command -v docker-compose >/dev/null 2>&1; then
    if [ -f "$install_dir/$override_file" ]; then
      (cd "$install_dir" && docker-compose -f docker-compose.yml -f "$override_file" "$@")
    else
      (cd "$install_dir" && docker-compose -f docker-compose.yml "$@")
    fi
    return
  fi
  die "未检测到 Docker Compose。请安装 Docker 20+ 与 Compose 插件。"
}

need_docker() {
  command -v docker >/dev/null 2>&1 || die "未检测到 Docker。请先安装 Docker 20+ 与 Compose 插件。"
  docker info >/dev/null 2>&1 || die "Docker 未运行。请先启动 Docker 再重试。"
  have_compose || die "未检测到 Docker Compose。请安装 Docker 20+ 与 Compose 插件。"
}

need_layout() {
  [ -f "$install_dir/docker-compose.yml" ] || die "尚未安装。执行 ecohub install，或：curl -fsSL ${raw_base}/scripts/install-release.sh | sh"
  [ -f "$install_dir/.env" ] || die "缺少 ${install_dir}/.env"
}

env_get() {
  key=$1
  default=$2
  file="${install_dir}/.env"
  if [ ! -f "$file" ]; then
    printf '%s' "$default"
    return
  fi
  line=$(grep -E "^[[:space:]]*${key}=" "$file" | tail -n 1 || true)
  if [ -z "$line" ]; then
    printf '%s' "$default"
    return
  fi
  val=${line#*=}
  val=$(printf '%s' "$val" | sed -e 's/[[:space:]]*#.*$//' -e 's/^["'\'']//' -e 's/["'\'']$//' -e 's/[[:space:]]*$//')
  if [ -z "$val" ]; then
    val=$default
  fi
  printf '%s' "$val"
}

set_env_key() {
  key=$1
  val=$2
  file="${install_dir}/.env"
  ECOHUB_SET_KEY=$key ECOHUB_SET_VAL=$val awk '
    BEGIN {
      k = ENVIRON["ECOHUB_SET_KEY"]
      v = ENVIRON["ECOHUB_SET_VAL"]
      re = "^[[:space:]]*#?[[:space:]]*" k "="
      done = 0
    }
    {
      if ($0 ~ re) {
        if (!done) { print k "=" v; done = 1 }
        next
      }
      print
    }
    END { if (!done) print k "=" v }
  ' "$file" > "${file}.tmp"
  mv "${file}.tmp" "$file"
}

apply_env_overrides() {
  # 仅当本次命令显式传入时写回 .env，已有文件不会被整份覆盖。
  for key in ECOHUB_IMAGE WEB_PORT SERVER_PORT JWT_SECRET ECOHUB_ADMIN_PASSWORD MYSQL_ROOT_PASSWORD MYSQL_PASSWORD REDIS_PASSWORD; do
    val=$(printenv "$key" 2>/dev/null || true)
    if [ -n "$val" ]; then
      set_env_key "$key" "$val"
    fi
  done
}

primary_ip() {
  ip=""
  if command -v ip >/dev/null 2>&1; then
    ip=$(ip -4 route get 1.1.1.1 2>/dev/null | awk '{ for (i = 1; i <= NF; i++) if ($i == "src") { print $(i + 1); exit } }' || true)
  fi
  if [ -z "$ip" ] && command -v hostname >/dev/null 2>&1; then
    ip=$(hostname -I 2>/dev/null | awk '{ print $1 }' || true)
  fi
  if [ -z "$ip" ] && command -v ipconfig >/dev/null 2>&1; then
    for iface in en0 en1 eth0; do
      ip=$(ipconfig getifaddr "$iface" 2>/dev/null || true)
      if [ -n "$ip" ]; then
        break
      fi
    done
  fi
  printf '%s' "$ip"
}

print_access() {
  web_port=$(env_get WEB_PORT 3000)
  admin_password=$(env_get ECOHUB_ADMIN_PASSWORD admin)
  image=$(env_get ECOHUB_IMAGE "ghcr.io/fe-spark/ecohub:latest")
  host=$(primary_ip)
  if [ -z "$host" ]; then
    host="127.0.0.1"
  fi
  web="http://${host}:${web_port}"

  printf '\nEcoHub 已就绪\n'
  printf '%s\n' "$image"
  printf '%s\n' "$install_dir"
  if [ -f "$install_dir/$override_file" ]; then
    printf '已叠加 %s\n' "$override_file"
  fi
  printf '\n'
  printf '前台    %s\n' "$web"
  printf '后台    %s/manage\n' "$web"
  printf '账号    admin\n'
  printf '密码    %s\n\n' "$admin_password"
  printf '客户端  %s/api/provide/app\n' "$web"
  printf 'TVBox   %s/api/provide/tvbox\n' "$web"
  printf 'MacCMS  %s/api/provide/vod\n' "$web"
  if [ "${1:-}" = "services" ]; then
    printf '\n'
    compose ps --format '{{.Service}}  {{.Status}}' || true
  fi
  if [ "$host" = "127.0.0.1" ]; then
    printf '\n远程访问时把 127.0.0.1 换成服务器 IP。\n'
  fi
  if [ -n "$cli_bin" ]; then
    bindir=$(dirname "$cli_bin")
    case ":${PATH}:" in
      *":${bindir}:"*) ;;
      *)
        # shellcheck disable=SC2016 # 原样打印 $PATH，让用户复制到自己的终端
        printf '\n当前终端还找不到 ecohub：\nexport PATH="%s:$PATH"\n' "$bindir"
        ;;
    esac
  fi
  printf '\n'
}

fetch_bundle() {
  mkdir -p "$install_dir/data/mysql" "$install_dir/data/redis" "$install_dir/data/uploads"
  download "${raw_base}/deploy/release/compose.yml" "${install_dir}/docker-compose.yml"
  download "${raw_base}/.env.example" "${install_dir}/.env.example"
  fresh=0
  if [ ! -f "${install_dir}/.env" ]; then
    cp "${install_dir}/.env.example" "${install_dir}/.env"
    fresh=1
    printf '已创建 %s/.env\n' "$install_dir"
  else
    printf '已保留 %s/.env\n' "$install_dir"
  fi
  apply_env_overrides
  # 模板里镜像是注释掉的，不写时 compose 会落到 :latest。新安装默认钉当前正式版。
  if [ "$fresh" -eq 1 ] && [ -z "$(env_get ECOHUB_IMAGE "")" ]; then
    set_env_key ECOHUB_IMAGE "ghcr.io/fe-spark/ecohub:v3.0.4"
  fi
  install_cli
}

cmd_install() {
  need_docker
  fetch_bundle
  compose up -d
  print_access
}

cmd_start() {
  need_docker
  need_layout
  compose up -d
  print_access
}

cmd_stop() {
  need_docker
  need_layout
  compose stop
  printf '已停止。数据仍在 %s/data\n' "$install_dir"
  printf '再次启动：ecohub start\n'
}

cmd_restart() {
  need_docker
  need_layout
  compose up -d --force-recreate
  print_access
}

cmd_status() {
  need_docker
  need_layout
  print_access services
}

cmd_logs() {
  need_docker
  need_layout
  if [ "$#" -eq 0 ]; then
    compose logs --tail 100
  else
    compose logs "$@"
  fi
}

cmd_update() {
  need_docker
  fetch_bundle
  compose pull
  compose up -d
  print_access
}

cmd_uninstall() {
  purge=0
  for arg in "$@"; do
    if [ "$arg" = "--purge" ]; then
      purge=1
    else
      die "未知参数：${arg}。卸载用法：uninstall 或 uninstall --purge"
    fi
  done
  need_docker
  if [ -f "$install_dir/docker-compose.yml" ]; then
    compose down --remove-orphans
  else
    printf '未找到 %s/docker-compose.yml，跳过容器删除。\n' "$install_dir"
  fi
  if [ "$purge" -eq 1 ]; then
    rm -rf "${install_dir}/data/mysql" "${install_dir}/data/redis" "${install_dir}/data/uploads"
    printf '已卸载，并删除 %s/data 下的 MySQL、Redis、上传文件。\n' "$install_dir"
    printf '.env 与编排文件仍保留在 %s。命令 ecohub 仍可用。\n' "$install_dir"
  else
    printf '已卸载容器。数据仍在 %s/data\n' "$install_dir"
    printf '连数据一起删除：ecohub uninstall --purge\n'
    printf '命令 ecohub 仍保留，可再次执行 ecohub install。\n'
  fi
}

cmd_help() {
  cat <<EOF
用法：ecohub <命令>

  install                 下载编排、启动，并打印端口与订阅（默认）
  status                  查看容器、端口、前台、后台和订阅地址
  logs [参数]             查看日志。logs -f 跟踪，logs ecohub 只看应用
  start                   按当前 .env 启动
  stop                    停止容器，保留数据
  restart                 改 .env 后重启，并再次打印访问地址
  update                  更新编排和镜像，重建后打印访问地址
  uninstall               删除容器，保留数据与 .env
  uninstall --purge       删除容器，并删除 data 下的数据库和上传文件
  help                    显示本说明

首次安装（变量要写在 sh 前面，写在 curl 前面不会进脚本）：

  curl -fsSL ${raw_base}/scripts/install-release.sh | ECOHUB_IMAGE=ghcr.io/fe-spark/ecohub:v3.0.4 sh

命令安装到 /usr/local/bin/ecohub；该目录不可写时安装到 ~/.local/bin/ecohub。
数据目录默认 \$HOME/ecohub。换目录把 ECOHUB_INSTALL_DIR 写在 sh 前面。命令会记住上次的数据目录。
安装和 update 时，写在 sh 前面的 ECOHUB_IMAGE、WEB_PORT、SERVER_PORT、JWT_SECRET、
ECOHUB_ADMIN_PASSWORD、MYSQL_ROOT_PASSWORD、MYSQL_PASSWORD、REDIS_PASSWORD 会进 .env 再启动。
自定义端口绑定等编排改动写进数据目录下的 docker-compose.override.yml；
install 和 update 不会覆盖它，它存在时每条命令都会叠加。
EOF
}

main() {
  cmd=${1:-install}
  if [ "$#" -gt 0 ]; then
    shift
  fi
  case "$cmd" in
    install | up) cmd_install ;;
    start) cmd_start "$@" ;;
    stop) cmd_stop ;;
    restart) cmd_restart ;;
    status | ps) cmd_status ;;
    logs | log) cmd_logs "$@" ;;
    update | upgrade) cmd_update ;;
    uninstall | remove) cmd_uninstall "$@" ;;
    help | -h | --help) cmd_help ;;
    *) die "未知命令：${cmd}。执行 ecohub help 查看用法。" ;;
  esac
}

main "$@"
