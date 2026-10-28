# xray-setup

`xray-setup` 是一个运行在 Debian VPS 上的 Xray 初始化与管理工具。它用终端交互界面（TUI）把 **VLESS + XHTTP + REALITY** 的常见部署流程串起来：检查环境、生成凭据、选择端口、下载并校验官方 Xray、写入 systemd 服务、输出客户端分享链接。

它适合希望快速搭建个人代理节点、又不想手动维护 Xray JSON 和 systemd 单元的用户。Windows / macOS 可使用 v2rayN，Android 可使用 v2rayNG 导入生成的 VLESS 链接。

项目不会改善 VPS 到中国大陆的线路质量，也不保证所有网络都能连通；REALITY 目标站、VPS 出口和当地运营商线路仍需要实际验证。工具不接管第三方面板已有的 Xray 配置，不修改 SSH、防火墙或 BBR。

## 适用场景

- 新装 Debian VPS 上首次部署 Xray。
- 443 已被网站占用，需要选择 8443、9443 或其他空闲 TCP 端口。
- 希望用简单模式自动生成 UUID、密钥、Short ID 和 XHTTP 路径。
- 需要高级模式编辑完整的原生 Xray JSON，添加 routing、DNS、额外用户或其他字段。
- 已经部署后，查看服务状态、修改节点参数或重新输出分享链接。

## 功能概览

- 简体中文 / English 双语，启动时可选择语言，默认中文。
- 简单模式和高级模式；高级模式保留未知原生 JSON 字段。
- 自动检测端口占用，默认优先使用 TCP 443。
- 通过 Xray 官方发行版下载地址获取核心，并校验 SHA-256。
- systemd 服务、专用服务账号、配置权限和变更前备份。
- 失败或取消时尽量恢复配置、单元文件和原有服务状态。
- Linux amd64 / arm64 预编译 Release，以及 GitHub Actions 自动打包。

## 直接下载 Release

服务器不需要拉取源码、不需要安装 Go，直接下载对应架构的预编译文件即可。当前仓库是 Public，不需要 GitHub Token。

默认命令（以 root 用户登录时使用，无需 `sudo`）：

```bash
VERSION=v0.1.0
ARCH=$(dpkg --print-architecture)  # amd64 或 arm64

curl -fL --retry 3 \
  -o /usr/local/bin/xray-setup \
  "https://github.com/Fairyx2/xray-setup/releases/download/${VERSION}/xray-setup-linux-${ARCH}" \
  && chmod 0755 /usr/local/bin/xray-setup \
  && /usr/local/bin/xray-setup --version
```

如果写入 `/usr/local/bin` 时提示 `Permission denied` 或伴随权限错误的 `curl: (23)`，说明当前用户没有该目录的写入权限，改用下面的命令。它先下载到临时文件，再通过 `sudo` 安装；下载失败时不会继续安装：

```bash
VERSION=v0.1.0
ARCH=$(dpkg --print-architecture)  # amd64 或 arm64
DOWNLOAD_FILE=$(mktemp)

curl -fL --retry 3 \
  -o "$DOWNLOAD_FILE" \
  "https://github.com/Fairyx2/xray-setup/releases/download/${VERSION}/xray-setup-linux-${ARCH}" \
  && sudo install -m 0755 "$DOWNLOAD_FILE" /usr/local/bin/xray-setup \
  && rm -f "$DOWNLOAD_FILE" \
  && /usr/local/bin/xray-setup --version
```

此方式需要当前用户有 `sudo` 权限；如果系统没有 `sudo` 或当前用户无权使用，请切换到 root 后执行默认命令。

校验下载内容（沿用上面的 `VERSION` 和 `ARCH`；两种安装方式均可使用，无需 `sudo`）：

```bash
CHECKSUM_FILE=$(mktemp)
curl -fL --retry 3 \
  -o "$CHECKSUM_FILE" \
  "https://github.com/Fairyx2/xray-setup/releases/download/${VERSION}/checksums.txt" \
  && grep "xray-setup-linux-${ARCH}$" "$CHECKSUM_FILE" | \
    sed "s|xray-setup-linux-${ARCH}$|/usr/local/bin/xray-setup|" | sha256sum -c - \
  && rm -f "$CHECKSUM_FILE"
```

也可以在本机下载后用 `scp` 传到服务器：

```bash
scp xray-setup-linux-amd64 root@YOUR_VPS_IP:/root/xray-setup
```

