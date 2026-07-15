// netquotad — 核心监控循环
package main

import (
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"
)

const (
	TrafficThreshold    = 100 * 1024 // 流量阈值：100KB
	MonitorInterval     = 60         // 监控间隔：60秒
	ArpTablePath        = "/proc/net/arp"
)

// Monitor 监控器
type Monitor struct {
	state  *StateManager
	config *AppConfig
}

// NewMonitor 创建监控器
func NewMonitor(state *StateManager, config *AppConfig) *Monitor {
	return &Monitor{
		state:  state,
		config: config,
	}
}

// Run 启动监控主循环
func (m *Monitor) Run(stop chan struct{}) {
	log.Println("[netquotad] 监控循环已启动，间隔 60 秒")

	// 初始化 nftables
	if err := NFTableInit(); err != nil {
		log.Printf("[netquotad] nftables 初始化失败: %v", err)
		return
	}
	log.Println("[netquotad] nftables 初始化完成")

	// 首次同步
	m.refreshConfig()
	m.syncDeviceTracking()
	m.syncBlockedState()

	ticker := time.NewTicker(MonitorInterval * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			log.Println("[netquotad] 监控循环已停止")
			return
		case <-ticker.C:
			m.tick()
		}
	}
}

// tick 每分钟执行一次的主逻辑
func (m *Monitor) tick() {
	// 1. 检查是否需要每日重置
	if m.state.NeedReset() {
		log.Println("[netquotad] 新的一天，重置所有设备统计")
		m.resetDaily()
	}

	// 2. 刷新配置和设备IP
	m.refreshConfig()
	m.scanARP()

	// 3. 检查每个设备的流量和时长
	devices := m.state.GetAllDevices()
	for _, dev := range devices {
		if !dev.Enabled {
			continue
		}
		if dev.IP == "" {
			continue // 设备不在线
		}
		if dev.Quota == 0 {
			continue // 无限制
		}

		m.checkDevice(&dev)
	}

	// 4. 保存状态
	m.state.Save()
}

// checkDevice 检查单个设备的时长和流量
func (m *Monitor) checkDevice(dev *DeviceState) {
	// 已阻断的设备不计时（阻断后流量包被 nftables drop 了，不需要再计）
	if dev.Blocked {
		// 但依然要检查是否需要解封（手动调整了时长）
		if dev.Quota > 0 && dev.UsedMinutes < dev.Quota {
			log.Printf("[netquotad] %s(%s) 已用时长调整为 %d/%d，自动解封", dev.Name, dev.IP, dev.UsedMinutes, dev.Quota)
			NFUnblockDevice(dev.IP)
			m.state.SetBlocked(dev.MAC, false)
		}
		return
	}

	// 获取当前流量计数
	_, bytes, err := NFGetCounter(dev.IP)
	if err != nil {
		log.Printf("[netquotad] 获取 %s(%s) 流量失败: %v", dev.Name, dev.IP, err)
		return
	}

	delta := bytes - dev.LastBytes
	shouldCount := false

	switch dev.Mode {
	case 1:
		// 模式1：连接WiFi即计时（只要设备在线就计）
		shouldCount = true
	case 2:
		// 模式2：有流量才计时（推荐）
		shouldCount = delta >= TrafficThreshold
	case 3:
		// 模式3：智能模式（综合判断）
		shouldCount = delta >= TrafficThreshold/2 // 简化为更低阈值
	}

	if shouldCount {
		m.state.UpdateLastBytes(dev.MAC, bytes)
		m.state.UpdateLastSeen(dev.MAC)
		m.state.IncrementUsed(dev.MAC)

		// 检查是否达到配额
		if dev.UsedMinutes+1 >= dev.Quota {
			log.Printf("[netquotad] %s(%s) 已达到 %d 分钟额度，阻断网络", dev.Name, dev.IP, dev.Quota)
			if err := NFBlockDevice(dev.IP); err != nil {
				log.Printf("[netquotad] 阻断失败: %v", err)
			} else {
				m.state.SetBlocked(dev.MAC, true)
			}
		}
	} else {
		// 无流量，更新最后活跃时间
		m.state.UpdateLastSeen(dev.MAC)
	}
}

