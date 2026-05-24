本版更新内容：
1. 安装器优化为低内存 VPS 友好模式：默认下载 Release 预编译二进制，不安装 Go。
2. 如果系统已有 curl/wget 和 tar/gzip，安装器不会执行 apt/yum/dnf，减少 256MB VPS 压力。
3. 新增 dnf、apk 包管理器识别，兼容 Rocky/Alma/Fedora/Alpine 等更多 Linux。
4. 安装前会显示系统、架构、安装方式和内存提示。
5. 只有手动指定 --build-from-source 时，才会安装 Go 工具链并从源码编译。
6. 操作完成后会中文提示：立即生效、已持久化、VPS 重启后自动恢复。
