// netquotad — nftables 规则管理
package main

import (
	"fmt"
	"os/exec"
	"strings"
)

const (
	nftTableName  = "netquota"
	nftBlockSet   = "blocked_devices"
	nftCountSet   = "tracked_devices"
	nftChainName  = "netquota_track"
)

// NFTableInit 初始化 nftables 表和规则
func NFTableInit() error {
	// 先删除旧 chain 避免重复规则
	exec.Command("sh", "-c", fmt.Sprintf("nft delete chain inet %s %s 2>/dev/null; true", nftTableName, nftChainName)).Run()

	cmds := []string{
		fmt.Sprintf("nft add table inet %s 2>/dev/null; true", nftTableName),
		fmt.Sprintf("nft add set inet %s %s '{ type ipv4_addr; flags timeout; }' 2>/dev/null; true", nftTableName, nftBlockSet),
		fmt.Sprintf("nft add set inet %s %s '{ type ipv4_addr; }' 2>/dev/null; true", nftTableName, nftCountSet),
		fmt.Sprintf("nft add chain inet %s %s '{ type filter hook forward priority -5; }' 2>/dev/null; true", nftTableName, nftChainName),
		fmt.Sprintf("nft add rule inet %s %s ip saddr @%s counter accept 2>/dev/null; true", nftTableName, nftChainName, nftCountSet),
		fmt.Sprintf("nft add rule inet %s %s ip saddr @%s drop 2>/dev/null; true", nftTableName, nftChainName, nftBlockSet),
	}

	for i, cmd := range cmds {
		if err := exec.Command("sh", "-c", cmd).Run(); err != nil {
			if i < 4 {
				return fmt.Errorf("nftables 初始化步骤 %d 失败: %w", i+1, err)
			}
		}
	}
	return nil
}

// NFBlockDevice 将设备加入黑名单
func NFBlockDevice(ip string) error {
	cmd := exec.Command("nft", "add", "element", "inet", nftTableName, nftBlockSet,
		fmt.Sprintf("{ %s }", ip))
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("阻断设备 %s 失败: %s: %w", ip, strings.TrimSpace(string(out)), err)
	}
	return nil
}

// NFUnblockDevice 从黑名单移除
func NFUnblockDevice(ip string) error {
	cmd := exec.Command("nft", "delete", "element", "inet", nftTableName, nftBlockSet,
		fmt.Sprintf("{ %s }", ip))
	cmd.Run() // 忽略元素不存在的错误
	return nil
}

// NFTrackDevice 将设备添加到流量跟踪集合
func NFTrackDevice(ip string) error {
	cmd := exec.Command("nft", "add", "element", "inet", nftTableName, nftCountSet,
		fmt.Sprintf("{ %s }", ip))
	return cmd.Run()
}

// NFGetCounter 获取设备的流量计数
// nftables 规则是 per-packet counter，所以返回累计包数和字节数
func NFGetCounter(ip string) (uint64, uint64, error) {
	// 列出监控链的规则和计数器
	cmd := exec.Command("nft", "--json", "list", "chain", "inet", nftTableName, nftChainName)
	out, err := cmd.Output()
	if err != nil {
		// 回退到文本格式
		cmd2 := exec.Command("nft", "list", "chain", "inet", nftTableName, nftChainName)
		out2, err2 := cmd2.Output()
		if err2 != nil {
			return 0, 0, fmt.Errorf("获取计数器失败: %w", err2)
		}
		return parseCounterForIPText(string(out2), ip)
	}
	return parseCounterForIPJSON(string(out), ip)
}

// parseCounterForIPJSON 从 nftables JSON 输出解析指定 IP 的流量
func parseCounterForIPJSON(output, ip string) (uint64, uint64, error) {
	// JSON 格式复杂，回退到文本解析
	return parseCounterForIPText(output, ip)
}

// parseCounterForIPText 从 nftables 文本输出解析
func parseCounterForIPText(output, ip string) (uint64, uint64, error) {
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.Contains(line, ip) && strings.Contains(line, "counter") {
			parts := strings.Fields(line)
			for i, p := range parts {
				if p == "bytes" && i+1 < len(parts) {
					bytes, _ := parseUint64(parts[i+1])
					for j, q := range parts {
						if q == "packets" && j+1 < len(parts) {
							pkts, _ := parseUint64(parts[j+1])
							return pkts, bytes, nil
						}
					}
					return 0, bytes, nil
				}
			}
		}
	}
	return 0, 0, nil
}

func parseUint64(s string) (uint64, error) {
	var val uint64
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		val = val*10 + uint64(c-'0')
	}
	return val, nil
}

// NFIsBlocked 检查设备是否被阻断
func NFIsBlocked(ip string) (bool, error) {
	cmd := exec.Command("nft", "get", "element", "inet", nftTableName, nftBlockSet,
		fmt.Sprintf("{ %s }", ip))
	err := cmd.Run()
	return err == nil, nil
}

// NFListBlocked 列出所有被阻断的设备 IP
func NFListBlocked() ([]string, error) {
	cmd := exec.Command("nft", "list", "set", "inet", nftTableName, nftBlockSet)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("列出阻断设备失败: %w", err)
	}

	var ips []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, ".") {
			parts := strings.Fields(line)
			if len(parts) > 0 {
				ip := strings.Trim(parts[0], " ,")
				if strings.Count(ip, ".") == 3 {
					ips = append(ips, ip)
				}
			}
		}
	}
	return ips, nil
}

// NFTeardown 清理 nftables 规则
func NFTeardown() error {
	exec.Command("nft", "delete", "table", "inet", nftTableName).Run()
	return nil
}