// netquotad — IPv6 阻断支持
// 原版只处理 IPv4（ip saddr @blocked_devices drop）。
// 本文件补充 IPv6：通过 NDP（ip -6 neigh）反查被阻断设备的
// 全球单播地址（GUA），写入 blocked_devices_v6 集合并 drop。
// 注意：设备使用 IPv6 隐私扩展时会轮换地址，本函数在监控周期内
// 幂等重建集合，轮换窗口 ≤ 一个 tick。
package main

import (
	"fmt"
	"log"
	"net"
	"os/exec"
	"strings"
)

const (
	nftBlockSetV6 = "blocked_devices_v6"
	ipv6ElemTO    = "24h" // 集合元素超时，兜底防残留
)

// ensureV6 确保v6 集合和 drop 规则存在（幂等）
func ensureV6() {
	exec.Command("sh", "-c", fmt.Sprintf(
		"nft add set inet %s %s '{ type ipv6_addr; flags timeout; }' 2>/dev/null; true",
		nftTableName, nftBlockSetV6)).Run()

	// 规则不存在才加，避免重复
	out, _ := exec.Command("nft", "list", "chain", "inet", nftTableName, nftChainName).Output()
	if !strings.Contains(string(out), nftBlockSetV6) {
		exec.Command("sh", "-c", fmt.Sprintf(
			"nft add rule inet %s %s ip6 saddr @%s drop",
			nftTableName, nftChainName, nftBlockSetV6)).Run()
	}
}

// isGUA 是否为全球单播地址（2000::/3），排除链路本地 fe80::/10 等
func isGUA(addr string) bool {
	ip := net.ParseIP(addr)
	if ip == nil || ip.To16() == nil {
		return false
	}
	return ip.To16()[0]&0xE0 == 0x20
}

// resolveGUAByMAC 从 NDP 邻居表反查某 MAC 的所有 IPv6 GUA
func resolveGUAByMAC(mac string) []string {
	out, err := exec.Command("ip", "-6", "neigh", "show").Output()
	if err != nil {
		return nil
	}
	mac = strings.ToUpper(mac)
	var addrs []string
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		// 期望格式: <addr> dev <if> lladdr <MAC> <state>
		if len(fields) < 4 {
			continue
		}
		if fields[2] != "lladdr" || strings.ToUpper(fields[3]) != mac {
			continue
		}
		if isGUA(fields[0]) {
			addrs = append(addrs, fields[0])
		}
	}
	return addrs
}

// SyncBlockedIPv6 幂等同步：清空 v6 集合，把当前所有被阻断设备的
// GUA 重新写入。任何 block/unblock 事件和每个监控周期都应调用。
func SyncBlockedIPv6(state *StateManager) {
	ensureV6()

	var addrs []string
	for _, dev := range state.GetAllDevices() {
		if dev.Blocked {
			addrs = append(addrs, resolveGUAByMAC(dev.MAC)...)
		}
	}

	exec.Command("nft", "flush", "set", "inet", nftTableName, nftBlockSetV6).Run()
	for _, a := range addrs {
		cmd := exec.Command("nft", "add", "element", "inet", nftTableName, nftBlockSetV6,
			fmt.Sprintf("{ %s timeout %s }", a, ipv6ElemTO))
		if out, err := cmd.CombinedOutput(); err != nil {
			log.Printf("[netquotad] IPv6 阻断 %s 失败: %s: %v", a, strings.TrimSpace(string(out)), err)
		}
	}

	if len(addrs) > 0 {
		log.Printf("[netquotad] IPv6 同步: 已阻断 %d 个地址", len(addrs))
	}
}

// NFListBlockedV6 列出 v6 集合中的地址
func NFListBlockedV6() ([]string, error) {
	out, err := exec.Command("nft", "list", "set", "inet", nftTableName, nftBlockSetV6).Output()
	if err != nil {
		return nil, fmt.Errorf("列出 IPv6 阻断设备失败: %w", err)
	}
	var addrs []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, ":") && !strings.Contains(line, "elements") &&
			!strings.Contains(line, "timeout") && !strings.Contains(line, "type") {
			parts := strings.Fields(line)
			if len(parts) > 0 {
				a := strings.Trim(parts[0], " ,")
				if isGUA(strings.Split(a, " ")[0]) {
					addrs = append(addrs, a)
				}
			}
		}
	}
	return addrs, nil
}
