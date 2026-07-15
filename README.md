# luci-app-netquota

OpenWrt LuCI application for **daily network time quota management** (parental control).

## Features

- 🕐 **Daily Time Quota** — Set daily internet time limits (5/10/15/20/30/60/120/240 min or unlimited)
- 📱 **Device Management** — Auto-scan LAN devices, manual add by MAC, name/IP/vendor display
- ⏱ **Three Timing Modes**:
  - Mode 1: Count time when WiFi connected
  - Mode 2: Count time only when traffic > 100KB/min (recommended)
  - Mode 3: Smart mode
- 🚫 **Auto Block** — Automatically block WAN access via nftables when quota exhausted (LAN stays accessible)
- 🔄 **Daily Auto Reset** — Resets all counters at 00:00 Beijing time
- ✏️ **Edit Devices** — Modify name, quota, mode after creation, add bonus or penalty time
- 🖥 **LuCI Web UI** — Clean interface under 服务 → 网络时长管理

## Architecture

```
luci-app-netquota/
├── netquotad/           # Go backend daemon
│   ├── main.go         # Entry point
│   ├── config.go       # Configuration parsing (JSON/UCI)
│   ├── state.go        # Device state management
│   ├── monitor.go      # Traffic monitoring loop
│   ├── nftables.go     # nftables rule management
│   └── rpc.go          # RPC server (Unix socket + TCP)
├── netquotad-cli/       # CLI tool for daemon communication
│   └── main.go
├── htdocs/              # LuCI frontend resources
│   └── luci-static/resources/view/netquota/overview.js
├── root/                # OpenWrt package files
│   └── etc/
│       ├── init.d/netquotad
│       └── rpcd/acl.d/netquota.json
├── Makefile             # OpenWrt package Makefile
└── deploy.sh            # Deployment script
```

## Installation

### Prerequisites
- OpenWrt 23.05+ / Kwrt 24.10+
- nftables
- Go 1.21+ (for cross-compilation)

### Quick Deploy
```bash
# Cross-compile for aarch64
cd netquotad && GOOS=linux GOARCH=arm64 go build -o netquotad .
cd ../netquotad-cli && GOOS=linux GOARCH=arm64 go build -o netquotad-cli .

# Copy to router
scp netquotad/netquotad root@192.168.1.1:/usr/bin/
scp netquotad-cli/netquotad-cli root@192.168.1.1:/usr/bin/

# Copy LuCI files
scp -r htdocs/* root@192.168.1.1:/www/

# Copy init script
scp root/etc/init.d/netquotad root@192.168.1.1:/etc/init.d/

# On router
ssh root@192.168.1.1 "/etc/init.d/netquotad enable && /etc/init.d/netquotad start"
```

## License

MIT