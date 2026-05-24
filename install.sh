#!/bin/bash
set -e

VERSION="2026.05.25-go3"
REPO="worldneedme/v2relay"
INSTALL_BIN="/usr/local/bin/v2relay"

NO_RUN=0
BUILD_FROM_SOURCE=0

show_update_notes() {
    cat <<'EOF'
本版更新内容：
1. 默认安装改为直接下载单文件二进制，不再需要 tar/gzip 解压，进一步适配 256MB VPS。
2. 默认安装不安装 Go，不跑源码编译；只有 --build-from-source 才会安装 Go 工具链。
3. 如果系统已有 curl 或 wget，安装器不会执行 apt/yum/dnf/apk，减少低内存 VPS 压力。
4. 新增 dnf、apk 包管理器识别，兼容 Rocky/Alma/Fedora/Alpine 等更多 Linux。
5. 安装前会显示系统、架构、安装方式和内存提示。
6. 操作完成后会中文提示：立即生效、已持久化、VPS 重启后自动恢复。
EOF
}

for arg in "$@"; do
    case "$arg" in
        --version)
            echo "$VERSION"
            exit 0
            ;;
        --update-notes)
            show_update_notes
            exit 0
            ;;
        --no-run)
            NO_RUN=1
            ;;
        --build-from-source)
            BUILD_FROM_SOURCE=1
            ;;
    esac
done

if [ "$EUID" -ne 0 ]; then
    echo "[错误] 请使用 root 用户权限运行此安装器。"
    exit 1
fi

command_exists() {
    command -v "$1" >/dev/null 2>&1
}

detect_os_name() {
    if [ -r /etc/os-release ]; then
        . /etc/os-release
        echo "${PRETTY_NAME:-${NAME:-Linux}}"
    else
        uname -s
    fi
}

detect_mem_mb() {
    awk '/MemTotal/ {printf "%d", $2/1024}' /proc/meminfo 2>/dev/null || echo "未知"
}

need_download_dependencies() {
    if ! command_exists curl && ! command_exists wget; then
        return 0
    fi
    return 1
}

install_packages() {
    if [ "$#" -eq 0 ]; then
        return
    fi

    if command_exists apt-get; then
        apt-get update -y
        DEBIAN_FRONTEND=noninteractive apt-get install -yq "$@"
    elif command_exists dnf; then
        dnf install -y "$@"
    elif command_exists yum; then
        yum install -y "$@"
    elif command_exists apk; then
        apk add --no-cache "$@"
    else
        echo "[错误] 未识别的系统包管理器，请先手动安装 curl 或 wget。"
        exit 1
    fi
}

install_download_dependencies() {
    if need_download_dependencies; then
        echo ">> 检测到系统缺少 curl/wget，正在安装基础下载工具..."
        install_packages curl wget ca-certificates
    else
        echo ">> 下载工具已存在，跳过包管理器安装。"
    fi
}

install_build_dependencies() {
    echo ">> 源码编译模式需要 Go 工具链，低内存 VPS 不建议使用。"
    if command_exists apt-get; then
        apt-get update -y
        DEBIAN_FRONTEND=noninteractive apt-get install -yq \
            golang-go curl wget ca-certificates tar gzip
    elif command_exists dnf; then
        dnf install -y golang curl wget ca-certificates tar gzip
    elif command_exists yum; then
        yum install -y golang curl wget ca-certificates tar gzip
    elif command_exists apk; then
        apk add --no-cache go curl wget ca-certificates tar gzip build-base
    else
        echo "[错误] 未识别的系统包管理器，无法自动安装 Go。"
        exit 1
    fi
}

download_file() {
    local url="$1"
    local output="$2"

    if command_exists curl; then
        curl -fL "$url" -o "$output"
    elif command_exists wget; then
        wget -O "$output" "$url"
    else
        echo "[错误] 系统缺少 curl / wget，无法下载文件。"
        exit 1
    fi
}

detect_asset_name() {
    local os arch

    os=$(uname -s | tr '[:upper:]' '[:lower:]')
    arch=$(uname -m)

    case "$os" in
        linux) os="linux" ;;
        *)
            echo "[错误] 当前系统暂不支持: $os"
            exit 1
            ;;
    esac

    case "$arch" in
        x86_64|amd64) arch="amd64" ;;
        aarch64|arm64) arch="arm64" ;;
        armv7l|armv7) arch="armv7" ;;
        *)
            echo "[错误] 当前 CPU 架构暂未提供预编译包: $arch"
            echo "可尝试源码编译安装：bash install.sh --build-from-source"
            exit 1
            ;;
    esac

    echo "v2relay_${VERSION}_${os}_${arch}"
}

detect_asset_label() {
    local asset
    asset=$(detect_asset_name)
    echo "${asset#v2relay_${VERSION}_}"
}

install_from_release() {
    local tmp_dir asset url

    install_download_dependencies
    tmp_dir=$(mktemp -d)
    trap 'rm -rf "$tmp_dir"' EXIT

    asset=$(detect_asset_name)
    url="https://github.com/${REPO}/releases/download/${VERSION}/${asset}"

    echo ">> 下载预编译单文件二进制: $asset"
    if ! download_file "$url" "$tmp_dir/$asset"; then
        echo "[错误] 未能下载匹配的预编译包。"
        echo "请确认 Release 已上传该架构，或使用 --build-from-source 源码编译。"
        exit 1
    fi

    echo ">> 安装到 $INSTALL_BIN..."
    install -m 0755 "$tmp_dir/$asset" "$INSTALL_BIN"
}

install_from_source() {
    local tmp_dir repo_url

    echo ">> 已选择源码编译模式，将安装 Go 工具链。"
    install_build_dependencies

    tmp_dir=$(mktemp -d)
    trap 'rm -rf "$tmp_dir"' EXIT
    repo_url="https://github.com/${REPO}/archive/refs/heads/main.tar.gz"

    echo ">> 下载源码..."
    download_file "$repo_url" "$tmp_dir/v2relay.tar.gz"
    tar -xzf "$tmp_dir/v2relay.tar.gz" -C "$tmp_dir"
    cd "$tmp_dir/v2relay-main"

    echo ">> 编译 Go 单文件二进制..."
    go build -buildvcs=false -trimpath -ldflags "-s -w -X main.Version=$VERSION" -o "$tmp_dir/v2relay" .

    echo ">> 安装到 $INSTALL_BIN..."
    install -m 0755 "$tmp_dir/v2relay" "$INSTALL_BIN"
}

echo "=== v2relay Go 版安装器 ==="
echo "目标版本: $VERSION"
echo "检测系统: $(detect_os_name)"
echo "检测架构: $(detect_asset_label)"
echo "检测内存: $(detect_mem_mb) MB"

if [ "$BUILD_FROM_SOURCE" -eq 1 ]; then
    echo "安装方式: 源码编译，会安装 Go 环境"
    install_from_source
else
    echo "安装方式: Release 预编译二进制，不安装 Go 环境"
    install_from_release
fi

echo "[成功] v2relay 已安装。以后直接输入 v2relay 打开面板。"

if [ "$NO_RUN" -eq 0 ]; then
    exec "$INSTALL_BIN"
fi
