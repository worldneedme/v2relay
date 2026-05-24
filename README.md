# v2relay

Go 版一体化 VPS 转发管理面板。

它把两个能力合在一个入口里：

- 转发代理：MicroSocks 落地 Socks5，默认监听 `8848/tcp`。
- 跳跃端口：UDP 源端口或端口范围跳到指定目标端口，支持 IPv4 / IPv6 / 双栈。

## 一键安装

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/worldneedme/v2relay/main/install.sh)
```

如果系统没有 `curl`：

```bash
wget -qO- https://raw.githubusercontent.com/worldneedme/v2relay/main/install.sh | bash
```

安装器默认下载 GitHub Release 预编译二进制，并安装到：

```text
/usr/local/bin/v2relay
```

普通 VPS 不会安装 Go 环境。如果系统已有 `curl` 或 `wget`，并且已有 `tar/gzip`，安装器也不会执行包管理器安装，适合 256MB 低内存 VPS。

安装器会自动识别：

- 包管理器：`apt-get` / `dnf` / `yum` / `apk`
- 架构：`linux_amd64` / `linux_arm64` / `linux_armv7`
- 系统和内存，并在安装前显示检测结果

只有手动指定源码编译时才会安装 Go：

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/worldneedme/v2relay/main/install.sh) --build-from-source
```

以后直接输入：

```bash
v2relay
```

## 主菜单

```text
1. 转发代理 (Socks5 落地节点)
2. 跳跃端口 (UDP 端口范围转发)
3. 全局规则总览
4. 更新脚本 (显示中文更新内容)
0. 退出面板
```

## 持久化逻辑

转发代理模块：

- 当前立即写入 `iptables filter/INPUT`。
- 只让配置端口进入 `V2RELAY` 专用链，默认是 `8848/tcp`。
- 不清空、不覆盖 VPS 上其它项目端口。
- 规则通过 `iptables-persistent` / `netfilter-persistent` 保存。
- VPS 重启后规则仍会恢复。

跳跃端口模块：

- 当前立即写入 `iptables/ip6tables nat/PREROUTING`。
- 规则保存到 `/etc/v2relay_dport.rules`。
- 开机加载脚本：`/usr/local/bin/v2relay-dport-apply.sh`。
- systemd 服务：`/etc/systemd/system/v2relay-dport.service`。
- VPS 重启后由 `v2relay-dport.service` 自动恢复跳跃端口规则。

每次添加、删除或重载规则后，面板都会提示：

```text
[保存] 规则已立即生效。
[保存] 规则已写入本机持久化配置，VPS 重启后仍会自动恢复。
```

## 更新

菜单选择 `4`。

更新前会显示远程中文更新说明，确认后才会下载并安装新版预编译二进制。