// refreshConfig 重新加载配置
func (m *Monitor) refreshConfig() {
	cfg, err := ParseConfig(m.config.ConfigPath)
	if err != nil {
		log.Printf("[netquotad] 加载配置失败: %v", err)
		return
	}
	m.config = cfg
	m.state.SyncConfig(cfg)
}

// scanARP 扫描 ARP 表获取设备 IP
func (m *Monitor) scanARP() {
	data, err := exec.Command("cat", ArpTablePath).Output()
	if err != nil {
		return
	}

	devices := m.state.GetAllDevices()
	deviceMap := make(map[string]bool)
	for _, d := range devices {
		deviceMap[d.MAC] = true
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		ip := fields[0]
		mac := strings.ToUpper(fields[3])

		if deviceMap[mac] {
			m.state.UpdateDeviceIP(mac, ip)
			m.state.UpdateLastSeen(mac)
		}
	}
}

// syncDeviceTracking 同步设备到 nftables 跟踪集合
func (m *Monitor) syncDeviceTracking() {
	devices := m.state.GetAllDevices()
	for _, dev := range devices {
		if dev.IP != "" && dev.Enabled {
			NFTrackDevice(dev.IP)
		}
	}
}

// syncBlockedState 同步阻断状态到 nftables（启动时确保一致性）
func (m *Monitor) syncBlockedState() {
	devices := m.state.GetAllDevices()
	for _, dev := range devices {
		if dev.Blocked && dev.IP != "" {
			log.Printf("[netquotad] 同步阻断状态: %s(%s)", dev.Name, dev.IP)
			NFBlockDevice(dev.IP)
		}
	}
}

// resetDaily 每日重置
func (m *Monitor) resetDaily() {
	m.state.ResetDaily()

	// 清理所有 nftables 阻断规则
	blocked, err := NFListBlocked()
	if err == nil {
		for _, ip := range blocked {
			NFUnblockDevice(ip)
		}
	}

	// 重新初始化 nftables
	NFTableInit()
	m.syncDeviceTracking()
	log.Println("[netquotad] 每日重置完成")
}

// DetectActiveDevices 自动扫描局域网内活跃设备
func (m *Monitor) DetectActiveDevices() ([]DetectedDevice, error) {
	// 执行 arp-scan 或读取 ARP 表
	data, err := exec.Command("cat", ArpTablePath).Output()
	if err != nil {
		return nil, fmt.Errorf("读取ARP表失败: %w", err)
	}

	var devices []DetectedDevice
	seen := make(map[string]bool)

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		ip := fields[0]
		mac := strings.ToUpper(fields[3])
		hw := fields[2] // 硬件类型

		if mac == "00:00:00:00:00:00" || mac == "" || seen[mac] {
			continue
		}
		if strings.Count(ip, ".") != 3 {
			continue
		}
		seen[mac] = true

		// 尝试获取主机名
		hostname := resolveHostname(ip)

		devices = append(devices, DetectedDevice{
			MAC:      mac,
			IP:       ip,
			Name:     hostname,
			HWType:   hw,
			Online:   true,
		})
	}
	return devices, nil
}

// DetectedDevice 扫描到的设备信息
type DetectedDevice struct {
	MAC      string `json:"mac"`
	IP       string `json:"ip"`
	Name     string `json:"name"`
	Vendor   string `json:"vendor"`
	HWType   string `json:"hw_type"`
	Online   bool   `json:"online"`
}

func resolveHostname(ip string) string {
	// 尝试通过 DNS 反向解析获取主机名
	cmd := exec.Command("nslookup", ip)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	output := string(out)
	// 解析 name = xxx
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "name =") {
			parts := strings.Split(line, "=")
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return ""
}