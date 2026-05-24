本版更新内容：
1. 默认安装改为直接下载单文件二进制，不再需要 tar/gzip 解压，进一步适配 256MB VPS。
2. 默认安装不安装 Go，不跑源码编译；只有 --build-from-source 才会安装 Go 工具链。
3. 如果系统已有 curl 或 wget，安装器不会执行 apt/yum/dnf/apk，减少低内存 VPS 压力。
4. 新增 dnf、apk 包管理器识别，兼容 Rocky/Alma/Fedora/Alpine 等更多 Linux。
5. 安装前会显示系统、架构、安装方式和内存提示。
6. 操作完成后会中文提示：立即生效、已持久化、VPS 重启后自动恢复。
