// netquotad — nftables 规则管理
package main

import (
	"fmt"
	"os/exec"
	"strings"
)

const (
	nftTableName   = "netquota"
	nftBlockSet    = "blocked_devices"
	nftCountChain  = "netquota_count"
	nftChainName   = "netquota_track"
)

// NFTableInit 初始化 nftables 表和规则
func NFTableInit() error {
	// 先删除旧 chain 避免重复规则
	exec.Command("sh", "-c", fmt.Sprintf("nft delete chain inet %s %s 2>/dev/null; true", nftTableName, nftChainName)).Run()
	exec.Command("sh", "-c", fmt.Sprintf("nft delete chain inet %s %s 2>/dev/null; true", nftTableName, nftCountChain)).Run()

	cmds := []string{
		fmt.Sprintf("nft add table inet %s 2>/dev/null; true", nftTableName),
		fmt.Sprintf("nft add set inet %s %s '{ type ipv4_addr; flags timeout; }' 2>/dev/null; true", nftTableName, nftBlockSet),
		// 清空旧数据，确保启动时状态干净
		fmt.Sprintf("nft flush set inet %s %s 2>/dev/null; true", nftTableName, nftBlockSet),
		// 主链：先 drop 阻断设备，再跳转到计数链
		fmt.Sprintf("nft add chain inet %s %s '{ type filter hook forward priority -5; }' 2>/dev/null; true", nftTableName, nftChainName),
		fmt.Sprintf("nft add rule inet %s %s ip saddr @%s drop 2>/dev/null; true", nftTableName, nftChainName, nftBlockSet),
		// 计数链：每个设备独立规则，由 jump 跳转进入（无独立 hook，只被主链 jump）
		fmt.Sprintf("nft add chain inet %s %s 2>/dev/null; true", nftTableName, nftCountChain),
		// 从主链跳转到计数链（阻断后的设备不会走到这里，因为已经被 drop 了）
		fmt.Sprintf("nft add rule inet %s %s jump %s 2>/dev/null; true", nftTableName, nftChainName, nftCountChain),
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
	// 阻断后从计数链移除该设备（不再需要计数）
	NFRemoveCounterRule(ip)
	return nil
}

// NFUnblockDevice 从黑名单移除
func NFUnblockDevice(ip string) error {
	cmd := exec.Command("nft", "delete", "element", "inet", nftTableName, nftBlockSet,
		fmt.Sprintf("{ %s }", ip))
	cmd.Run() // 忽略元素不存在的错误
	return nil
}

// NFTrackDevice 为设备添加独立的计数规则
func NFTrackDevice(ip string) error {
	// 先移除旧规则（避免重复），再添加新规则
	NFRemoveCounterRule(ip)
	cmd := exec.Command("nft", "add", "rule", "inet", nftTableName, nftCountChain,
		"ip", "saddr", ip, "counter", "accept")
	return cmd.Run()
}

// NFRemoveCounterRule 移除设备对应的计数规则
func NFRemoveCounterRule(ip string) {
	// 使用 nft -a 列出规则 handle，找到匹配的删除
	out, err := exec.Command("sh", "-c", fmt.Sprintf("nft -a list chain inet %s %s", nftTableName, nftCountChain)).Output()
	if err != nil {
		return
	}
	target := "ip saddr " + ip + " counter"
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, target) {
			continue
		}
		// 提取 handle 号
		parts := strings.Fields(line)
		for i, p := range parts {
			if p == "handle" && i+1 < len(parts) {
				handle := parts[i+1]
				exec.Command("nft", "delete", "rule", "inet", nftTableName, nftCountChain,
					"handle", handle).Run()
				return
			}
		}
	}
}

// NFGetCounter 获取设备的流量计数（从独立计数规则读取）
// 使用文本格式解析，避免 JSON 格式兼容问题
func NFGetCounter(ip string) (uint64, uint64, error) {
	cmd := exec.Command("nft", "list", "chain", "inet", nftTableName, nftCountChain)
	out, err := cmd.Output()
	if err != nil {
		return 0, 0, fmt.Errorf("获取计数器失败: %w", err)
	}
	return parseCounterForIP(string(out), ip)
}

// parseCounterForIP 从 nftables 文本输出解析指定 IP 的计数器
// 输出格式: ip saddr 192.168.124.119 counter packets 20 bytes 16709 accept
func parseCounterForIP(output, ip string) (uint64, uint64, error) {
	// 使用精确匹配：在行中查找 "ip saddr <IP> counter" 来避免 IP 子串误匹配
	target := "ip saddr " + ip + " counter"
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if !strings.Contains(line, target) {
			continue
		}
		// 找到了匹配行，提取 packets 和 bytes
		parts := strings.Fields(line)
		for i, p := range parts {
			if p == "bytes" && i+1 < len(parts) {
				bytes, _ := parseUint64(parts[i+1])
				// 往前找 packets
				for j := i - 1; j >= 0; j-- {
					if parts[j] == "packets" && j+1 < len(parts) {
						pkts, _ := parseUint64(parts[j+1])
						return pkts, bytes, nil
					}
				}
				return 0, bytes, nil
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

// NFResetCountChain 清空计数链并重新添加所有设备的规则
func NFResetCountChain(ips []string) error {
	// 清空计数链
	exec.Command("sh", "-c", fmt.Sprintf("nft flush chain inet %s %s 2>/dev/null; true", nftTableName, nftCountChain)).Run()
	// 重新添加所有设备的计数规则
	for _, ip := range ips {
		cmd := exec.Command("nft", "add", "rule", "inet", nftTableName, nftCountChain,
			"ip", "saddr", ip, "counter", "accept")
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("添加计数规则 %s 失败: %w", ip, err)
		}
	}
	return nil
}