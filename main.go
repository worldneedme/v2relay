package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var Version = "2026.05.25-go3"

const (
	configFile              = "/etc/v2relay_node.conf"
	serviceFile             = "/etc/systemd/system/microsocks.service"
	microsocksBin           = "/usr/local/bin/microsocks"
	microsocksSrc           = "/usr/local/src/microsocks"
	panelBin                = "/usr/local/bin/v2relay"
	chainName               = "V2RELAY"
	defaultPort             = "8848"
	rawInstallURL           = "https://raw.githubusercontent.com/worldneedme/v2relay/main/install.sh"
	rawUpdateNotesURL       = "https://raw.githubusercontent.com/worldneedme/v2relay/main/UPDATE_NOTES.md"
	dportConfigFile         = "/etc/v2relay_dport.rules"
	dportApplyBin           = "/usr/local/bin/v2relay-dport-apply.sh"
	dportServiceFile        = "/etc/systemd/system/v2relay-dport.service"
	dportDefaultJumpPort    = "10593"
	dportDefaultSourcePorts = "10595:11596"
)

const updateNotesText = `本版更新内容：
1. 默认安装改为直接下载单文件二进制，不再需要 tar/gzip 解压，进一步适配 256MB VPS。
2. 默认安装不安装 Go，不跑源码编译；只有 --build-from-source 才会安装 Go 工具链。
3. 如果系统已有 curl 或 wget，安装器不会执行 apt/yum/dnf/apk，减少低内存 VPS 压力。
4. 新增 dnf、apk 包管理器识别，兼容 Rocky/Alma/Fedora/Alpine 等更多 Linux。
5. 安装前会显示系统、架构、安装方式和内存提示。
6. 操作完成后会中文提示：立即生效、已持久化、VPS 重启后自动恢复。
`

type App struct {
	reader *bufio.Reader
}

type DPortRule struct {
	IPType      string
	Interface   string
	SourcePorts string
	JumpPort    string
}

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--version":
			fmt.Println(Version)
			return
		case "--update-notes":
			fmt.Print(updateNotesText)
			return
		case "--smoke":
			if err := smokeTest(); err != nil {
				fmt.Fprintf(os.Stderr, "smoke failed: %v\n", err)
				os.Exit(1)
			}
			fmt.Println("smoke ok")
			return
		}
	}

	if os.Geteuid() != 0 {
		fmt.Println("[错误] 请使用 root 用户权限运行此程序。")
		os.Exit(1)
	}

	app := &App{reader: bufio.NewReader(os.Stdin)}
	app.mainMenu()
}

func smokeTest() error {
	tests := []struct {
		port  string
		valid bool
	}{
		{"8848", true},
		{"0", false},
		{"65536", false},
	}
	for _, tt := range tests {
		if isValidPort(tt.port) != tt.valid {
			return fmt.Errorf("port validation mismatch for %s", tt.port)
		}
	}

	if !isValidPortRange("10595:11596") || !isValidPortRange("10595") || isValidPortRange("11596:10595") {
		return errors.New("port range validation mismatch")
	}
	if !isValidIPv4CIDR("1.2.3.4") || !isValidIPv4CIDR("1.2.3.0/24") || isValidIPv4CIDR("999.2.3.4") {
		return errors.New("IPv4 validation mismatch")
	}
	if !isValidIPType("dual") || isValidIPType("all") {
		return errors.New("IP type validation mismatch")
	}
	return nil
}

func (a *App) mainMenu() {
	for {
		clearScreen()
		fmt.Println(color(Cyan, "===================================================="))
		fmt.Println(color(Green, "        v2relay - 一体化 VPS 转发管理面板"))
		fmt.Println(color(Cyan, "===================================================="))
		fmt.Printf(" 脚本版本: %s | Socks5 端口: %s\n", color(Yellow, Version), color(Yellow, getCurrentPort()))
		fmt.Println()
		fmt.Printf("  %s 转发代理 (Socks5 落地节点)\n", menuNo("1."))
		fmt.Printf("  %s 跳跃端口 (UDP 端口范围转发)\n", menuNo("2."))
		fmt.Printf("  %s 全局规则总览\n", menuNo("3."))
		fmt.Printf("  %s 更新脚本 (显示中文更新内容)\n", menuNo("4."))
		fmt.Printf("  %s 退出面板\n", menuNo("0."))
		fmt.Println()

		switch a.readLine("请输入选项 [0-4]: ") {
		case "1":
			a.proxyMenu()
		case "2":
			a.dportMenu()
		case "3":
			a.showGlobalStatus()
		case "4":
			a.updateProgram()
		case "0":
			clearScreen()
			fmt.Println(color(Green, "v2relay 已退出。"))
			return
		default:
			fmt.Println(color(Red, "无效选项，请重新输入。"))
			sleepSecond()
		}
	}
}

func (a *App) proxyMenu() {
	for {
		clearScreen()
		fmt.Println(color(Cyan, "===================================================="))
		fmt.Println(color(Green, "        转发代理 - Socks5 落地节点管理"))
		fmt.Println(color(Cyan, "===================================================="))
		status := color(Red, "未运行")
		if commandOK("systemctl", "is-active", "--quiet", "microsocks") {
			status = color(Green, "运行中")
		}
		fmt.Printf(" 当前状态: %s | 监听端口: %s\n\n", status, color(Yellow, getCurrentPort()))
		fmt.Printf("  %s 安装 v2relay (默认 8848 + 专用防火墙链)\n", menuNo("1."))
		fmt.Printf("  %s 添加前端节点 IP (白名单放行)\n", menuNo("2."))
		fmt.Printf("  %s 移除前端节点 IP (阻断连接)\n", menuNo("3."))
		fmt.Printf("  %s 查看状态与安防规则\n", menuNo("4."))
		fmt.Printf("  %s 彻底卸载与清理\n", menuNo("5."))
		fmt.Printf("  %s 生成配置与 AI 域名分流备忘录\n", color(Green, "6."))
		fmt.Printf("  %s 返回主菜单\n", menuNo("0."))
		fmt.Println()

		switch a.readLine("请输入选项 [0-6]: ") {
		case "1":
			a.installNode()
		case "2":
			a.addIP()
		case "3":
			a.removeIP()
		case "4":
			a.showProxyStatus()
		case "5":
			a.uninstallNode()
		case "6":
			a.generateJSONConfig()
		case "0":
			return
		default:
			fmt.Println(color(Red, "无效选项，请重新输入。"))
			sleepSecond()
		}
	}
}

