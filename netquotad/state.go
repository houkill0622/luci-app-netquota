// netquotad — 设备时长统计状态管理
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// DeviceState 设备当前状态
type DeviceState struct {
	MAC          string `json:"mac"`
	Name         string `json:"name"`
	IP           string `json:"ip"`
	Quota        int    `json:"quota"`    // 每日额度（分钟）
	UsedMinutes  int    `json:"used"`     // 已用时长（分钟）
	Blocked      bool   `json:"blocked"`  // 是否被阻断
	LastSeen     int64  `json:"last_seen"` // 最后活跃时间戳
	LastBytes    uint64 `json:"last_bytes"` // 上一次的流量计数
	Enabled      bool   `json:"enabled"`
	Mode         int    `json:"mode"`
}

// StateManager 状态管理器
type StateManager struct {
	mu         sync.RWMutex
	devices    map[string]*DeviceState // key: MAC地址
	stateFile  string
	configPath string
	today      string // 今天的日期 YYYY-MM-DD，用于判断是否需要重置
}

// NewStateManager 创建状态管理器
func NewStateManager(stateDir, configPath string) *StateManager {
	os.MkdirAll(stateDir, 0755)
	sm := &StateManager{
		devices:    make(map[string]*DeviceState),
		stateFile:  filepath.Join(stateDir, "state.json"),
		configPath: configPath,
		today:      time.Now().Format("2006-01-02"),
	}
	sm.loadState()
	return sm
}

// SyncConfig 从 UCI 配置同步设备列表
func (sm *StateManager) SyncConfig(cfg *AppConfig) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// 添加配置中的设备
	for _, dc := range cfg.Devices {
		if _, exists := sm.devices[dc.MAC]; !exists {
			sm.devices[dc.MAC] = &DeviceState{
				MAC:     dc.MAC,
				Name:    dc.Name,
				Quota:   dc.Quota,
				Mode:    dc.Mode,
				Enabled: dc.Enabled,
			}
		} else {
			// 更新配置
			dev := sm.devices[dc.MAC]
			dev.Name = dc.Name
			dev.Quota = dc.Quota
			dev.Mode = dc.Mode
			dev.Enabled = dc.Enabled
		}
	}
}

// GetDevice 获取设备状态
func (sm *StateManager) GetDevice(mac string) *DeviceState {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.devices[mac]
}

// GetAllDevices 获取所有设备状态
func (sm *StateManager) GetAllDevices() []DeviceState {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	result := make([]DeviceState, 0, len(sm.devices))
	for _, d := range sm.devices {
		result = append(result, *d)
	}
	return result
}

// UpdateDeviceIP 更新设备IP
func (sm *StateManager) UpdateDeviceIP(mac, ip string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if d, ok := sm.devices[mac]; ok {
		d.IP = ip
	}
}

// IncrementUsed 增加设备已用时长
func (sm *StateManager) IncrementUsed(mac string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if d, ok := sm.devices[mac]; ok {
		d.UsedMinutes++
	}
}

// SetBlocked 设置设备阻断状态
func (sm *StateManager) SetBlocked(mac string, blocked bool) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if d, ok := sm.devices[mac]; ok {
		d.Blocked = blocked
	}
}

// UpdateLastBytes 更新设备最新的流量计数
func (sm *StateManager) UpdateLastBytes(mac string, bytes uint64) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if d, ok := sm.devices[mac]; ok {
		d.LastBytes = bytes
	}
}

// UpdateLastSeen 更新设备最后活跃时间
func (sm *StateManager) UpdateLastSeen(mac string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if d, ok := sm.devices[mac]; ok {
		d.LastSeen = time.Now().Unix()
	}
}

// AddDevice 添加新设备
func (sm *StateManager) AddDevice(mac, name string, quota, mode int) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.devices[mac] = &DeviceState{
		MAC:     mac,
		Name:    name,
		Quota:   quota,
		Mode:    mode,
		Enabled: true,
	}
}

// UpdateDevice 更新设备属性（保留 used、last_seen 等运行时状态）
func (sm *StateManager) UpdateDevice(mac string, name *string, quota, mode *int, enabled *bool) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if d, ok := sm.devices[mac]; ok {
		if name != nil {
			d.Name = *name
		}
		if quota != nil {
			d.Quota = *quota
		}
		if mode != nil {
			d.Mode = *mode
		}
		if enabled != nil {
			d.Enabled = *enabled
		}
	}
}

// SetUsedMinutes 强制设置已用时长（加奖励/扣时间用）
func (sm *StateManager) SetUsedMinutes(mac string, used int) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if d, ok := sm.devices[mac]; ok {
		if used < 0 {
			used = 0
		}
		d.UsedMinutes = used
		// 如果已用时长小于配额，自动解封
		if d.Quota > 0 && used < d.Quota && d.Blocked {
			d.Blocked = false
		}
	}
}

// RemoveDevice 移除设备
func (sm *StateManager) RemoveDevice(mac string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	delete(sm.devices, mac)
}

// NeedReset 检查是否需要每日重置
func (sm *StateManager) NeedReset() bool {
	today := time.Now().Format("2006-01-02")
	return today != sm.today
}

// ResetDaily 每日重置所有设备的已用时长和阻断状态
func (sm *StateManager) ResetDaily() {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.today = time.Now().Format("2006-01-02")
	for _, d := range sm.devices {
		d.UsedMinutes = 0
		d.Blocked = false
		d.LastBytes = 0
	}
	sm.saveState()
}

// saveState 保存状态到文件
func (sm *StateManager) saveState() {
	data := struct {
		Today   string                 `json:"today"`
		Devices map[string]*DeviceState `json:"devices"`
	}{
		Today:   sm.today,
		Devices: sm.devices,
	}
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return
	}
	os.WriteFile(sm.stateFile, b, 0644)
}

// loadState 从文件加载状态
func (sm *StateManager) loadState() {
	b, err := os.ReadFile(sm.stateFile)
	if err != nil {
		return // 文件不存在，使用空状态
	}
	var data struct {
		Today   string                 `json:"today"`
		Devices map[string]*DeviceState `json:"devices"`
	}
	if err := json.Unmarshal(b, &data); err != nil {
		return
	}
	sm.today = data.Today
	for k, v := range data.Devices {
		sm.devices[k] = v
	}
}

// Save 立即保存状态
func (sm *StateManager) Save() {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.saveState()
}