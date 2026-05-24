#!/bin/bash
# =========================================================
# v2relay - Secure Socks5 Forwarder
# 功能: 极简 Socks5 落地 + 专用防火墙链 + 面板配置生成
# 适用: Debian / Ubuntu / CentOS / NAT 机
# =========================================================

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
CYAN='\033[0;36m'
NC='\033[0m'

CONFIG_FILE="/etc/v2relay_node.conf"
SERVICE_FILE="/etc/systemd/system/microsocks.service"
MICROSOCKS_BIN="/usr/local/bin/microsocks"
MICROSOCKS_SRC="/usr/local/src/microsocks"
PANEL_BIN="/usr/local/bin/v2relay"
CHAIN_NAME="V2RELAY"
DEFAULT_PORT="8848"
GITHUB_RAW_URL="https://raw.githubusercontent.com/feinhunter/v2relay/main/install.sh"

if [ "$EUID" -ne 0 ]; then
    echo -e "${RED}[错误] 请使用 root 用户权限运行此脚本！${NC}"
    exit 1
fi

command_exists() {
    command -v "$1" >/dev/null 2>&1
}

pause_menu() {
    echo -n "按任意键返回主菜单..."
    read -r -n 1
}

is_valid_port() {
    local port="$1"
    [[ "$port" =~ ^[0-9]+$ ]] && [ "$port" -ge 1 ] && [ "$port" -le 65535 ]
}