func (a *App) dportMenu() {
	for {
		clearScreen()
		fmt.Println(color(Cyan, "===================================================="))
		fmt.Println(color(Green, "        跳跃端口 - UDP 端口范围转发管理"))
		fmt.Println(color(Cyan, "===================================================="))
		fmt.Printf("  %s 添加跳跃端口规则\n", menuNo("1."))
		fmt.Printf("  %s 查看跳跃端口规则\n", menuNo("2."))
		fmt.Printf("  %s 删除跳跃端口规则\n", menuNo("3."))
		fmt.Printf("  %s 重载并保存规则\n", menuNo("4."))
		fmt.Printf("  %s 返回主菜单\n", menuNo("0."))
		fmt.Println()

		switch a.readLine("请输入选项 [0-4]: ") {
		case "1":
			a.dportAddRule()
		case "2":
			a.dportShowRules()
		case "3":
			a.dportDeleteRule()
		case "4":
			a.dportReloadRules()
		case "0":
			return
		default:
			fmt.Println(color(Red, "无效选项，请重新输入。"))
			sleepSecond()
		}
	}
}

func (a *App) installNode() {
	clearScreen()
	fmt.Println(color(Cyan, "=== 部署 v2relay 落地中继节点 ==="))
	port := a.readLine(fmt.Sprintf("请输入要监听的 Socks5 端口 (默认 %s): ", defaultPort))
	if port == "" {
		port = defaultPort
	}
	if !isValidPort(port) {
		fmt.Println(color(Red, "[错误] 端口必须是 1-65535 的数字。"))
		sleepSecond()
		return
	}

	allowIP := a.readLine("请输入【允许访问】的前端机 IPv4 或 CIDR: ")
	if !isValidIPv4CIDR(allowIP) {
		fmt.Println(color(Red, "[错误] 必须输入合法 IPv4 或 CIDR，例如 1.2.3.4 或 1.2.3.0/24。"))
		sleepSecond()
		return
	}

	oldPort := getCurrentPort()

	fmt.Println(color(Yellow, ">> 正在安装编译依赖..."))
	if err := installProxyDependencies(); err != nil {
		fmt.Println(color(Red, "[错误] 依赖安装失败: "+err.Error()))
		sleepSecond()
		return
	}

	fmt.Println(color(Yellow, ">> 正在编译极简核心 (MicroSocks)..."))
	if err := buildMicrosocks(); err != nil {
		fmt.Println(color(Red, "[错误] MicroSocks 编译失败: "+err.Error()))
		sleepSecond()
		return
	}

	fmt.Println(color(Yellow, ">> 配置 Systemd 守护进程..."))
	serviceUser := createServiceUser()
	if err := writeMicrosocksService(port, serviceUser); err != nil {
		fmt.Println(color(Red, "[错误] Systemd 服务写入失败: "+err.Error()))
		sleepSecond()
		return
	}

	fmt.Printf("%s\n", color(Yellow, fmt.Sprintf(">> 配置 iptables 专用链，只接管 TCP %s...", port)))
	if err := resetFirewallRules(port, allowIP, oldPort); err != nil {
		fmt.Println(color(Red, "[错误] 防火墙规则配置失败，未启动 microsocks: "+err.Error()))
		sleepSecond()
		return
	}
	if err := saveIptables(); err != nil {
		fmt.Println(color(Red, "[错误] 防火墙规则持久化失败: "+err.Error()))
		sleepSecond()
		return
	}
	if err := saveProxyConfig(port); err != nil {
		fmt.Println(color(Red, "[错误] 配置文件保存失败: "+err.Error()))
		sleepSecond()
		return
	}

	if err := run("systemctl", "daemon-reload"); err != nil {
		fmt.Println(color(Red, "[错误] systemctl daemon-reload 失败: "+err.Error()))
		sleepSecond()
		return
	}
	_ = run("systemctl", "enable", "microsocks")
	if err := run("systemctl", "restart", "microsocks"); err != nil {
		fmt.Println(color(Red, "[错误] microsocks 启动失败: "+err.Error()))
		sleepSecond()
		return
	}
	if !commandOK("systemctl", "is-active", "--quiet", "microsocks") {
		fmt.Println(color(Red, "[错误] microsocks 未处于运行状态，请执行 journalctl -u microsocks -n 50 查看原因。"))
		sleepSecond()
		return
	}

	fmt.Println(color(Green, "[成功] 快捷命令已安装：直接输入 v2relay 即可打开面板。"))
	fmt.Printf("%s\n", color(Green, fmt.Sprintf("[成功] v2relay 部署完成！仅 TCP %s 进入 %s 专用链，其它端口不受影响。", port, chainName)))
	printPersistHint("转发代理规则")
	a.pause()
}

func (a *App) addIP() {
	port := getCurrentPort()
	if port == "未配置" {
		fmt.Println(color(Red, "[错误] 请先执行安装部署。"))
		sleepSecond()
		return
	}

	fmt.Println(color(Cyan, "=== 添加允许访问的前端机 IP ==="))
	newIP := a.readLine("请输入新的前端机 IPv4 或 CIDR: ")
	if !isValidIPv4CIDR(newIP) {
		fmt.Println(color(Red, "[错误] IP 格式不合法。"))
		sleepSecond()
		return
	}

	if err := insertAllowIP(port, newIP); err != nil {
		fmt.Println(color(Red, "[错误] 防火墙规则写入失败: "+err.Error()))
		sleepSecond()
		return
	}
	if err := saveIptables(); err != nil {
		fmt.Println(color(Red, "[错误] 防火墙规则持久化失败: "+err.Error()))
		sleepSecond()
		return
	}
	fmt.Println(color(Green, "[成功] 已放行 IP: "+newIP))
	printPersistHint("转发代理白名单")
	a.pause()
}

