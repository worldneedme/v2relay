# v2relay

极简 Socks5 落地中继节点管理脚本，默认监听 `8848/tcp`。

## 特点

- 编译安装 MicroSocks，作为轻量 Socks5 出口。
- 使用 `V2RELAY` iptables 专用链，只接管配置端口，默认 `8848/tcp`。
- 白名单只允许指定前端机 IPv4/CIDR 访问。
- 不清空、不覆盖 VPS 上其它项目的防火墙规则。
- 安装后自动创建快捷命令：`v2relay`。
- 生成 WONDERX / Xray Socks5 出站配置和常用 AI 域名分流备忘。

## 一键安装

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/feinhunter/v2relay/main/install.sh)
```

如果系统没有 `curl`：

```bash
wget -qO- https://raw.githubusercontent.com/feinhunter/v2relay/main/install.sh | bash
```

## 使用

安装完成后，直接输入：

```bash
v2relay
```

菜单功能：

- 安装 v2relay，默认监听 `8848/tcp`。
- 添加前端节点 IP 到白名单。
- 移除前端节点 IP。
- 查看 systemd 状态和 `V2RELAY` 专用链规则。
- 卸载并清理 v2relay 管理的规则。
- 生成 Xray 出站配置。

## 防火墙行为

脚本会在 `INPUT` 链中添加一条只匹配当前 Socks5 端口的跳转规则，例如：

```text
tcp dpt:8848 -> V2RELAY
```

然后在 `V2RELAY` 链内维护白名单和默认拒绝：

```text
允许 前端机IP 访问 tcp/8848
拒绝 其它来源 访问 tcp/8848
```

其它端口不会进入 `V2RELAY` 链，因此不会影响 VPS 上已有的 SSH、网站、面板或其它项目端口。

## 卸载

在菜单中选择 `5`。

卸载会删除：

- `microsocks` systemd 服务
- `/usr/local/bin/microsocks`
- `/usr/local/bin/v2relay`
- `/etc/v2relay_node.conf`
- `V2RELAY` 专用链和对应 INPUT 跳转

不会清空整台机器的 iptables 规则。