is_valid_ipv4_cidr() {
    local value="$1"
    local ip mask a b c d extra octet

    [[ "$value" =~ ^[0-9./]+$ ]] || return 1

    if [[ "$value" == */* ]]; then
        ip="${value%/*}"
        mask="${value#*/}"
        [[ "$mask" =~ ^[0-9]+$ ]] || return 1
        [ "$mask" -ge 0 ] && [ "$mask" -le 32 ] || return 1
    else
        ip="$value"
    fi

    IFS=. read -r a b c d extra <<< "$ip"
    [ -z "${extra:-}" ] || return 1

    for octet in "$a" "$b" "$c" "$d"; do
        [[ "$octet" =~ ^[0-9]+$ ]] || return 1
        [ "$octet" -ge 0 ] && [ "$octet" -le 255 ] || return 1
    done
}

get_current_port() {
    local port

    if [ ! -f "$CONFIG_FILE" ]; then
        echo "未配置"
        return
    fi

    port=$(awk -F= '$1=="PORT" {gsub(/"/, "", $2); print $2; exit}' "$CONFIG_FILE" 2>/dev/null)
    if is_valid_port "$port"; then
        echo "$port"
    else
        echo "未配置"
    fi
}

save_config() {
    local port="$1"

    (
        umask 077
        {
            echo "PORT=$port"
            echo "CHAIN=$CHAIN_NAME"
        } > "$CONFIG_FILE"
    )
}

save_iptables() {
    if command_exists netfilter-persistent; then
        netfilter-persistent save >/dev/null 2>&1 && return
    fi

    if command_exists apt-get; then
        DEBIAN_FRONTEND=noninteractive apt-get install -yq iptables-persistent >/dev/null 2>&1 || true
        mkdir -p /etc/iptables
        iptables-save > /etc/iptables/rules.v4
    elif command_exists yum; then
        yum install -y iptables-services >/dev/null 2>&1 || true
        service iptables save >/dev/null 2>&1 || true
    fi
}

install_dependencies() {
    if command_exists apt-get; then
        apt-get update -y >/dev/null 2>&1
        DEBIAN_FRONTEND=noninteractive apt-get install -yq \
            build-essential git iptables curl wget ca-certificates >/dev/null 2>&1
    elif command_exists yum; then
        yum groupinstall -y "Development Tools" >/dev/null 2>&1
        yum install -y git iptables curl wget ca-certificates >/dev/null 2>&1
    else
        echo -e "${RED}[错误] 未识别的系统包管理器，仅支持 apt-get / yum。${NC}"
        return 1
    fi
}

install_panel_shortcut() {
    local self_path

    mkdir -p "$(dirname "$PANEL_BIN")"
    self_path=$(readlink -f "$0" 2>/dev/null || true)

    if [ -n "$self_path" ] && [ -f "$self_path" ]; then
        if [ "$self_path" != "$PANEL_BIN" ]; then
            cp "$self_path" "$PANEL_BIN"
        fi
    elif command_exists curl; then
        curl -fsSL "$GITHUB_RAW_URL" -o "$PANEL_BIN" || return 1
    elif command_exists wget; then
        wget -qO "$PANEL_BIN" "$GITHUB_RAW_URL" || return 1
    else
        return 1
    fi

    chmod +x "$PANEL_BIN"
}

create_service_user() {
    if id -u v2relay >/dev/null 2>&1; then
        return
    fi

    useradd --system --no-create-home --shell /usr/sbin/nologin v2relay >/dev/null 2>&1 \
        || useradd -r -s /sbin/nologin v2relay >/dev/null 2>&1 \
        || true
}

chain_exists() {
    iptables -nL "$CHAIN_NAME" >/dev/null 2>&1
}

remove_all_chain_jumps() {
    local line

    while :; do
        line=$(iptables -nL INPUT --line-numbers 2>/dev/null | awk -v chain="$CHAIN_NAME" '$2==chain {print $1; exit}')
        [ -n "$line" ] || break
        iptables -D INPUT "$line" 2>/dev/null || break
    done
}

cleanup_legacy_port_rules() {
    local port="$1"
    local lines line

    is_valid_port "$port" || return 0

    while :; do
        lines=$(iptables -nL INPUT --line-numbers 2>/dev/null \
            | awk -v p="dpt:$port" -v chain="$CHAIN_NAME" '$0 ~ p && $2 != chain && ($2=="ACCEPT" || $2=="DROP") {print $1}' \
            | sort -rn)
        [ -n "$lines" ] || break

        for line in $lines; do
            iptables -D INPUT "$line" 2>/dev/null || true
        done
    done
}

ensure_firewall_chain() {
    local port="$1"

    iptables -N "$CHAIN_NAME" 2>/dev/null || true
    iptables -C INPUT -p tcp --dport "$port" -j "$CHAIN_NAME" 2>/dev/null \
        || iptables -I INPUT 1 -p tcp --dport "$port" -j "$CHAIN_NAME"
}

reset_firewall_rules() {
    local port="$1"
    local allow_ip="$2"
    local old_port="${3:-}"

    remove_all_chain_jumps
    cleanup_legacy_port_rules "$port"
    if [ -n "$old_port" ] && [ "$old_port" != "$port" ]; then
        cleanup_legacy_port_rules "$old_port"
    fi

    ensure_firewall_chain "$port"
    iptables -F "$CHAIN_NAME"
    iptables -A "$CHAIN_NAME" -p tcp -s "$allow_ip" --dport "$port" -j ACCEPT
    iptables -A "$CHAIN_NAME" -p tcp --dport "$port" -j DROP
}

insert_allow_ip() {
    local port="$1"
    local allow_ip="$2"
    local drop_line

    ensure_firewall_chain "$port"

    if iptables -C "$CHAIN_NAME" -p tcp -s "$allow_ip" --dport "$port" -j ACCEPT 2>/dev/null; then
        echo -e "${YELLOW}[提示] IP 已存在，无需重复添加: $allow_ip${NC}"
        return
    fi

    drop_line=$(iptables -nL "$CHAIN_NAME" --line-numbers 2>/dev/null \
        | awk -v p="dpt:$port" '$2=="DROP" && $0 ~ p {print $1; exit}')

    if [ -n "$drop_line" ]; then
        iptables -I "$CHAIN_NAME" "$drop_line" -p tcp -s "$allow_ip" --dport "$port" -j ACCEPT
    else
        iptables -A "$CHAIN_NAME" -p tcp -s "$allow_ip" --dport "$port" -j ACCEPT
        iptables -A "$CHAIN_NAME" -p tcp --dport "$port" -j DROP
    fi
}

delete_allow_ip() {
    local port="$1"
    local allow_ip="$2"
    local removed=0

    while iptables -D "$CHAIN_NAME" -p tcp -s "$allow_ip" --dport "$port" -j ACCEPT 2>/dev/null; do
        removed=1
    done

    while iptables -D INPUT -p tcp -s "$allow_ip" --dport "$port" -j ACCEPT 2>/dev/null; do
        removed=1
    done

    if [ "$removed" -eq 1 ]; then
        echo -e "${GREEN}[成功] 已移除 IP: $allow_ip${NC}"
    else
        echo -e "${YELLOW}[提示] 未找到该 IP 的放行规则: $allow_ip${NC}"
    fi
}

cleanup_firewall() {
    local port="$1"

    remove_all_chain_jumps
    cleanup_legacy_port_rules "$port"

    if chain_exists; then
        iptables -F "$CHAIN_NAME" 2>/dev/null || true
        iptables -X "$CHAIN_NAME" 2>/dev/null || true
    fi
}

install_node() {
    local input_port PORT ALLOW_IP OLD_PORT SERVICE_USER

    clear
    echo -e "${CYAN}=== 1. 部署 v2relay 落地中继节点 ===${NC}"
    read -r -p "请输入要监听的 Socks5 端口 (默认 $DEFAULT_PORT): " input_port
    PORT=${input_port:-$DEFAULT_PORT}

    if ! is_valid_port "$PORT"; then
        echo -e "${RED}[错误] 端口必须是 1-65535 的数字。${NC}"
        sleep 2
        return
    fi

    read -r -p "请输入【允许访问】的前端机 IPv4 或 CIDR: " ALLOW_IP
    if ! is_valid_ipv4_cidr "$ALLOW_IP"; then
        echo -e "${RED}[错误] 必须输入合法 IPv4 或 CIDR，例如 1.2.3.4 或 1.2.3.0/24。${NC}"
        sleep 2
        return
    fi

    OLD_PORT=$(get_current_port)

    echo -e "${YELLOW}>> 正在安装编译依赖...${NC}"
    install_dependencies || {
        echo -e "${RED}[错误] 依赖安装失败。${NC}"
        sleep 2
        return
    }

    echo -e "${YELLOW}>> 正在编译极简核心 (MicroSocks)...${NC}"
    mkdir -p /usr/local/src
    cd /usr/local/src || return
    rm -rf microsocks
    git clone https://github.com/rofl0r/microsocks.git >/dev/null 2>&1 || {
        echo -e "${RED}[错误] MicroSocks 源码拉取失败。${NC}"
        sleep 2
        return
    }
    make -C "$MICROSOCKS_SRC" >/dev/null 2>&1 || {
        echo -e "${RED}[错误] MicroSocks 编译失败。${NC}"
        sleep 2
        return
    }
    install -m 0755 "$MICROSOCKS_SRC/microsocks" "$MICROSOCKS_BIN"

    echo -e "${YELLOW}>> 配置 Systemd 守护进程...${NC}"
    create_service_user
    SERVICE_USER="root"
    if id -u v2relay >/dev/null 2>&1; then
        SERVICE_USER="v2relay"
    fi

    cat > "$SERVICE_FILE" << SYSTEMD_EOF
[Unit]
Description=v2relay Secure Socks5 Forwarder
After=network.target

[Service]
Type=simple
User=$SERVICE_USER
ExecStart=$MICROSOCKS_BIN -q -i 0.0.0.0 -p $PORT
Restart=on-failure
RestartSec=5s
LimitNOFILE=65535
NoNewPrivileges=true
PrivateTmp=true
ProtectHome=true
ProtectSystem=full

[Install]
WantedBy=multi-user.target
SYSTEMD_EOF

    systemctl daemon-reload
    systemctl enable microsocks >/dev/null 2>&1
    systemctl restart microsocks

    if ! systemctl is-active --quiet microsocks; then
        echo -e "${RED}[错误] microsocks 启动失败，请执行 journalctl -u microsocks -n 50 查看原因。${NC}"
        sleep 3
        return
    fi

    echo -e "${YELLOW}>> 配置 iptables 专用链，只接管 TCP $PORT...${NC}"
    reset_firewall_rules "$PORT" "$ALLOW_IP" "$OLD_PORT"
    save_iptables
    save_config "$PORT"

    if install_panel_shortcut; then
        echo -e "${GREEN}[成功] 快捷命令已安装：直接输入 v2relay 即可打开面板。${NC}"
    else
        echo -e "${YELLOW}[提示] 快捷命令安装失败，不影响 Socks5 服务运行。${NC}"
    fi

    echo -e "${GREEN}[成功] v2relay 部署完成！仅 TCP $PORT 进入 $CHAIN_NAME 专用链，其它端口不受影响。${NC}"
    pause_menu
}