func (a *App) removeIP() {
	port := getCurrentPort()
	if port == "未配置" {
		fmt.Println(color(Red, "[错误] 请先执行安装部署。"))
		sleepSecond()
		return
	}

	fmt.Println(color(Cyan, "=== 移除已被放行的 IP ==="))
	fmt.Println(color(Yellow, "当前 "+chainName+" 放行规则如下："))
	if chainExists() {
		printFilteredCommand([]string{"iptables", "-nL", chainName, "--line-numbers"}, []string{"dpt:" + port, "ACCEPT"}, "暂无放行 IP")
	} else {
		fmt.Println("暂无专用链")
	}
	fmt.Println()

	delIP := a.readLine("请输入要移除的 IPv4 或 CIDR: ")
	if !isValidIPv4CIDR(delIP) {
		fmt.Println(color(Red, "[错误] IP 格式不合法。"))
		sleepSecond()
		return
	}

	removed := deleteAllowIP(port, delIP)
	if err := saveIptables(); err != nil {
		fmt.Println(color(Red, "[错误] 防火墙规则持久化失败: "+err.Error()))
		sleepSecond()
		return
	}
	if removed {
		fmt.Println(color(Green, "[成功] 已移除 IP: "+delIP))
		printPersistHint("转发代理白名单")
	} else {
		fmt.Println(color(Yellow, "[提示] 未找到该 IP 的放行规则: "+delIP))
	}
	a.pause()
}

func (a *App) showProxyStatus() {
	clearScreen()
	port := getCurrentPort()
	fmt.Println(color(Cyan, "=== 节点运行状态与防火墙规则 ==="))
	fmt.Printf("当前配置端口: %s\n", color(Yellow, port))
	fmt.Println("----------------------------------------")
	if commandOK("systemctl", "is-active", "--quiet", "microsocks") {
		fmt.Println("中继核心状态: " + color(Green, "运行中 (Active)"))
	} else {
		fmt.Println("中继核心状态: " + color(Red, "已停止 (Inactive)"))
	}
	fmt.Println("----------------------------------------")
	fmt.Println(color(Yellow, "INPUT 中仅挂载到 "+chainName+" 的规则:"))
	printInputChainJumps()
	fmt.Println("----------------------------------------")
	fmt.Println(color(Yellow, chainName+" 专用链规则:"))
	if chainExists() {
		printCommand("iptables", "-nL", chainName, "--line-numbers")
	} else {
		fmt.Println("专用链不存在")
	}
	fmt.Println("----------------------------------------")
	a.pause()
}

func (a *App) uninstallNode() {
	port := getCurrentPort()
	fmt.Println(color(Red, "警告：这将删除 v2relay 核心程序、快捷命令，并清空 "+chainName+" 专用防火墙规则。"))
	fmt.Println(color(Yellow, "只会清理 v2relay 管理的端口和专用链，不会改动其它 VPS 项目端口。"))
	if a.readLine("确定要卸载吗？(y/n): ") != "y" {
		return
	}

	_ = run("systemctl", "stop", "microsocks")
	_ = run("systemctl", "disable", "microsocks")
	_ = os.Remove(serviceFile)
	_ = os.Remove(microsocksBin)
	_ = os.Remove(panelBin)
	_ = os.RemoveAll(microsocksSrc)
	_ = run("systemctl", "daemon-reload")

	cleanupFirewall(port)
	if err := saveIptables(); err != nil {
		fmt.Println(color(Red, "[错误] 防火墙规则持久化失败: "+err.Error()))
		sleepSecond()
		return
	}
	_ = os.Remove(configFile)
	fmt.Println(color(Green, "[成功] 已彻底卸载 v2relay。"))
	printPersistHint("卸载清理结果")
	a.pause()
}

func (a *App) generateJSONConfig() {
	clearScreen()
	port := getCurrentPort()
	if port == "未配置" {
		fmt.Println(color(Red, "[错误] 尚未配置端口，请先安装部署。"))
		sleepSecond()
		return
	}

	fmt.Println(color(Yellow, "正在获取本机公网 IP 地址..."))
	publicIP := strings.TrimSpace(fetchText("https://ipv4.icanhazip.com", 3*time.Second))
	if publicIP == "" {
		publicIP = strings.TrimSpace(fetchText("https://ipinfo.io/ip", 3*time.Second))
	}
	if publicIP == "" {
		publicIP = "你的机器公网IP"
	}

	fmt.Println(color(Cyan, "======================================================"))
	fmt.Println(color(Green, "       WONDERX 面板专属出站与路由匹配备忘录"))
	fmt.Println(color(Cyan, "======================================================"))
	fmt.Println(color(Yellow, "【第一步】在 [路由管理 -> Xray出站配置] 中新建或替换："))
	fmt.Println("注意：如果是德国节点，请把 tag 改为 de-socks5 区分。")
	fmt.Println(color(Cyan, "------------------- 复制下方基础配置 -------------------"))
	fmt.Printf(`{
  "tag": "v2relay-socks5",
  "protocol": "socks",
  "settings": {
    "servers": [
      {
        "address": "%s",
        "port": %s
      }
    ]
  }
}
`, publicIP, port)
	fmt.Println(color(Cyan, "--------------------------------------------------------"))
	fmt.Println()
	fmt.Println(color(Yellow, "【第二步】常用 AI 域名匹配值与分流动作备忘："))
	fmt.Println("如果在德国或某些节点连不上 Google AI/Gemini，需要在 Xray 路由规则里指定匹配。")
	fmt.Println()
	fmt.Println("  " + color(Green, "匹配网站 (domain):"))
	fmt.Println("     - " + color(Cyan, "Google AI / Gemini:") + "  domain:google.com, domain:gemini.google.com, domain:generativelanguage.googleapis.com")
	fmt.Println("     - " + color(Cyan, "OpenAI / ChatGPT:") + "   domain:openai.com, domain:chatgpt.com, domain:auth0.openai.com")
	fmt.Println("     - " + color(Cyan, "Anthropic / Claude:") + " domain:anthropic.com, domain:claude.ai")
	fmt.Println()
	fmt.Println("  " + color(Green, "对应动作 (Action / Outbound Tag):"))
	fmt.Println("     - 将上述域名加入路由规则，Outbound 动作指向对应节点的 tag，例如 de-socks5 或 kr-arm02-socks5")
	fmt.Println("     - 确保规则优先级高于默认的通用出站规则。")
	fmt.Println(color(Cyan, "======================================================"))
	a.pause()
}

