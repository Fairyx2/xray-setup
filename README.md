# my-vpn

通过 SSH 在 Debian 服务器上运行的 Xray 初始化与管理 TUI。默认配置为 **VLESS + XHTTP + REALITY**，使用官方 Xray 发行文件和 systemd。

启动时选择 **简体中文 / English**，默认选中中文。界面文案及业务错误采用独立的中英翻译资源，操作系统和 Xray 返回的诊断保留原文。

## 构建与预览

需要 Go 1.24 或更高版本：

```bash
go mod download
make build
./dist/my-vpn --demo
```

`--demo` 可以在 macOS / Linux 上查看完整交互流程，不进行网络访问、服务操作或系统目录写入。原生 JSON 编辑仅使用会自动清理的临时文件。演示地址 `203.0.113.10` 不是真实节点。

```bash
./dist/my-vpn --demo --lang en
make check
make release
```

`make release` 生成 Linux amd64、arm64 两种架构的可执行文件。源码按模块组织，服务器只需要对应的可执行程序，无需安装 Go。

推送形如 `v0.1.0` 的 Git 标签会触发 GitHub Actions：先执行 race 测试和 `go vet`，再构建 Linux amd64 / arm64，生成 SHA-256 校验和，并自动创建 GitHub Release。也可以在 GitHub Actions 页面手动运行 `Release`，填写要发布的标签。

## 从 GitHub Release 下载到服务器

服务器不需要拉取源码或安装 Go，直接下载与你的 VPS 架构匹配的 Release 文件即可。下面以 `v0.1.0` 为例；发布新版本时替换版本号。下载前可先在服务器执行 `dpkg --print-architecture`，`amd64` 使用 amd64 文件，`arm64` 使用 arm64 文件。

```bash
VERSION=v0.1.0
ARCH=amd64  # arm64 VPS 改为 arm64
# 当前仓库是 Private：先在服务器临时设置一个只有仓库读取权限的 token。
export GITHUB_TOKEN='替换为你的 GitHub token'
curl -fL --retry 3 \
  -H "Authorization: Bearer ${GITHUB_TOKEN}" \
  -H 'Accept: application/octet-stream' \
  -o /usr/local/bin/my-vpn \
  "https://github.com/Fairyx2/my-vpn/releases/download/${VERSION}/my-vpn-linux-${ARCH}"
chmod 0755 /usr/local/bin/my-vpn
/usr/local/bin/my-vpn --version
```

如需校验下载内容，可同时下载 `checksums.txt`，并在同一目录验证：

```bash
cd /usr/local/bin
curl -fL \
  -H "Authorization: Bearer ${GITHUB_TOKEN}" \
  -o checksums.txt \
  "https://github.com/Fairyx2/my-vpn/releases/download/${VERSION}/checksums.txt"
grep "my-vpn-linux-${ARCH}$" checksums.txt | sha256sum -c -
```

然后直接运行 `/usr/local/bin/my-vpn`。如果把仓库改为 Public，可以去掉两个 `Authorization` 头；也可以先在本地下载，再通过 `scp` 传到服务器。不要把 token 写进下载 URL 或提交到仓库。

## 在服务器运行

支持原生 Debian + systemd，amd64 / arm64，要求 root 权限、可用的 CA 证书和能访问 GitHub 的出站网络。服务操作使用 Debian 的 `systemctl`、`journalctl`、`useradd`、`runuser`；端口占用详情使用 `ss`。精简系统如缺少这些工具，应先安装对应的 `systemd`、`passwd`、`util-linux`、`iproute2`、`ca-certificates` 包。容器中未启动 systemd 时不支持实际部署。

例如，将已构建的 amd64 程序传到 VPS：

```bash
scp dist/my-vpn-linux-amd64 root@YOUR_VPS_IP:/root/my-vpn
ssh root@YOUR_VPS_IP
chmod +x /root/my-vpn
/root/my-vpn
```

1. 选择语言和简单 / 高级模式。
2. 自动检查环境，识别公网 IPv4，检测 REALITY 候选站，生成凭据。
3. 默认使用 TCP 443；被占用时说明占用情况，依次推荐 8443、9443、10443，均不可用则由系统分配空闲端口。最终部署时再次检查，保留原占用进程。
4. 查看推荐配置。公网地址识别失败时填写地址；目标站不适用时修改目标。自动识别的是公网出口，存在 NAT 时需确认入站地址和端口映射。
5. 确认后下载官方最新正式版，校验 `.dgst` 中的 SHA-256，以 Xray 检查候选配置，再写入并启动服务。后续修改沿用已安装版本，不自动升级核心。
6. 在云安全组及系统防火墙中放行所选 TCP 端口。工具只提供指引，不修改防火墙、SSH 或 BBR。
7. 查看分享链接，在 v2rayN / v2rayNG 中导入；电脑选择 Xray 核心。分享页按 `p` 会退出 TUI，并在终端打印完整链接，方便复制。

本机服务和 TCP 检查通过后，还应使用客户端验证出口 IP；目标站 TLS 1.3 / HTTP/2 检测只用于筛选候选，不能保证所有网络下连通。

## 两种模式

**简单模式**只提供连接地址、监听端口、REALITY 目标三个可修改项。目标站省略端口时使用 443，同时同步 SNI。UUID、密钥、Short ID、路径等自动生成；重复运行和修改配置时保留已有凭据。

**高级模式**额外开放 SNI、监听 IPv4、UUID、私钥、Short ID、路径、XHTTP 模式、客户端指纹和日志级别。所有字段已有默认值，清空后保存会沿用打开表单时的值。私钥输入始终隐藏，公钥根据私钥自动计算。