add_ip() {
    local PORT NEW_IP

    PORT=$(get_current_port)
    if [ "$PORT" = "未配置" ]; then
        echo -e "${RED}[错误] 请先执行安装部署！${NC}"
        sleep 2
        return
    fi

    echo -e "${CYAN}=== 2. 添加允许访问的前端机 IP ===${NC}"
    read -r -p "请输入新的前端机 IPv4 或 CIDR: " NEW_IP
    if ! is_valid_ipv4_cidr "$NEW_IP"; then
        echo -e "${RED}[错误] IP 格式不合法。${NC}"
        sleep 2
        return
    fi

    insert_allow_ip "$PORT" "$NEW_IP"
    save_iptables
    echo -e "${GREEN}[成功] 已放行 IP: $NEW_IP${NC}"
    pause_menu
}

remove_ip() {
    local PORT DEL_IP

    PORT=$(get_current_port)
    if [ "$PORT" = "未配置" ]; then
        echo -e "${RED}[错误] 请先执行安装部署！${NC}"
        sleep 2
        return
    fi

    echo -e "${CYAN}=== 3. 移除已被放行的 IP ===${NC}"
    echo -e "${YELLOW}当前 $CHAIN_NAME 放行规则如下：${NC}"
    if chain_exists; then
        iptables -nL "$CHAIN_NAME" --line-numbers | grep "dpt:$PORT" | grep ACCEPT || echo "暂无放行 IP"
    else
        echo "暂无专用链"
    fi
    echo ""
    read -r -p "请输入要移除的 IPv4 或 CIDR: " DEL_IP
    if ! is_valid_ipv4_cidr "$DEL_IP"; then
        echo -e "${RED}[错误] IP 格式不合法。${NC}"
        sleep 2
        return
    fi

    delete_allow_ip "$PORT" "$DEL_IP"
    save_iptables
    pause_menu
}