func (a *App) dportAddRule() {
	clearScreen()
	fmt.Println(color(Cyan, "=== 跳跃端口：添加规则 ==="))
	if err := installDPortDependencies(); err != nil {
		fmt.Println(color(Red, "[错误] 跳跃端口依赖安装失败: "+err.Error()))
		sleepSecond()
		return
	}

	detected := detectNetworkInterface()
	if detected == "" {
		detected = "未检测到"
	}
	fmt.Println(color(Yellow, "检测到的网卡名称: "+detected))
	iface := a.readLine("按 Enter 使用检测到的网卡，或手动输入网卡名称: ")
	if iface == "" && detected != "未检测到" {
		iface = detected
	}
	if iface == "" || !commandOK("ip", "link", "show", iface) {
		fmt.Println(color(Red, "[错误] 网卡不存在或未输入网卡名称。"))
		sleepSecond()
		return
	}

	ipType := a.selectIPType()
	if !isValidIPType(ipType) {
		fmt.Println(color(Red, "[错误] IP Type 选择无效。"))
		sleepSecond()
		return
	}

	jumpPort := a.readLine(fmt.Sprintf("输入跳跃目标端口 (默认 %s): ", dportDefaultJumpPort))
	if jumpPort == "" {
		jumpPort = dportDefaultJumpPort
	}
	if !isValidPort(jumpPort) {
		fmt.Println(color(Red, "[错误] 目标端口必须是 1-65535 的数字。"))
		sleepSecond()
		return
	}

	sourcePorts := a.readLine(fmt.Sprintf("输入源端口或范围 (默认 %s): ", dportDefaultSourcePorts))
	if sourcePorts == "" {
		sourcePorts = dportDefaultSourcePorts
	}
	if !isValidPortRange(sourcePorts) {
		fmt.Println(color(Red, "[错误] 源端口格式不合法，例如 10595 或 10595:11596。"))
		sleepSecond()
		return
	}

	fmt.Println()
	fmt.Println(color(Yellow, "即将添加规则:"))
	fmt.Println("  IP Type: " + dportTypeLabel(ipType))
	fmt.Println("  网卡: " + iface)
	fmt.Println("  协议: UDP")
	fmt.Println("  源端口: " + sourcePorts)
	fmt.Println("  跳跃目标端口: " + jumpPort)
	fmt.Println()
	if a.readLine("确认添加？(y/n): ") != "y" {
		fmt.Println(color(Yellow, "[提示] 已取消。"))
		sleepSecond()
		return
	}

	rule := DPortRule{IPType: ipType, Interface: iface, SourcePorts: sourcePorts, JumpPort: jumpPort}
	if err := dportApplyNATRule(rule); err != nil {
		fmt.Println(color(Red, "[错误] NAT 规则应用失败: "+err.Error()))
		sleepSecond()
		return
	}
	if err := dportSaveRuleConfig(rule); err != nil {
		fmt.Println(color(Red, "[错误] 规则配置保存失败: "+err.Error()))
		sleepSecond()
		return
	}
	if err := dportWriteApplyScript(); err != nil {
		fmt.Println(color(Red, "[错误] 开机加载脚本写入失败: "+err.Error()))
		sleepSecond()
		return
	}
	if err := dportWriteSystemdService(); err != nil {
		fmt.Println(color(Red, "[错误] systemd 服务写入失败: "+err.Error()))
		sleepSecond()
		return
	}
	if err := saveIptables(); err != nil {
		fmt.Println(color(Red, "[错误] 防火墙规则持久化失败: "+err.Error()))
		sleepSecond()
		return
	}

	fmt.Println(color(Green, "[成功] 跳跃端口规则已添加，并已配置开机自动加载。"))
	printPersistHint("跳跃端口规则")
	a.pause()
}

func (a *App) dportShowRules() {
	clearScreen()
	fmt.Println(color(Cyan, "=== 跳跃端口：查看规则 ==="))
	fmt.Println(color(Yellow, "配置文件规则:"))
	rules := readDPortRules()
	if len(rules) == 0 {
		fmt.Println("  暂无规则")
	} else {
		for i, rule := range rules {
			fmt.Printf("  %d. IP Type=%s | 网卡=%s | UDP %s -> %s\n", i+1, dportTypeLabel(rule.IPType), rule.Interface, rule.SourcePorts, rule.JumpPort)
		}
	}
	fmt.Println("----------------------------------------")
	fmt.Println(color(Yellow, "当前 NAT 规则 (IPv4):"))
	printFilteredCommand([]string{"iptables", "-t", "nat", "-L", "PREROUTING", "-n", "-v"}, []string{"DNAT", "udp"}, "  暂无 IPv4 跳跃规则")
	fmt.Println("----------------------------------------")
	fmt.Println(color(Yellow, "当前 NAT 规则 (IPv6):"))
	printFilteredCommand([]string{"ip6tables", "-t", "nat", "-L", "PREROUTING", "-n", "-v"}, []string{"DNAT", "udp"}, "  暂无 IPv6 跳跃规则")
	fmt.Println("----------------------------------------")
	if commandOK("systemctl", "is-enabled", "--quiet", "v2relay-dport.service") {
		fmt.Println("开机加载服务: " + color(Green, "已启用"))
	} else {
		fmt.Println("开机加载服务: " + color(Yellow, "未启用"))
	}
	a.pause()
}

