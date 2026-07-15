<div align="center">

# 🕐 luci-app-netquota

**OpenWrt 每日网络时长管理插件**  
*Parental control — 给孩子一个健康的网络环境*

[![GitHub release](https://img.shields.io/github/v/release/houkill0622/luci-app-netquota)](https://github.com/houkill0622/luci-app-netquota/releases)
[![License](https://img.shields.io/github/license/houkill0622/luci-app-netquota)](LICENSE)
[![OpenWrt](https://img.shields.io/badge/OpenWrt-23.05+-00b4ff)](https://openwrt.org)
[![Go](https://img.shields.io/badge/Go-1.21+-00ADD8)](https://go.dev)

</div>

---

## 📖 简介

**luci-app-netquota** 是一款 OpenWrt 插件，用于**管理家庭设备每日上网时长**。  
你可以为孩子的 iPad、手机、电脑等设备设置每日上网时长配额，超时自动断网（仅断外网，内网正常），次日凌晨自动恢复。

> 💡 **适用场景**：控制孩子上网时间、限制游戏时长、培养良好上网习惯

---

## ✨ 功能特性

### 🎯 核心功能
| 功能 | 说明 |
|------|------|
| **每日配额** | 5/10/15/20/30/60/120/240 分钟或无限制 |
| **自动阻断** | 配额用完后自动断外网（nftables），内网正常访问 |
| **每日重置** | 北京时间 00:00 自动清零所有设备统计 |
| **设备管理** | 手动添加/扫描局域网/删除设备 |
| **编辑设备** | 修改名称、配额、模式，加奖励或扣减时间 |

### ⏱ 三种计时模式
| 模式 | 名称 | 说明 |
|:----:|------|------|
| 1 | **连网即计时** | 设备连接 WiFi 就开始计时（适合严格管控） |
| 2 | **有流量才计时** | 设备有实际流量（>100KB/分钟）才计时（推荐） |
| 3 | **智能模式** | 综合判断，低阈值检测流量 |

### 🖥 LuCI 界面
- 位于 **服务 → 网络时长管理**
- 实时显示：设备总数、在线设备、已阻断数量
- 设备列表：名称、MAC、IP、状态、配额、已用、剩余
- 一键扫描局域网在线设备，批量勾选添加
- 每 15 秒自动刷新设备状态

### 🛠 CLI 命令行
```bash
netquotad-cli status                    # 查看整体状态
netquotad-cli devices list              # 列出所有设备
netquotad-cli devices add <mac> <name> [quota] [mode]  # 添加设备
netquotad-cli devices update <mac> <json>  # 更新设备
netquotad-cli devices delete <mac>      # 删除设备
netquotad-cli scan                      # 扫描局域网
netquotad-cli block <mac> [ip]          # 手动阻断
netquotad-cli unblock <mac> [ip]        # 解除阻断
netquotad-cli reset                     # 手动重置今日统计
```

---

## 🏗 架构

```
┌─────────────────────────────────────────────────┐
│                   LuCI 浏览器界面                  │
│          (服务 → 网络时长管理)                     │
└──────────────────────┬──────────────────────────┘
                       │ HTTP (rpcd)
                       ▼
┌─────────────────────────────────────────────────┐
│              netquotad-cli (CLI)                  │
│          Unix Socket ↔ HTTP 客户端                │
└──────────────────────┬──────────────────────────┘
                       │ Unix Socket
                       ▼
┌─────────────────────────────────────────────────┐
│              netquotad (Go 守护进程)               │
│ ┌─────────┐ ┌──────────┐ ┌───────────────────┐  │
│ │ 状态管理  │ │ 监控循环   │ │   RPC 服务        │  │
│ │ state.go│ │ monitor.go│ │   rpc.go          │  │
│ └─────────┘ └────┬─────┘ └───────────────────┘  │
│                  │                                │
│         ┌────────┴────────┐                       │
│         │  nftables 规则管理│                      │
│         │  nftables.go    │                       │
│         └────────┬────────┘                       │
└──────────────────┼────────────────────────────────┘
                   │ nft
                   ▼
┌─────────────────────────────────────────────────┐
│              nftables 内核防火墙                   │
│  table inet netquota                              │
│  ├── set tracked_devices    { type ipv4_addr }    │
│  ├── set blocked_devices    { type ipv4_addr }    │
│  └── chain netquota_track   (hook forward -5)    │
│       ├── ip saddr @tracked → counter accept     │
│       └── ip saddr @blocked → drop               │
└─────────────────────────────────────────────────┘
```

### 项目结构
```
luci-app-netquota/
├── netquotad/                    # Go 后端守护进程
│   ├── main.go                  # 主入口
│   ├── config.go                # 配置解析（JSON/UCI）
│   ├── state.go                 # 设备状态管理
│   ├── monitor.go               # 监控循环（60s间隔）
│   ├── nftables.go              # nftables 规则管理
│   └── rpc.go                   # RPC 服务（Unix Socket + TCP 9800）
├── netquotad-cli/               # 命令行工具
│   └── main.go
├── htdocs/                      # LuCI 前端资源
│   └── luci-static/resources/view/netquota/overview.js
├── root/                        # OpenWrt 包文件
│   └── etc/
│       ├── init.d/netquotad     # procd 启动脚本
│       └── rpcd/acl.d/netquota.json  # LuCI 权限配置
├── deploy/                      # 部署相关
├── Makefile                     # OpenWrt 包 Makefile
├── deploy.sh                    # 一键部署脚本
├── README.md
└── LICENSE
```

---

## 📦 安装

### 前置条件
- OpenWrt 23.05+ / Kwrt 24.10+（aarch64 / x86_64）
- nftables（OpenWrt 默认已包含）
- Go 1.21+（仅交叉编译需要）

### 方法一：快速部署（推荐）

在编译机上操作：

```bash
# 1. 克隆仓库
git clone https://github.com/houkill0622/luci-app-netquota.git
cd luci-app-netquota

# 2. 交叉编译（以 aarch64 为例）
cd netquotad && GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -o netquotad .
cd ../netquotad-cli && GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -o netquotad-cli .

# 3. 上传到路由器（替换 192.168.1.1 为你的路由器 IP）
scp netquotad/netquotad root@192.168.1.1:/usr/bin/
scp netquotad-cli/netquotad-cli root@192.168.1.1:/usr/bin/
scp -r htdocs/* root@192.168.1.1:/www/
scp root/etc/init.d/netquotad root@192.168.1.1:/etc/init.d/

# 4. 在路由器上启动
ssh root@192.168.1.1 "/etc/init.d/netquotad enable && /etc/init.d/netquotad start"
```

**x86_64 编译：**
```bash
GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o netquotad .
```

### 方法二：OpenWrt SDK 编译（制作 .ipk）

> 待完善 —— 欢迎贡献 Makefile 集成

---

## 🎮 使用指南

### 首次使用
1. 登录 OpenWrt LuCI 界面
2. 进入 **服务 → 网络时长管理**
3. 点击 **扫描局域网** 自动发现在线设备
4. 勾选要管控的设备，点 **添加选中**
5. 在设备列表中点击 **编辑** 调整配额和时间模式

### 编辑设备
点击设备行右侧的 **编辑** 按钮，可以：
- ✏️ 修改设备名称（备注）
- ⏱ 调整每日配额（0 = 无限制）
- 🔄 切换计时模式
- 🎁 **+5分钟奖励** — 给孩子加时间
- ⛔ **-5分钟扣减** — 超时惩罚
- 🔄 **重置已用时长** — 清零重新计

### 手动添加设备
如果设备不在线，可以手动添加：
- 点击 **添加设备**
- 输入 MAC 地址（如 `AA:BB:CC:DD:EE:FF`）
- 输入设备名称
- 设置配额和模式

---

## ⚙️ 配置说明

配置文件位于 `/etc/config/netquota`（UCI 格式）：

```bash
# 查看当前配置
cat /etc/config/netquota

# 通过 UCI 命令修改
uci set netquota.settings.reset_hour=0
uci commit netquota
```

设备配置示例：
```
config device
    option mac 'AA:BB:CC:DD:EE:FF'
    option name '宝宝iPad'
    option quota '30'
    option mode '2'
    option enabled '1'
```

---

## 🔧 开发指南

### 本地开发
```bash
# 构建
cd netquotad && go build -o netquotad .

# 运行（独立模式，非 OpenWrt 环境）
./netquotad -config /path/to/config.json -state /tmp/netquotad
```

### 调试
```bash
# 查看守护进程日志
logread | grep netquotad

# 查看 nftables 规则
nft list table inet netquota

# 查看设备状态
netquotad-cli status
netquotad-cli devices list
netquotad-cli config
```

---

## 📋 版本计划

- [x] **v1.0** — 核心功能：设备管理、配额、阻断、重置、LuCI 界面
- [ ] **v1.1** — 周末/节假日模式、奖励时间、暂停功能
- [ ] **v1.2** — 统计图表、使用记录、通知推送
- [ ] **v2.0** — 多用户分组、家长模式、OpenAppFilter 联动
- [ ] **v2.1** — REST API、家庭日历、QR 码临时放行

---

## 🤝 贡献

欢迎提交 Issue 和 Pull Request！

1. Fork 本仓库
2. 创建特性分支 (`git checkout -b feature/amazing-feature`)
3. 提交改动 (`git commit -m 'Add amazing feature'`)
4. 推送到分支 (`git push origin feature/amazing-feature`)
5. 提交 Pull Request

---

## 📄 License

[MIT](LICENSE) © 2026 houkill0622

---

<div align="center">
  <sub>Built with ❤️ for a better digital parenting experience</sub>
</div>