show_status() {
    local PORT

    clear
    PORT=$(get_current_port)
    echo -e "${CYAN}=== 4. 节点运行状态与防火墙规则 ===${NC}"
    echo -e "当前配置端口: ${YELLOW}$PORT${NC}"
    echo "----------------------------------------"
    if systemctl is-active --quiet microsocks; then
        echo -e "中继核心状态: ${GREEN}运行中 (Active)${NC}"
    else
        echo -e "中继核心状态: ${RED}已停止 (Inactive)${NC}"
    fi
    echo "----------------------------------------"
    echo -e "${YELLOW}INPUT 中仅挂载到 $CHAIN_NAME 的规则:${NC}"
    iptables -nL INPUT --line-numbers | awk -v chain="$CHAIN_NAME" '$2==chain {print}' || true
    echo "----------------------------------------"
    echo -e "${YELLOW}$CHAIN_NAME 专用链规则:${NC}"
    if chain_exists; then
        iptables -nL "$CHAIN_NAME" --line-numbers
    else
        echo "专用链不存在"
    fi
    echo "----------------------------------------"
    pause_menu
}

uninstall_node() {
    local PORT confirm

    PORT=$(get_current_port)
    echo -e "${RED}警告：这将删除 v2relay 核心程序、快捷命令，并清空 $CHAIN_NAME 专用防火墙规则！${NC}"
    echo -e "${YELLOW}只会清理 v2relay 管理的端口和专用链，不会改动其它 VPS 项目端口。${NC}"
    read -r -p "确定要卸载吗？(y/n): " confirm
    if [ "$confirm" = "y" ]; then
        systemctl stop microsocks 2>/dev/null || true
        systemctl disable microsocks 2>/dev/null || true
        rm -f "$SERVICE_FILE"
        rm -f "$MICROSOCKS_BIN"
        rm -f "$PANEL_BIN"
        rm -rf "$MICROSOCKS_SRC"
        systemctl daemon-reload

        cleanup_firewall "$PORT"
        save_iptables
        rm -f "$CONFIG_FILE"
        echo -e "${GREEN}[成功] 已彻底卸载 v2relay。${NC}"
    fi
    pause_menu
}

generate_json_config() {
    local PORT PUBLIC_IP

    clear
    PORT=$(get_current_port)
    if [ "$PORT" = "未配置" ]; then
        echo -e "${RED}[错误] 尚未配置端口，请先执行选项 [1] 安装部署！${NC}"
        sleep 2
        return
    fi

    echo -e "${YELLOW}正在获取本机公网 IP 地址...${NC}"
    PUBLIC_IP=$(curl -s --max-time 3 ipv4.icanhazip.com | tr -d '[:space:]')
    if [ -z "$PUBLIC_IP" ]; then
        PUBLIC_IP=$(curl -s --max-time 3 ipinfo.io/ip | tr -d '[:space:]')
    fi
    PUBLIC_IP=${PUBLIC_IP:-你的机器公网IP}

    echo -e "${CYAN}======================================================${NC}"
    echo -e "${GREEN}       WONDERX 面板专属出站与路由匹配备忘录         ${NC}"
    echo -e "${CYAN}======================================================${NC}"

    echo -e "${YELLOW}【第一步】在 [路由管理 -> Xray出站配置] 中新建或替换：${NC}"
    echo -e "${NC}注意：如果是德国节点，请把 tag 改为 de-socks5 区分。"
    echo -e "${CYAN}------------------- 复制下方基础配置 -------------------${NC}"
    cat <<EOF
{
  "tag": "v2relay-socks5",
  "protocol": "socks",
  "settings": {
    "servers": [
      {
        "address": "$PUBLIC_IP",
        "port": $PORT
      }
    ]
  }
}
EOF
    echo -e "${CYAN}--------------------------------------------------------${NC}"
    echo ""
    echo -e "${YELLOW}【第二步】常用 AI 域名匹配值与分流动作备忘：${NC}"
    echo -e "如果在德国或某些节点连不上 Google AI/Gemini，需要在 Xray 路由规则里指定匹配。"
    echo ""
    echo -e "  ${GREEN}匹配网站 (domain):${NC}"
    echo -e "     - ${CYAN}Google AI / Gemini:${NC}  domain:google.com, domain:gemini.google.com, domain:generativelanguage.googleapis.com"
    echo -e "     - ${CYAN}OpenAI / ChatGPT:${NC}   domain:openai.com, domain:chatgpt.com, domain:auth0.openai.com"
    echo -e "     - ${CYAN}Anthropic / Claude:${NC} domain:anthropic.com, domain:claude.ai"
    echo ""
    echo -e "  ${GREEN}对应动作 (Action / Outbound Tag):${NC}"
    echo -e "     - 将上述域名加入路由规则，Outbound 动作指向对应节点的 ${YELLOW}tag${NC} (例如：de-socks5 或 kr-arm02-socks5)"
    echo -e "     - 确保规则优先级高于默认的通用出站规则。"
    echo -e "${CYAN}======================================================${NC}"
    echo ""
    echo -n "看完并复制完成后，按任意键返回主菜单..."
    read -r -n 1
}