func (a *App) dportDeleteRule() {
	clearScreen()
	fmt.Println(color(Cyan, "=== 跳跃端口：删除规则 ==="))
	rules := readDPortRules()
	if len(rules) == 0 {
		fmt.Println(color(Yellow, "[提示] 当前没有已记录的跳跃端口规则。"))
		sleepSecond()
		return
	}
	for i, rule := range rules {
		fmt.Printf("  %d. IP Type=%s | 网卡=%s | UDP %s -> %s\n", i+1, dportTypeLabel(rule.IPType), rule.Interface, rule.SourcePorts, rule.JumpPort)
	}
	fmt.Println()
	selected := a.readLine("请输入要删除的规则编号: ")
	idx, err := strconv.Atoi(selected)
	if err != nil || idx < 1 || idx > len(rules) {
		fmt.Println(color(Red, "[错误] 规则编号无效。"))
		sleepSecond()
		return
	}

	rule := rules[idx-1]
	fmt.Println()
	fmt.Printf("%s IP Type=%s | 网卡=%s | UDP %s -> %s\n", color(Yellow, "即将删除:"), dportTypeLabel(rule.IPType), rule.Interface, rule.SourcePorts, rule.JumpPort)
	if a.readLine("确认删除？(y/n): ") != "y" {
		fmt.Println(color(Yellow, "[提示] 已取消。"))
		sleepSecond()
		return
	}

	dportDeleteNATRule(rule)
	if err := dportRemoveRuleConfig(rule); err != nil {
		fmt.Println(color(Red, "[错误] 规则配置删除失败: "+err.Error()))
		sleepSecond()
		return
	}
	_ = dportWriteApplyScript()
	if err := dportWriteSystemdService(); err != nil {
		fmt.Println(color(Red, "[错误] 开机加载服务写入失败: "+err.Error()))
		sleepSecond()
		return
	}
	if err := saveIptables(); err != nil {
		fmt.Println(color(Red, "[错误] 防火墙规则持久化失败: "+err.Error()))
		sleepSecond()
		return
	}
	fmt.Println(color(Green, "[成功] 跳跃端口规则已删除。"))
	printPersistHint("跳跃端口规则")
	a.pause()
}

func (a *App) dportReloadRules() {
	clearScreen()
	fmt.Println(color(Cyan, "=== 跳跃端口：重载并保存规则 ==="))
	if len(readDPortRules()) == 0 {
		fmt.Println(color(Yellow, "[提示] 没有配置文件规则可重载。"))
		sleepSecond()
		return
	}
	if err := dportApplyAllRules(); err != nil {
		fmt.Println(color(Red, "[错误] 跳跃端口规则重载失败: "+err.Error()))
		sleepSecond()
		return
	}
	fmt.Println(color(Green, "[成功] 跳跃端口规则已重载并保存。"))
	printPersistHint("跳跃端口规则")
	a.pause()
}

func (a *App) showGlobalStatus() {
	clearScreen()
	port := getCurrentPort()
	fmt.Println(color(Cyan, "=== 全局规则总览 ==="))
	fmt.Printf("程序版本: %s\n", color(Yellow, Version))
	fmt.Println("----------------------------------------")
	fmt.Println(color(Yellow, "转发代理模块:"))
	fmt.Printf("  Socks5 端口: %s\n", color(Yellow, port))
	if commandOK("systemctl", "is-active", "--quiet", "microsocks") {
		fmt.Println("  microsocks: " + color(Green, "运行中"))
	} else {
		fmt.Println("  microsocks: " + color(Red, "未运行"))
	}
	fmt.Println("  INPUT 跳转规则:")
	printInputChainJumps()
	fmt.Println("  " + chainName + " 专用链:")
	if chainExists() {
		indentCommand("iptables", "-nL", chainName, "--line-numbers")
	} else {
		fmt.Println("    专用链不存在")
	}

	fmt.Println("----------------------------------------")
	fmt.Println(color(Yellow, "跳跃端口模块:"))
	rules := readDPortRules()
	if len(rules) == 0 {
		fmt.Println("  暂无配置文件规则")
	} else {
		for i, rule := range rules {
			fmt.Printf("  %d. IP Type=%s | 网卡=%s | UDP %s -> %s\n", i+1, dportTypeLabel(rule.IPType), rule.Interface, rule.SourcePorts, rule.JumpPort)
		}
	}
	if commandOK("systemctl", "is-enabled", "--quiet", "v2relay-dport.service") {
		fmt.Println("  开机加载服务: " + color(Green, "已启用"))
	} else {
		fmt.Println("  开机加载服务: " + color(Yellow, "未启用"))
	}
	fmt.Println("----------------------------------------")
	a.pause()
}

func (a *App) updateProgram() {
	clearScreen()
	fmt.Println(color(Cyan, "=== 更新 v2relay 一体化脚本 ==="))
	fmt.Println(color(Yellow, "正在读取远程版本更新说明..."))

	notes := fetchText(rawUpdateNotesURL, 8*time.Second)
	if strings.TrimSpace(notes) == "" {
		notes = "远程仓库暂未提供更新说明。"
	}
	fmt.Println()
	fmt.Println(color(Yellow, "当前版本: ") + Version)
	fmt.Println(color(Cyan, "------------------- 远程版本更新内容 -------------------"))
	fmt.Print(notes)
	if !strings.HasSuffix(notes, "\n") {
		fmt.Println()
	}
	fmt.Println(color(Cyan, "--------------------------------------------------------"))
	fmt.Println()

	if a.readLine("确认从 GitHub 更新并重新编译？(y/n): ") != "y" {
		fmt.Println(color(Yellow, "[提示] 已取消更新。"))
		sleepSecond()
		return
	}

	tmpFile := filepath.Join(os.TempDir(), "v2relay-install.sh")
	data, err := fetchBytes(rawInstallURL, 15*time.Second)
	if err != nil {
		fmt.Println(color(Red, "[错误] 下载安装器失败: "+err.Error()))
		sleepSecond()
		return
	}
	if err := os.WriteFile(tmpFile, data, 0755); err != nil {
		fmt.Println(color(Red, "[错误] 写入临时安装器失败: "+err.Error()))
		sleepSecond()
		return
	}

	if err := run("bash", tmpFile, "--no-run"); err != nil {
		fmt.Println(color(Red, "[错误] 更新失败: "+err.Error()))
		sleepSecond()
		return
	}

	fmt.Println(color(Green, "[成功] 已更新并重新安装 v2relay，正在重启面板..."))
	time.Sleep(2 * time.Second)
	_ = syscall.Exec(panelBin, []string{panelBin}, os.Environ())
}

func installProxyDependencies() error {
	if hasCommand("apt-get") {
		_ = runEnv([]string{"DEBIAN_FRONTEND=noninteractive"}, "apt-get", "update", "-y")
		return runEnv([]string{"DEBIAN_FRONTEND=noninteractive"}, "apt-get", "install", "-yq", "build-essential", "git", "iptables", "curl", "wget", "ca-certificates", "make")
	}
	if hasCommand("yum") {
		_ = run("yum", "groupinstall", "-y", "Development Tools")
		return run("yum", "install", "-y", "git", "iptables", "curl", "wget", "ca-certificates", "make")
	}
	return errors.New("未识别的系统包管理器，仅支持 apt-get / yum")
}

