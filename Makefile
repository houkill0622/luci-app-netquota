#
# Copyright (C) 2024 HOSKILL
#
# This is free software, licensed under MIT.
#

include $(TOPDIR)/rules.mk

PKG_NAME:=luci-app-netquota
PKG_VERSION:=1.0.0
PKG_RELEASE:=1

PKG_BUILD_DIR:=$(BUILD_DIR)/$(PKG_NAME)
PKG_MAINTAINER:=HOSKILL <hoskill@example.com>
PKG_LICENSE:=MIT
PKG_LICENSE_FILES:=LICENSE

include $(INCLUDE_DIR)/package.mk
include $(INCLUDE_DIR)/golang.mk

# Go 编译设置
GO_PKG:=github.com/hoskill/luci-app-netquota/netquotad
GO_PKG_BUILD_PKG:=github.com/hoskill/luci-app-netquota/netquotad
GO_PKG_LDFLAGS:=-s -w
GO_PKG_INSTALL_DIR:=$(PKG_BUILD_DIR)/root/usr/bin

define Package/luci-app-netquota
	SECTION:=luci
	CATEGORY:=LuCI
	SUBMENU:=3. Applications
	TITLE:=网络时长管理 - 每日设备联网时长配额
	URL:=https://github.com/hoskill/luci-app-netquota
	DEPENDS:=+rpcd +luci-base +nftables +nftables-json
	PKGARCH:=all
endef

define Package/luci-app-netquota/description
	OpenWrt 每日联网时长管理插件。
	支持设置设备每日上网时长配额，超时自动阻断外网访问。
	提供三种计时模式：连网即计时、有流量才计时、智能模式。
	每日 00:00 自动重置。
endef

define Build/Prepare
	$(call Build/Prepare/Default)
	# 复制 Go 源码
	cp -r ./netquotad $(PKG_BUILD_DIR)/
endef

define Build/Compile
	# 编译 netquotad 守护程序
	cd $(PKG_BUILD_DIR)/netquotad && \
		GOOS=linux GOARCH=$(ARCH) CGO_ENABLED=0 \
		go build -ldflags="-s -w" -o $(PKG_BUILD_DIR)/root/usr/bin/netquotad .
	# 编译 netquotad-cli 命令行工具
	cd $(PKG_BUILD_DIR)/netquotad-cli && \
		GOOS=linux GOARCH=$(ARCH) CGO_ENABLED=0 \
		go build -ldflags="-s -w" -o $(PKG_BUILD_DIR)/root/usr/bin/netquotad-cli .
endef

define Package/luci-app-netquota/install
	$(INSTALL_DIR) $(1)/usr/bin
	$(INSTALL_BIN) $(PKG_BUILD_DIR)/root/usr/bin/netquotad $(1)/usr/bin/
	$(INSTALL_BIN) $(PKG_BUILD_DIR)/root/usr/bin/netquotad-cli $(1)/usr/bin/

	$(INSTALL_DIR) $(1)/etc/init.d
	$(INSTALL_BIN) ./root/etc/init.d/netquotad $(1)/etc/init.d/

	$(INSTALL_DIR) $(1)/etc/rpcd/acl.d
	$(INSTALL_DATA) ./root/etc/rpcd/acl.d/netquota.json $(1)/etc/rpcd/acl.d/

	$(INSTALL_DIR) $(1)/usr/share/luci/menu.d
	$(INSTALL_DATA) ./root/usr/share/luci/menu.d/luci-app-netquota.json $(1)/usr/share/luci/menu.d/

	$(INSTALL_DIR) $(1)/www/luci-static/resources/view/netquota
	$(INSTALL_DATA) ./htdocs/luci-static/resources/view/netquota/overview.js \
		$(1)/www/luci-static/resources/view/netquota/overview.js
endef

define Package/luci-app-netquota/postinst
#!/bin/sh
if [ -z "$${IPKG_INSTROOT}" ]; then
	# 重启 rpcd 加载 ACL
	/etc/init.d/rpcd restart 2>/dev/null || true
	# 启动 netquotad
	/etc/init.d/netquotad enable 2>/dev/null || true
	/etc/init.d/netquotad start 2>/dev/null || true
fi
exit 0
endef

define Package/luci-app-netquota/prerm
#!/bin/sh
if [ -z "$${IPKG_INSTROOT}" ]; then
	/etc/init.d/netquotad stop 2>/dev/null || true
	/etc/init.d/netquotad disable 2>/dev/null || true
fi
exit 0
endef

$(eval $(call BuildPackage,luci-app-netquota))