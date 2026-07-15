// netquotad — OpenWrt 每日联网时长管理守护程序
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	DefaultConfigPath = "/etc/netquotad/config.json"
	DefaultStateDir   = "/var/lib/netquotad"
)

// DeviceConfig 设备规则配置
type DeviceConfig struct {
	MAC     string `json:"mac"`
	Name    string `json:"name"`
	Quota   int    `json:"quota"`   // 每日额度（分钟）, 0=无限制
	Mode    int    `json:"mode"`    // 1=连网即计时, 2=有流量才计时, 3=智能模式
	Enabled bool   `json:"enabled"`
}

// AppConfig 应用配置
type AppConfig struct {
	Devices    []DeviceConfig `json:"devices"`
	ResetHour  int            `json:"reset_hour"` // 重置小时（北京时间）
	StateDir   string
	ConfigPath string
}

// ParseConfig 解析配置文件（自动检测 JSON 或 UCI 格式）
func ParseConfig(path string) (*AppConfig, error) {
	cfg := &AppConfig{
		ResetHour:  0,
		StateDir:   DefaultStateDir,
		ConfigPath: path,
	}

	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("打开配置文件失败: %w", err)
	}
	defer f.Close()

	// 检测格式：读取前2个非空字符
	buf := make([]byte, 2)
	n, _ := f.Read(buf)
	f.Seek(0, 0)
	firstNonSpace := byte(0)
	for _, b := range buf[:n] {
		if b != ' ' && b != '\t' && b != '\n' && b != '\r' {
			firstNonSpace = b
			break
		}
	}

	if firstNonSpace == '{' {
		return parseJSONConfig(f, cfg)
	}
	return parseUCIConfig(f, cfg)
}

// parseJSONConfig 解析 JSON 格式配置
func parseJSONConfig(f *os.File, cfg *AppConfig) (*AppConfig, error) {
	var data struct {
		Devices   []DeviceConfig `json:"devices"`
		ResetHour int            `json:"reset_hour"`
	}
	if err := json.NewDecoder(f).Decode(&data); err != nil {
		return cfg, fmt.Errorf("JSON 配置解析失败: %w", err)
	}
	cfg.Devices = data.Devices
	if data.ResetHour > 0 {
		cfg.ResetHour = data.ResetHour
	}
	return cfg, nil
}

// parseUCIConfig 解析 UCI 格式配置
func parseUCIConfig(f *os.File, cfg *AppConfig) (*AppConfig, error) {
	var currentDevice *DeviceConfig
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		raw := strings.TrimSpace(scanner.Text())
		if raw == "" || strings.HasPrefix(raw, "#") {
			continue
		}

		// 处理 config 节
		if strings.HasPrefix(raw, "config") {
			if currentDevice != nil {
				cfg.Devices = append(cfg.Devices, *currentDevice)
				currentDevice = nil
			}
			parts := strings.Fields(raw)
			if len(parts) >= 2 {
				typ := strings.Trim(parts[1], "'\"")
				if typ == "device" {
					currentDevice = &DeviceConfig{Enabled: true}
				}
			}
			continue
		}

		// 处理 option 行
		if currentDevice != nil {
			optName, optVal := parseOption(raw)
			switch optName {
			case "mac":
				currentDevice.MAC = strings.ToUpper(optVal)
			case "name":
				currentDevice.Name = optVal
			case "quota":
				currentDevice.Quota, _ = strconv.Atoi(optVal)
			case "mode":
				currentDevice.Mode, _ = strconv.Atoi(optVal)
			case "enabled":
				currentDevice.Enabled = optVal == "1" || optVal == "true"
			}
		} else {
			optName, optVal := parseOption(raw)
			switch optName {
			case "reset_hour":
				cfg.ResetHour, _ = strconv.Atoi(optVal)
			}
		}
	}

	if currentDevice != nil {
		cfg.Devices = append(cfg.Devices, *currentDevice)
	}
	return cfg, scanner.Err()
}

func parseOption(line string) (string, string) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "option") {
		return "", ""
	}
	rest := strings.TrimSpace(strings.TrimPrefix(line, "option"))
	parts := splitOption(rest)
	if len(parts) < 2 {
		return "", ""
	}
	name := parts[0]
	val := strings.Trim(strings.Join(parts[1:], " "), "'\"")
	return name, val
}

func splitOption(s string) []string {
	var result []string
	var current strings.Builder
	inQuote := false
	quoteChar := byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inQuote {
			if c == quoteChar {
				inQuote = false
			} else {
				current.WriteByte(c)
			}
		} else if c == '\'' || c == '"' {
			inQuote = true
			quoteChar = c
		} else if c == ' ' || c == '\t' {
			if current.Len() > 0 {
				result = append(result, current.String())
				current.Reset()
			}
		} else {
			current.WriteByte(c)
		}
	}
	if current.Len() > 0 {
		result = append(result, current.String())
	}
	return result
}

// WriteDeviceConfig 写入设备配置到文件（JSON 格式）
func WriteDeviceConfig(path string, devices []DeviceConfig, resetHour int) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data := struct {
		Devices   []DeviceConfig `json:"devices"`
		ResetHour int            `json:"reset_hour"`
	}{
		Devices:   devices,
		ResetHour: resetHour,
	}
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0644)
}