func installDPortDependencies() error {
	if hasCommand("apt-get") {
		return runEnv([]string{"DEBIAN_FRONTEND=noninteractive"}, "apt-get", "install", "-yq", "iptables", "curl", "wget", "ca-certificates")
	}
	if hasCommand("yum") {
		return run("yum", "install", "-y", "iptables", "iptables-services", "curl", "wget", "ca-certificates")
	}
	return nil
}

func buildMicrosocks() error {
	if err := os.MkdirAll("/usr/local/src", 0755); err != nil {
		return err
	}
	if err := os.RemoveAll(microsocksSrc); err != nil {
		return err
	}
	if err := run("git", "clone", "https://github.com/rofl0r/microsocks.git", microsocksSrc); err != nil {
		return err
	}
	if err := run("make", "-C", microsocksSrc); err != nil {
		return err
	}
	return run("install", "-m", "0755", filepath.Join(microsocksSrc, "microsocks"), microsocksBin)
}

func createServiceUser() string {
	if commandOK("id", "-u", "v2relay") {
		return "v2relay"
	}
	if commandOK("useradd", "--system", "--no-create-home", "--shell", "/usr/sbin/nologin", "v2relay") {
		return "v2relay"
	}
	if commandOK("useradd", "-r", "-s", "/sbin/nologin", "v2relay") {
		return "v2relay"
	}
	return "root"
}

func writeMicrosocksService(port, user string) error {
	content := fmt.Sprintf(`[Unit]
Description=v2relay Secure Socks5 Forwarder
After=network.target

[Service]
Type=simple
User=%s
ExecStart=%s -q -i 0.0.0.0 -p %s
Restart=on-failure
RestartSec=5s
LimitNOFILE=65535
NoNewPrivileges=true
PrivateTmp=true
ProtectHome=true
ProtectSystem=full

[Install]
WantedBy=multi-user.target
`, user, microsocksBin, port)
	return os.WriteFile(serviceFile, []byte(content), 0644)
}

func saveProxyConfig(port string) error {
	content := fmt.Sprintf("PORT=%s\nCHAIN=%s\n", port, chainName)
	return os.WriteFile(configFile, []byte(content), 0600)
}

func getCurrentPort() string {
	data, err := os.ReadFile(configFile)
	if err != nil {
		return "未配置"
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "PORT=") {
			port := strings.Trim(strings.TrimPrefix(line, "PORT="), `"`)
			if isValidPort(port) {
				return port
			}
		}
	}
	return "未配置"
}

func saveIptables() error {
	if hasCommand("netfilter-persistent") && commandOK("netfilter-persistent", "save") {
		return nil
	}
	if hasCommand("apt-get") {
		if err := runEnv([]string{"DEBIAN_FRONTEND=noninteractive"}, "apt-get", "install", "-yq", "iptables-persistent"); err != nil {
			return err
		}
		if err := os.MkdirAll("/etc/iptables", 0755); err != nil {
			return err
		}
		out, err := commandOutput("iptables-save")
		if err != nil {
			return err
		}
		if err := os.WriteFile("/etc/iptables/rules.v4", []byte(out), 0644); err != nil {
			return err
		}
		if out, err := commandOutput("ip6tables-save"); err == nil {
			if err := os.WriteFile("/etc/iptables/rules.v6", []byte(out), 0644); err != nil {
				return err
			}
		}
		return nil
	}
	if hasCommand("yum") {
		if err := run("yum", "install", "-y", "iptables-services"); err != nil {
			return err
		}
		return run("service", "iptables", "save")
	}
	return errors.New("未找到可用的 iptables 持久化方式")
}

func chainExists() bool {
	return commandOK("iptables", "-nL", chainName)
}

func removeAllChainJumps() {
	for {
		out, err := commandOutput("iptables", "-nL", "INPUT", "--line-numbers")
		if err != nil {
			return
		}
		lineNo := ""
		for _, line := range strings.Split(out, "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 2 && fields[1] == chainName {
				lineNo = fields[0]
				break
			}
		}
		if lineNo == "" {
			return
		}
		if err := runQuiet("iptables", "-D", "INPUT", lineNo); err != nil {
			return
		}
	}
}

func cleanupLegacyPortRules(port string) {
	if !isValidPort(port) {
		return
	}
	for {
		out, err := commandOutput("iptables", "-nL", "INPUT", "--line-numbers")
		if err != nil {
			return
		}
		var lines []int
		for _, line := range strings.Split(out, "\n") {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			if strings.Contains(line, "dpt:"+port) && fields[1] != chainName && (fields[1] == "ACCEPT" || fields[1] == "DROP") {
				n, err := strconv.Atoi(fields[0])
				if err == nil {
					lines = append(lines, n)
				}
			}
		}
		if len(lines) == 0 {
			return
		}
		sort.Sort(sort.Reverse(sort.IntSlice(lines)))
		for _, lineNo := range lines {
			_ = runQuiet("iptables", "-D", "INPUT", strconv.Itoa(lineNo))
		}
	}
}

func ensureFirewallChain(port string) error {
	_ = runQuiet("iptables", "-N", chainName)
	if commandOK("iptables", "-C", "INPUT", "-p", "tcp", "--dport", port, "-j", chainName) {
		return nil
	}
	return run("iptables", "-I", "INPUT", "1", "-p", "tcp", "--dport", port, "-j", chainName)
}

func resetFirewallRules(port, allowIP, oldPort string) error {
	removeAllChainJumps()
	cleanupLegacyPortRules(port)
	if oldPort != "" && oldPort != "未配置" && oldPort != port {
		cleanupLegacyPortRules(oldPort)
	}
	if err := ensureFirewallChain(port); err != nil {
		return err
	}
	if err := run("iptables", "-F", chainName); err != nil {
		return err
	}
	if err := run("iptables", "-A", chainName, "-p", "tcp", "-s", allowIP, "--dport", port, "-j", "ACCEPT"); err != nil {
		return err
	}
	return run("iptables", "-A", chainName, "-p", "tcp", "--dport", port, "-j", "DROP")
}