高级模式还可使用 `$VISUAL` / `$EDITOR`（默认查找 nano / vi）编辑**完整原生 Xray JSON**：

- 编辑的是 0600 临时副本，退出编辑器后先校验 JSON 并同步参数，点击部署才修改实际配置。
- 保留 `tag: "my-vpn"` 的主入站，使用 VLESS + XHTTP + REALITY、`decryption: "none"`、空 flow；第一个用户、SNI、Short ID 用于主节点分享。
- 可添加 routing、DNS、额外用户或其他原生字段。后续表单修改保留未知字段、额外入站和其他用户，不重新生成整份模板。
- 第一版不提供多个入站的独立菜单管理。额外入站可在原生 JSON 中配置，但分享页仅导出主节点，辅助端口需自行放行。
- 主节点的 XHTTP `host` 必须为空，不支持主节点 `downloadSettings` 上下行分离；其他 XHTTP 参数随分享链接的 `extra` 输出。复杂客户端参数的导入兼容性仍需客户端验证。
- 自定义路由引用的 geodata / 其他文件需自行提供，建议使用绝对路径；默认安装不下载 geodata。服务使用 `ProtectHome`、`ProtectSystem` 和 `PrivateTmp`，需要文件写入的高级功能可能需要额外 systemd 配置。
- 编辑器变量支持简单的空白分隔参数，例如 `EDITOR="nano -w"`，不执行 shell 展开，不支持带空格的编辑器路径或复杂引号。

## 按键

| 页面 | 操作 |
| --- | --- |
| 菜单 | ↑ / ↓、Tab、Enter；数字直接选择；Esc 返回；q 退出 |
| 长页面 | PgUp / PgDn 翻页 |
| 参数表单 | Tab / ↑ / ↓ 切换；左右、Home / End 定位；Ctrl+U 清空；Ctrl+S 保存；Esc 放弃 |
| 日志 / 配置预览 | ↑ / ↓、PgUp / PgDn 滚动；Enter / Esc 返回 |
| 正在部署 | Ctrl+C 请求取消，等待恢复完成后继续操作 |
| 分享页 | p 退出并输出链接；Esc 返回 |

## 文件与服务

| 内容 | 位置 |
| --- | --- |
| 官方 Xray 二进制 | `/usr/local/lib/my-vpn/xray` |
| 生效配置 | `/etc/my-vpn/xray.json`，root:my-vpn，0640 |
| 私有状态 | `/var/lib/my-vpn/state.json`，root:root，0600 |
| 修改前备份 | `/var/lib/my-vpn/backups/`，0700；快照文件为 0600 |
| systemd 单元 | `/etc/systemd/system/my-vpn-xray.service` |
| 专用服务账号 | `my-vpn`，系统账号、无登录 shell |
| 操作锁 | `/run/my-vpn.lock`，进程退出后内核释放锁 |

检测到常见位置已有 Xray、已有配置或同名单元时停止安装，不接管第三方面板。其他程序仍可使用这台服务器；端口冲突通过更换本工具端口处理。

```bash
systemctl status my-vpn-xray --no-pager
journalctl -u my-vpn-xray -n 80 --no-pager
```

## 失败与恢复

候选配置先通过真实 Xray 检查，写入后再以专用服务账号检查读取权限。实际切换前保存配置、单元、状态快照；首次安装还记录二进制文件原先不存在。失败或取消时恢复原文件，并尝试恢复服务原有的运行与开机启动状态。恢复使用独立超时上下文，取消安装不会连带取消恢复。

自动恢复不覆盖账号及目录创建：失败后可能保留空目录和专用系统账号。首次安装成功后清理无旧文件的快照；后续配置变更保留快照，包含敏感凭据，需要自行控制保留数量。

强制断电、`kill -9` 或磁盘故障不保证能自动恢复。备份目录有 `manifest.json`，记录原路径、文件是否存在、权限及属主，`service-state.json` 记录原运行及开机启动状态；发生此类中断时可据此恢复文件，再执行 `systemctl daemon-reload` 并恢复服务状态。当前没有一键恢复 / 升级 / 卸载菜单。

## 模块结构

```text
cmd/my-vpn/          程序入口、启动参数、信号处理
internal/config/     参数、原生配置结构体、默认模板、局部更新、分享与脱敏
internal/server/     环境检测、网络检测、官方发行下载、部署事务、服务管理
internal/system/     受限命令执行、原子写入、路径与终端输出处理
internal/tui/        页面状态机、参数表单、原生编辑器、界面渲染
internal/i18n/       中英词典、翻译和占位符检查
internal/message/    与语言无关的业务消息及错误
```

配置结构体和安装 / 恢复等关键逻辑均带中文注释。

## 验证

```bash
go test ./...
go vet ./...
go test -race ./...
```

测试覆盖配置及分享一致性、原生扩展保留、敏感值脱敏、参数错误、校验文件篡改、压缩包提取、备份恢复、进程锁、端口冲突、中英文键及占位符一致性、取消状态和小终端渲染。测试使用临时目录和模拟 HTTP，不操作真实 systemd。

如有对应本机架构的官方 Xray，可额外执行模板集成检查：

```bash
XRAY_TEST_BINARY=/absolute/path/to/xray go test ./internal/config -run TestOfficialXrayConfig -v
```

项目首次交付仍需在目标 Debian VPS 上完成真实安装及客户端连通验证；本机预览不能代替服务器实测。