update_script() {
    local SCRIPT_PATH tmp_file

    echo -e "${CYAN}=== 7. 更新 v2relay 面板脚本 ===${NC}"
    echo -e "${YELLOW}正在从 GitHub 拉取最新版本代码...${NC}"

    SCRIPT_PATH=$(readlink -f "$0" 2>/dev/null || true)
    tmp_file="/tmp/v2relay_update.sh"

    if command_exists curl; then
        curl -fsSL "$GITHUB_RAW_URL" -o "$tmp_file"
    else
        wget -qO "$tmp_file" "$GITHUB_RAW_URL"
    fi

    if [ $? -eq 0 ] && bash -n "$tmp_file"; then
        install -m 0755 "$tmp_file" "$PANEL_BIN"
        if [ -n "$SCRIPT_PATH" ] && [ -f "$SCRIPT_PATH" ] && [ "$SCRIPT_PATH" != "$PANEL_BIN" ]; then
            cp "$tmp_file" "$SCRIPT_PATH"
            chmod +x "$SCRIPT_PATH"
        fi
        echo -e "${GREEN}[成功] 脚本已更新至最新版！正在重启面板...${NC}"
        sleep 2
        exec "$PANEL_BIN"
    else
        echo -e "${RED}[错误] 更新失败，请检查网络或 GitHub 链接是否正确。${NC}"
        sleep 2
    fi
}

while true; do
    clear
    echo -e "${CYAN}====================================================${NC}"
    echo -e "${GREEN}        v2relay - 极简 AI 落地机安全管理面板        ${NC}"
    echo -e "${CYAN}====================================================${NC}"
    echo -e " 当前状态: $(systemctl is-active --quiet microsocks 2>/dev/null && echo -e "${GREEN}运行中${NC}" || echo -e "${RED}未运行${NC}") | 监听端口: ${YELLOW}$(get_current_port)${NC}"
    echo ""
    echo -e "  ${YELLOW}1.${NC} 安装 v2relay (默认 8848 + 专用防火墙链)"
    echo -e "  ${YELLOW}2.${NC} 添加前端节点 IP (白名单放行)"
    echo -e "  ${YELLOW}3.${NC} 移除前端节点 IP (阻断连接)"
    echo -e "  ${YELLOW}4.${NC} 查看状态与安防规则"
    echo -e "  ${YELLOW}5.${NC} 彻底卸载与清理"
    echo -e "  ${GREEN}6.${NC} 生成配置与 AI 域名分流备忘录"
    echo -e "  ${YELLOW}7.${NC} 更新脚本至最新版本 (从 GitHub)"
    echo -e "  ${YELLOW}0.${NC} 退出面板"
    echo ""
    read -r -p "请输入选项 [0-7]: " OPTION

    case $OPTION in
        1) install_node ;;
        2) add_ip ;;
        3) remove_ip ;;
        4) show_status ;;
        5) uninstall_node ;;
        6) generate_json_config ;;
        7) update_script ;;
        0) clear; echo -e "${GREEN}v2relay 已退出。${NC}"; exit 0 ;;
        *) echo -e "${RED}无效选项，请重新输入。${NC}"; sleep 1 ;;
    esac
done