func insertAllowIP(port, allowIP string) error {
	if err := ensureFirewallChain(port); err != nil {
		return err
	}
	if commandOK("iptables", "-C", chainName, "-p", "tcp", "-s", allowIP, "--dport", port, "-j", "ACCEPT") {
		fmt.Println(color(Yellow, "[提示] IP 已存在，无需重复添加: "+allowIP))
		return nil
	}
	dropLine := findDropLineInChain(port)
	if dropLine != "" {
		return run("iptables", "-I", chainName, dropLine, "-p", "tcp", "-s", allowIP, "--dport", port, "-j", "ACCEPT")
	}
	if err := run("iptables", "-A", chainName, "-p", "tcp", "-s", allowIP, "--dport", port, "-j", "ACCEPT"); err != nil {
		return err
	}
	return run("iptables", "-A", chainName, "-p", "tcp", "--dport", port, "-j", "DROP")
}

func findDropLineInChain(port string) string {
	out, err := commandOutput("iptables", "-nL", chainName, "--line-numbers")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == "DROP" && strings.Contains(line, "dpt:"+port) {
			return fields[0]
		}
	}
	return ""
}

func deleteAllowIP(port, allowIP string) bool {
	removed := false
	for commandOK("iptables", "-D", chainName, "-p", "tcp", "-s", allowIP, "--dport", port, "-j", "ACCEPT") {
		removed = true
	}
	for commandOK("iptables", "-D", "INPUT", "-p", "tcp", "-s", allowIP, "--dport", port, "-j", "ACCEPT") {
		removed = true
	}
	return removed
}

func cleanupFirewall(port string) {
	removeAllChainJumps()
	cleanupLegacyPortRules(port)
	if chainExists() {
		_ = runQuiet("iptables", "-F", chainName)
		_ = runQuiet("iptables", "-X", chainName)
	}
}

func dportApplyNATRule(rule DPortRule) error {
	dportDeleteNATRule(rule)
	if rule.IPType == "ipv4" || rule.IPType == "dual" {
		if err := run("iptables", "-t", "nat", "-A", "PREROUTING", "-i", rule.Interface, "-p", "udp", "--dport", rule.SourcePorts, "-j", "DNAT", "--to-destination", ":"+rule.JumpPort); err != nil {
			return err
		}
	}
	if rule.IPType == "ipv6" || rule.IPType == "dual" {
		if err := run("ip6tables", "-t", "nat", "-A", "PREROUTING", "-i", rule.Interface, "-p", "udp", "--dport", rule.SourcePorts, "-j", "DNAT", "--to-destination", ":"+rule.JumpPort); err != nil {
			return err
		}
	}
	return nil
}

func dportDeleteNATRule(rule DPortRule) {
	if rule.IPType == "ipv4" || rule.IPType == "dual" {
		for commandOK("iptables", "-t", "nat", "-D", "PREROUTING", "-i", rule.Interface, "-p", "udp", "--dport", rule.SourcePorts, "-j", "DNAT", "--to-destination", ":"+rule.JumpPort) {
		}
	}
	if rule.IPType == "ipv6" || rule.IPType == "dual" {
		for commandOK("ip6tables", "-t", "nat", "-D", "PREROUTING", "-i", rule.Interface, "-p", "udp", "--dport", rule.SourcePorts, "-j", "DNAT", "--to-destination", ":"+rule.JumpPort) {
		}
	}
}

func dportSaveRuleConfig(rule DPortRule) error {
	rules := readDPortRules()
	for _, existing := range rules {
		if existing == rule {
			return nil
		}
	}
	rules = append(rules, rule)
	return writeDPortRules(rules)
}

func dportRemoveRuleConfig(rule DPortRule) error {
	var kept []DPortRule
	for _, existing := range readDPortRules() {
		if existing != rule {
			kept = append(kept, existing)
		}
	}
	return writeDPortRules(kept)
}

func readDPortRules() []DPortRule {
	data, err := os.ReadFile(dportConfigFile)
	if err != nil {
		return nil
	}
	var rules []DPortRule
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) != 4 {
			continue
		}
		rule := DPortRule{IPType: parts[0], Interface: parts[1], SourcePorts: parts[2], JumpPort: parts[3]}
		if isValidIPType(rule.IPType) && rule.Interface != "" && isValidPortRange(rule.SourcePorts) && isValidPort(rule.JumpPort) {
			rules = append(rules, rule)
		}
	}
	return rules
}

func writeDPortRules(rules []DPortRule) error {
	if err := os.MkdirAll(filepath.Dir(dportConfigFile), 0755); err != nil {
		return err
	}
	var buf bytes.Buffer
	for _, rule := range rules {
		fmt.Fprintf(&buf, "%s|%s|%s|%s\n", rule.IPType, rule.Interface, rule.SourcePorts, rule.JumpPort)
	}
	return os.WriteFile(dportConfigFile, buf.Bytes(), 0600)
}

func dportWriteApplyScript() error {
	content := `#!/bin/bash
CONFIG_FILE="/etc/v2relay_dport.rules"

[ -f "$CONFIG_FILE" ] || exit 0

while IFS='|' read -r ip_type interface source_ports jump_port; do
    [ -n "$ip_type" ] || continue
    case "$ip_type" in
        \#*) continue ;;
    esac

    if [ "$ip_type" = "ipv4" ] || [ "$ip_type" = "dual" ]; then
        while iptables -t nat -D PREROUTING -i "$interface" -p udp --dport "$source_ports" -j DNAT --to-destination ":$jump_port" 2>/dev/null; do :; done
        iptables -t nat -A PREROUTING -i "$interface" -p udp --dport "$source_ports" -j DNAT --to-destination ":$jump_port"
    fi

    if [ "$ip_type" = "ipv6" ] || [ "$ip_type" = "dual" ]; then
        while ip6tables -t nat -D PREROUTING -i "$interface" -p udp --dport "$source_ports" -j DNAT --to-destination ":$jump_port" 2>/dev/null; do :; done
        ip6tables -t nat -A PREROUTING -i "$interface" -p udp --dport "$source_ports" -j DNAT --to-destination ":$jump_port"
    fi
done < "$CONFIG_FILE"
`
	return os.WriteFile(dportApplyBin, []byte(content), 0755)
}

