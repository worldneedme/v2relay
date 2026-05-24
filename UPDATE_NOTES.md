本版更新内容：
1. 核心面板从 Bash 重构为 Go，长期维护更清晰，运行时是单文件二进制。
2. 保留 install.sh 作为轻量安装器，负责安装 Go 工具链、编译并安装 /usr/local/bin/v2relay。
3. 主菜单升级为一体化入口：转发代理、跳跃端口、全局规则总览、更新脚本。
4. 转发代理继续使用 TCP 8848 + V2RELAY 专用链，只接管本项目端口。
5. 新增跳跃端口模块，支持 UDP 端口范围跳到指定端口。
6. 跳跃端口新增 IP Type 选择：IPv4、IPv6、IPv4 + IPv6。
7. 跳跃端口规则写入 /etc/v2relay_dport.rules，并通过 v2relay-dport.service 开机加载。
8. 更新脚本会显示远程中文更新说明，确认后才执行更新。