Release 页面：[github.com/Fairyx2/xray-setup/releases](https://github.com/Fairyx2/xray-setup/releases)

## 在服务器启动

要求：Debian、root 权限、systemd、可访问 GitHub 的出站网络，以及 `ca-certificates`、`iproute2` 等常用工具。容器中没有运行 systemd 时不能完成实际部署。

默认以 root 用户启动：

```bash
/usr/local/bin/xray-setup
```

普通用户启动时，如果提示需要 root 权限，使用：

```bash
sudo /usr/local/bin/xray-setup
```

启动后按向导操作：

1. 选择语言和简单 / 高级模式。
2. 自动检查服务器、公网 IPv4、443 端口和 REALITY 候选目标。
3. 默认使用 443；如果被占用，界面会显示占用信息并推荐其他空闲端口。
4. 确认地址、端口和目标站，简单模式下其他凭据自动生成。
5. 确认部署，工具下载官方 Xray、校验文件、检查配置并启动 systemd 服务。
6. 在云安全组和系统防火墙中放行最终使用的 TCP 端口。
7. 在分享页面复制链接，导入 v2rayN / v2rayNG；Windows / macOS 客户端选择 Xray 核心。

常用管理命令：

```bash
systemctl status my-vpn-xray --no-pager
journalctl -u my-vpn-xray -n 80 --no-pager
/usr/local/bin/xray-setup
```

`--demo` 仅用于本机预览，不访问网络、不写入服务器目录：

```bash
/usr/local/bin/xray-setup --demo
/usr/local/bin/xray-setup --demo --lang en
```

## 简单模式与高级模式

简单模式只需要确认连接地址、监听端口和 REALITY 目标；UUID、PrivateKey、公钥、Short ID、XHTTP 路径等由程序生成，并在已有配置上尽量保留。

高级模式提供 SNI、监听地址、UUID、私钥、Short ID、路径、XHTTP 模式、客户端指纹和日志级别等字段。还可以通过 `$VISUAL` / `$EDITOR` 编辑完整原生 Xray JSON。主入站使用 `tag: "my-vpn"`，这是为了兼容已有配置和服务文件，仓库及命令名称已经改为 `xray-setup`。

高级 JSON 可以添加 routing、DNS、额外用户和额外入站；分享页只自动导出主入站。自定义 geodata 或其他文件需要自行放置，并确保 systemd 沙箱权限满足需求。

## 从源码运行

需要 Go 1.24 或更高版本：

```bash
git clone https://github.com/Fairyx2/xray-setup.git
cd xray-setup
go mod download
go run ./cmd/my-vpn --demo
```

也可以先构建再运行：

```bash
make build
./dist/xray-setup --demo
```

## 本地测试与打包

```bash
# 格式、依赖和常规检查
go test ./...
go vet ./...
go test -race ./...

# 构建当前系统的开发版本
make build

# 交叉构建服务器 Release 文件
make release VERSION=0.1.0
ls -lh dist/xray-setup-linux-*
```

`make release` 会生成：

- `dist/xray-setup-linux-amd64`
- `dist/xray-setup-linux-arm64`

推送 `v*` 标签会触发 [.github/workflows/release.yml](.github/workflows/release.yml)：运行测试和 `go vet`，交叉构建两个架构，生成 `checksums.txt`，并自动创建 GitHub Release。

```bash
git add .
git commit -m "描述本次修改"
git push origin main

git tag -a v0.1.0 -m "Release v0.1.0"
git push origin v0.1.0
```

## 目录结构

```text
cmd/my-vpn/          程序入口和启动参数；构建产物名称为 xray-setup
internal/config/     配置结构体、默认模板、局部更新、分享与脱敏
internal/server/     环境检测、网络检测、Xray 下载、部署事务、服务管理
internal/system/     受限命令执行、原子写入、路径和终端输出
internal/tui/        页面状态机、参数表单、原生 JSON 编辑器和渲染
internal/i18n/       中英文翻译资源
internal/message/    与语言无关的业务消息及错误
```

服务器上的配置和服务路径暂时保留 `my-vpn` 前缀，用于兼容已经部署的版本：

| 内容 | 位置 |
| --- | --- |
| 官方 Xray 二进制 | `/usr/local/lib/my-vpn/xray` |
| 生效配置 | `/etc/my-vpn/xray.json` |
| 私有状态 | `/var/lib/my-vpn/state.json` |
| 备份目录 | `/var/lib/my-vpn/backups/` |
| systemd 单元 | `/etc/systemd/system/my-vpn-xray.service` |
| 服务账号 | `my-vpn` |

## 按键

| 页面 | 操作 |
| --- | --- |
| 菜单 | ↑ / ↓、Tab、Enter；数字直接选择；Esc 返回；q 退出 |
| 长页面 | PgUp / PgDn 翻页 |
| 参数表单 | Tab / ↑ / ↓ 切换；左右、Home / End 定位；Ctrl+U 清空；Ctrl+S 保存；Esc 放弃 |
| 日志 / 配置预览 | ↑ / ↓、PgUp / PgDn 滚动；Enter / Esc 返回 |
| 正在部署 | Ctrl+C 请求取消，等待恢复完成后继续操作 |
| 分享页 | p 退出并在终端输出链接；Esc 返回 |

## 许可证与实测范围

当前仓库尚未声明开源许可证，代码使用前请先确认你的使用范围。项目测试使用临时目录和模拟 HTTP，不会替代目标 Debian VPS 上的真实安装及客户端连通验证。