func dportWriteSystemdService() error {
	content := fmt.Sprintf(`[Unit]
Description=v2relay UDP jump port rules
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
ExecStart=%s
RemainAfterExit=yes

[Install]
WantedBy=multi-user.target
`, dportApplyBin)
	if err := os.WriteFile(dportServiceFile, []byte(content), 0644); err != nil {
		return err
	}
	_ = run("systemctl", "daemon-reload")
	_ = run("systemctl", "enable", "v2relay-dport.service")
	return nil
}

func dportApplyAllRules() error {
	if err := dportWriteApplyScript(); err != nil {
		return err
	}
	if err := dportWriteSystemdService(); err != nil {
		return err
	}
	if err := run(dportApplyBin); err != nil {
		return err
	}
	return saveIptables()
}

func printPersistHint(name string) {
	fmt.Println(color(Green, "[保存] "+name+" 已立即生效。"))
	fmt.Println(color(Green, "[保存] 规则已写入本机持久化配置，VPS 重启后仍会自动恢复。"))
	fmt.Println(color(Green, "[提示] 转发代理由 iptables-persistent/netfilter 保存；跳跃端口由 /etc/v2relay_dport.rules + v2relay-dport.service 加载。"))
}

func (a *App) selectIPType() string {
	fmt.Println("请选择 IP Type:")
	fmt.Printf("  %s IPv4\n", menuNo("1."))
	fmt.Printf("  %s IPv6\n", menuNo("2."))
	fmt.Printf("  %s IPv4 + IPv6\n", menuNo("3."))
	choice := a.readLine("请输入选项 [1-3] (默认 3): ")
	if choice == "" {
		choice = "3"
	}
	switch choice {
	case "1":
		return "ipv4"
	case "2":
		return "ipv6"
	case "3":
		return "dual"
	default:
		return ""
	}
}

func dportTypeLabel(value string) string {
	switch value {
	case "ipv4":
		return "IPv4"
	case "ipv6":
		return "IPv6"
	case "dual":
		return "IPv4 + IPv6"
	default:
		return value
	}
}

func detectNetworkInterface() string {
	out, err := commandOutput("ip", "route")
	if err == nil {
		for _, line := range strings.Split(out, "\n") {
			fields := strings.Fields(line)
			for i := 0; i+1 < len(fields); i++ {
				if fields[i] == "dev" && strings.HasPrefix(line, "default") {
					return fields[i+1]
				}
			}
		}
	}

	out, err = commandOutput("ip", "link", "show")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, ": ") {
			continue
		}
		parts := strings.SplitN(line, ": ", 3)
		if len(parts) >= 2 && !strings.HasPrefix(parts[1], "lo") {
			return strings.TrimSpace(parts[1])
		}
	}
	return ""
}

func isValidPort(value string) bool {
	if value == "" {
		return false
	}
	port, err := strconv.Atoi(value)
	return err == nil && port >= 1 && port <= 65535 && strconv.Itoa(port) == value
}

func isValidPortRange(value string) bool {
	if isValidPort(value) {
		return true
	}
	parts := strings.Split(value, ":")
	if len(parts) != 2 || !isValidPort(parts[0]) || !isValidPort(parts[1]) {
		return false
	}
	start, _ := strconv.Atoi(parts[0])
	end, _ := strconv.Atoi(parts[1])
	return start <= end
}

func isValidIPType(value string) bool {
	return value == "ipv4" || value == "ipv6" || value == "dual"
}

func isValidIPv4CIDR(value string) bool {
	if value == "" {
		return false
	}
	if strings.Contains(value, "/") {
		ip, _, err := net.ParseCIDR(value)
		return err == nil && ip.To4() != nil
	}
	ip := net.ParseIP(value)
	return ip != nil && ip.To4() != nil
}

func (a *App) readLine(prompt string) string {
	fmt.Print(prompt)
	text, _ := a.reader.ReadString('\n')
	return strings.TrimSpace(text)
}

func (a *App) pause() {
	fmt.Print("按 Enter 返回菜单...")
	_, _ = a.reader.ReadString('\n')
}

func clearScreen() {
	fmt.Print("\033[H\033[2J")
}

func sleepSecond() {
	time.Sleep(time.Second)
}

type colorCode string

const (
	Red    colorCode = "\033[0;31m"
	Green  colorCode = "\033[0;32m"
	Yellow colorCode = "\033[0;33m"
	Cyan   colorCode = "\033[0;36m"
	NC     colorCode = "\033[0m"
)

func color(code colorCode, text string) string {
	return string(code) + text + string(NC)
}

func menuNo(text string) string {
	return color(Yellow, text)
}

func hasCommand(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func runQuiet(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	return cmd.Run()
}

func runEnv(env []string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func commandOK(name string, args ...string) bool {
	return runQuiet(name, args...) == nil
}

func commandOutput(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func printCommand(name string, args ...string) {
	out, err := commandOutput(name, args...)
	if err != nil {
		fmt.Print(out)
		return
	}
	fmt.Print(out)
}

func indentCommand(name string, args ...string) {
	out, err := commandOutput(name, args...)
	if err != nil {
		fmt.Println("    无法读取规则")
		return
	}
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		fmt.Println("    " + line)
	}
}

func printFilteredCommand(command []string, filters []string, emptyMessage string) {
	if len(command) == 0 {
		fmt.Println(emptyMessage)
		return
	}
	out, err := commandOutput(command[0], command[1:]...)
	if err != nil {
		fmt.Println(emptyMessage)
		return
	}
	found := false
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		ok := true
		for _, filter := range filters {
			if !strings.Contains(line, filter) {
				ok = false
				break
			}
		}
		if ok {
			fmt.Println(line)
			found = true
		}
	}
	if !found {
		fmt.Println(emptyMessage)
	}
}

func printInputChainJumps() {
	out, err := commandOutput("iptables", "-nL", "INPUT", "--line-numbers")
	if err != nil {
		fmt.Println("  无法读取 INPUT 链")
		return
	}
	found := false
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == chainName {
			fmt.Println("  " + line)
			found = true
		}
	}
	if !found {
		fmt.Println("  暂无跳转规则")
	}
}

func fetchText(url string, timeout time.Duration) string {
	data, err := fetchBytes(url, timeout)
	if err != nil {
		return ""
	}
	return string(data)
}

func fetchBytes(url string, timeout time.Duration) ([]byte, error) {
	client := &http.Client{Timeout: timeout}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %s", resp.Status)
	}
	return io.ReadAll(resp.Body)
}
