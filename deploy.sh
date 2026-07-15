#!/bin/bash
# deploy.sh — 部署 luci-app-netquota 到软路由
set -e

ROUTER="root@192.168.124.188"
SSH_PORT="22"
# Try alternate port if default fails
if ! ssh -o ConnectTimeout=2 -p $SSH_PORT "$ROUTER" "echo ok" 2>/dev/null; then
	SSH_PORT="10000"
fi

echo "连接软路由: $ROUTER (端口 $SSH_PORT)"

# 1. 交叉编译
echo "=== 编译 aarch64 二进制 ==="
cd /opt/data/luci-app-netquota

GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="-s -w" -o /tmp/netquotad_arm64 ./netquotad
echo "  netquotad: OK"

cd /opt/data/luci-app-netquota/netquotad-cli
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="-s -w" -o /tmp/netquotad-cli_arm64 .
echo "  netquotad-cli: OK"

cd /opt/data/luci-app-netquota

# 2. 部署二进制文件
echo "=== 部署二进制文件 ==="
ssh -p $SSH_PORT "$ROUTER" "mkdir -p /usr/bin /etc/config /etc/init.d /etc/rpcd/acl.d /usr/share/luci/menu.d /www/luci-static/resources/view/netquota /var/lib/netquotad"

scp -P $SSH_PORT /tmp/netquotad_arm64 "$ROUTER:/usr/bin/netquotad"
scp -P $SSH_PORT /tmp/netquotad-cli_arm64 "$ROUTER:/usr/bin/netquotad-cli"
ssh -p $SSH_PORT "$ROUTER" "chmod 755 /usr/bin/netquotad /usr/bin/netquotad-cli"
echo "  二进制文件已部署"

# 3. 部署 init 脚本
echo "=== 部署 init 脚本 ==="
scp -P $SSH_PORT root/etc/init.d/netquotad "$ROUTER:/etc/init.d/netquotad"
ssh -p $SSH_PORT "$ROUTER" "chmod 755 /etc/init.d/netquotad"
echo "  init 脚本已部署"

# 4. 部署 LuCI 文件
echo "=== 部署 LuCI 界面 ==="
scp -P $SSH_PORT htdocs/luci-static/resources/view/netquota/overview.js "$ROUTER:/www/luci-static/resources/view/netquota/overview.js"
scp -P $SSH_PORT root/usr/share/luci/menu.d/luci-app-netquota.json "$ROUTER:/usr/share/luci/menu.d/luci-app-netquota.json"
scp -P $SSH_PORT root/etc/rpcd/acl.d/netquota.json "$ROUTER:/etc/rpcd/acl.d/netquota.json"
echo "  LuCI 界面已部署"

# 5. 创建 UCI 配置（如果不存在）
echo "=== 配置 ==="
ssh -p $SSH_PORT "$ROUTER" "bash -c '
if [ ! -f /etc/config/netquota ]; then
	cat > /etc/config/netquota <<-EOF
# netquotad 配置
# 设备每日联网时长管理

config settings
	option reset_hour \"0\"

EOF
	echo \"  UCI 配置已创建\"
else
	echo \"  UCI 配置已存在\"
fi
'"

# 6. 启动服务
echo "=== 启动服务 ==="
ssh -p $SSH_PORT "$ROUTER" "bash -c '
	/etc/init.d/netquotad stop 2>/dev/null || true
	sleep 1
	/etc/init.d/netquotad enable
	/etc/init.d/netquotad start
	sleep 2
	echo \"  netquotad 状态:\"
	/usr/bin/netquotad-cli status 2>/dev/null || echo \"  等待启动...\"
'"

# 7. 重启 rpcd 加载 ACL
echo "=== 重启 rpcd 加载 ACL ==="
ssh -p $SSH_PORT "$ROUTER" "/etc/init.d/rpcd restart 2>/dev/null || true"

echo ""
echo "=== 部署完成 ==="
echo "请在浏览器中刷新 LuCI 界面：服务 → 网络时长管理"