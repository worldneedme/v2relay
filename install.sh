#!/bin/bash
set -e

VERSION="2026.05.25-go1"
REPO="worldneedme/v2relay"
INSTALL_BIN="/usr/local/bin/v2relay"

NO_RUN=0
BUILD_FROM_SOURCE=0

show_update_notes() {
    cat <<'EOF'
本版更新内容：
1. 安装器改为默认下载 GitHub Release 预编译二进制，用户 VPS 不再需要安装 Go 环境。
2. 只有手动指定 --build-from-source 时，才会安装 Go 工具链并从源码编译。
3. 核心面板保持 Go 单文件二进制，长期维护更清晰，运行时依赖更少。
4. 主菜单包含转发代理、跳跃端口、全局规则总览、更新脚本。
5. 跳跃端口规则写入 /etc/v2relay_dport.rules，并通过 v2relay-dport.service 开机加载。
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

install_download_dependencies() {
    if command_exists apt-get; then
        apt-get update -y
        DEBIAN_FRONTEND=noninteractive apt-get install -yq \
            curl wget ca-certificates tar gzip
    elif command_exists yum; then
        yum install -y curl wget ca-certificates tar gzip
    else
        echo "[错误] 未识别的系统包管理器，仅支持 apt-get / yum。"
        exit 1
    fi
}

install_build_dependencies() {
    if command_exists apt-get; then
        apt-get update -y
        DEBIAN_FRONTEND=noninteractive apt-get install -yq \
            golang-go curl wget ca-certificates tar gzip
    elif command_exists yum; then
        yum install -y golang curl wget ca-certificates tar gzip
    else
        echo "[错误] 未识别的系统包管理器，仅支持 apt-get / yum。"
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

    echo "v2relay_${VERSION}_${os}_${arch}.tar.gz"
}

install_from_release() {
    local tmp_dir asset url

    install_download_dependencies
    tmp_dir=$(mktemp -d)
    trap 'rm -rf "$tmp_dir"' EXIT

    asset=$(detect_asset_name)
    url="https://github.com/${REPO}/releases/download/${VERSION}/${asset}"

    echo ">> 下载预编译二进制: $asset"
    if ! download_file "$url" "$tmp_dir/$asset"; then
        echo "[错误] 未能下载匹配的预编译包。"
        echo "请确认 Release 已上传该架构，或使用 --build-from-source 源码编译。"
        exit 1
    fi

    echo ">> 解压并安装到 $INSTALL_BIN..."
    tar -xzf "$tmp_dir/$asset" -C "$tmp_dir"
    install -m 0755 "$tmp_dir/v2relay" "$INSTALL_BIN"
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

if [ "$BUILD_FROM_SOURCE" -eq 1 ]; then
    install_from_source
else
    echo ">> 默认使用预编译二进制安装，不会安装 Go 环境。"
    install_from_release
fi

echo "[成功] v2relay 已安装。以后直接输入 v2relay 打开面板。"

if [ "$NO_RUN" -eq 0 ]; then
    exec "$INSTALL_BIN"
fi
