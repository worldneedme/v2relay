#!/bin/bash
set -e

VERSION="2026.05.25-go1"
REPO_TARBALL_URL="https://github.com/worldneedme/v2relay/archive/refs/heads/main.tar.gz"
INSTALL_BIN="/usr/local/bin/v2relay"

NO_RUN=0

show_update_notes() {
    cat <<'EOF'
本版更新内容：
1. 核心面板从 Bash 重构为 Go，长期维护更清晰，运行时是单文件二进制。
2. 保留 install.sh 作为轻量安装器，负责安装 Go 工具链、编译并安装 /usr/local/bin/v2relay。
3. 主菜单升级为一体化入口：转发代理、跳跃端口、全局规则总览、更新脚本。
4. 转发代理继续使用 TCP 8848 + V2RELAY 专用链，只接管本项目端口。
5. 新增跳跃端口模块，支持 UDP 端口范围跳到指定端口。
6. 跳跃端口新增 IP Type 选择：IPv4、IPv6、IPv4 + IPv6。
7. 跳跃端口规则写入 /etc/v2relay_dport.rules，并通过 v2relay-dport.service 开机加载。
8. 更新脚本会显示远程中文更新说明，确认后才执行更新。
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
    esac
done

if [ "$EUID" -ne 0 ]; then
    echo "[错误] 请使用 root 用户权限运行此安装器。"
    exit 1
fi

command_exists() {
    command -v "$1" >/dev/null 2>&1
}

install_build_dependencies() {
    if command_exists apt-get; then
        apt-get update -y
        DEBIAN_FRONTEND=noninteractive apt-get install -yq \
            golang-go curl wget ca-certificates tar
    elif command_exists yum; then
        yum install -y golang curl wget ca-certificates tar
    else
        echo "[错误] 未识别的系统包管理器，仅支持 apt-get / yum。"
        exit 1
    fi
}

download_source() {
    local output="$1"

    if command_exists curl; then
        curl -fsSL "$REPO_TARBALL_URL" -o "$output"
    elif command_exists wget; then
        wget -qO "$output" "$REPO_TARBALL_URL"
    else
        echo "[错误] 系统缺少 curl / wget，无法下载源码。"
        exit 1
    fi
}

echo "=== v2relay Go 版安装器 ==="
echo "目标版本: $VERSION"
echo ">> 安装 Go 编译环境与下载工具..."
install_build_dependencies

TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT

echo ">> 下载 v2relay 源码..."
download_source "$TMP_DIR/v2relay.tar.gz"

echo ">> 解压源码..."
tar -xzf "$TMP_DIR/v2relay.tar.gz" -C "$TMP_DIR"
cd "$TMP_DIR/v2relay-main"

echo ">> 编译 Go 单文件二进制..."
go build -buildvcs=false -trimpath -ldflags "-s -w -X main.Version=$VERSION" -o "$TMP_DIR/v2relay" .

echo ">> 安装到 $INSTALL_BIN..."
install -m 0755 "$TMP_DIR/v2relay" "$INSTALL_BIN"

echo "[成功] v2relay 已安装。以后直接输入 v2relay 打开面板。"

if [ "$NO_RUN" -eq 0 ]; then
    exec "$INSTALL_BIN"
fi
