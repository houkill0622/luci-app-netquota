// netquotad — JSON-RPC over Unix socket
// 供 LuCI 通过 rpcd 调用，查询状态和管理配置
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
)

// RPCServer RPC 服务
type RPCServer struct {
	state    *StateManager
	monitor  *Monitor
	socket   string
	httpPort int
}

// NewRPCServer 创建 RPC 服务
func NewRPCServer(state *StateManager, monitor *Monitor, socketPath string, httpPort ...int) *RPCServer {
	port := 0
	if len(httpPort) > 0 {
		port = httpPort[0]
	}
	return &RPCServer{
		state:    state,
		monitor:  monitor,
		socket:   socketPath,
		httpPort: port,
	}
}

func (s *RPCServer) setupMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/status", s.handleStatus)
	mux.HandleFunc("/api/v1/devices", s.handleDevices)
	mux.HandleFunc("/api/v1/blocked", s.handleBlocked)
	mux.HandleFunc("/api/v1/scan", s.handleScan)
	mux.HandleFunc("/api/v1/reset", s.handleReset)
	mux.HandleFunc("/api/v1/config", s.handleConfig)
	mux.HandleFunc("/api/v1/", s.handleCORS)
	return mux
}

func (s *RPCServer) handleCORS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	if r.Method == "OPTIONS" {
		w.WriteHeader(204)
		return
	}
	jsonError(w, "not found")
}

// Start 启动 RPC 服务（Unix Socket + 可选 TCP）
func (s *RPCServer) Start() error {
	mux := s.setupMux()

	// 启动 Unix Socket 监听
	os.Remove(s.socket)
	unixListener, err := net.Listen("unix", s.socket)
	if err != nil {
		return fmt.Errorf("监听 Unix Socket 失败: %w", err)
	}
	os.Chmod(s.socket, 0666)
	log.Printf("[netquotad] Unix Socket 服务已启动: %s", s.socket)
	go http.Serve(unixListener, mux)

	// 可选：启动 TCP 监听（供直接 HTTP 调用）
	if s.httpPort > 0 {
		addr := fmt.Sprintf("0.0.0.0:%d", s.httpPort)
		tcpListener, err := net.Listen("tcp", addr)
		if err != nil {
			return fmt.Errorf("监听 TCP %s 失败: %w", addr, err)
		}
		log.Printf("[netquotad] TCP 服务已启动: %s", addr)
		go http.Serve(tcpListener, mux)
	}

	// 阻塞主 goroutine
	select {}
}

// handleStatus 返回整体状态
func (s *RPCServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	devices := s.state.GetAllDevices()
	total := len(devices)
	online := 0
	blocked := 0
	for _, d := range devices {
		if d.LastSeen > 0 {
			online++
		}
		if d.Blocked {
			blocked++
		}
	}

	jsonResp(w, map[string]interface{}{
		"total_devices":   total,
		"online_devices":  online,
		"blocked_devices": blocked,
	})
}

// handleDevices 返回所有设备状态
func (s *RPCServer) handleDevices(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		devices := s.state.GetAllDevices()
		jsonResp(w, map[string]interface{}{"devices": devices})
	case http.MethodPost:
		var req struct {
			MAC     string `json:"mac"`
			Name    *string `json:"name"`
			Quota   *int    `json:"quota"`
			Mode    *int    `json:"mode"`
			Enabled *bool   `json:"enabled"`
			Used    *int    `json:"used"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, "无效请求")
			return
		}
		mac := strings.ToUpper(req.MAC)
		existing := s.state.GetDevice(mac)
		if existing != nil {
			// 原地更新，保留运行时状态
			s.state.UpdateDevice(mac, req.Name, req.Quota, req.Mode, req.Enabled)
			if req.Used != nil {
				s.state.SetUsedMinutes(mac, *req.Used)
			}
		} else {
			name := mac
			if req.Name != nil && *req.Name != "" {
				name = *req.Name
			}
			quota := 60
			if req.Quota != nil {
				quota = *req.Quota
			}
			mode := 2
			if req.Mode != nil {
				mode = *req.Mode
			}
			s.state.AddDevice(mac, name, quota, mode)
		}
		s.state.Save()
		jsonResp(w, map[string]string{"status": "ok"})
	case http.MethodDelete:
		mac := r.URL.Query().Get("mac")
		if mac == "" {
			jsonError(w, "缺少 mac 参数")
			return
		}
		s.state.RemoveDevice(strings.ToUpper(mac))
		s.state.Save()
		jsonResp(w, map[string]string{"status": "ok"})
	}
}

// handleBlocked 返回/管理阻断列表
func (s *RPCServer) handleBlocked(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		ips, _ := NFListBlocked()
		jsonResp(w, map[string]interface{}{"blocked": ips})
	case http.MethodPost:
		var req struct {
			Action string `json:"action"`
			MAC    string `json:"mac"`
			IP     string `json:"ip"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, "无效请求")
			return
		}
		ip := req.IP
		if ip == "" {
			dev := s.state.GetDevice(strings.ToUpper(req.MAC))
			if dev != nil {
				ip = dev.IP
			}
		}
		if ip == "" {
			jsonError(w, "无法确定设备IP")
			return
		}
		if req.Action == "block" {
			NFBlockDevice(ip)
			s.state.SetBlocked(strings.ToUpper(req.MAC), true)
			jsonResp(w, map[string]string{"status": "blocked"})
		} else {
			NFUnblockDevice(ip)
			s.state.SetBlocked(strings.ToUpper(req.MAC), false)
			jsonResp(w, map[string]string{"status": "unblocked"})
		}
		s.state.Save()
	}
}

// handleScan 扫描局域网设备
func (s *RPCServer) handleScan(w http.ResponseWriter, r *http.Request) {
	devices, err := s.monitor.DetectActiveDevices()
	if err != nil {
		jsonError(w, err.Error())
		return
	}
	jsonResp(w, map[string]interface{}{"devices": devices})
}

// handleReset 手动重置
func (s *RPCServer) handleReset(w http.ResponseWriter, r *http.Request) {
	s.monitor.resetDaily()
	jsonResp(w, map[string]string{"status": "reset"})
}

// handleConfig 读写配置
func (s *RPCServer) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg, err := ParseConfig(s.monitor.config.ConfigPath)
		if err != nil {
			jsonError(w, err.Error())
			return
		}
		jsonResp(w, map[string]interface{}{"config": cfg})
	case http.MethodPost:
		var req struct {
			Devices   []DeviceConfig `json:"devices"`
			ResetHour int            `json:"reset_hour"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, "无效请求")
			return
		}
		if err := WriteDeviceConfig(s.monitor.config.ConfigPath, req.Devices, req.ResetHour); err != nil {
			jsonError(w, "保存配置失败: "+err.Error())
			return
		}
		// 重新加载配置
		s.monitor.refreshConfig()
		jsonResp(w, map[string]string{"status": "ok"})
	}
}

func jsonResp(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	json.NewEncoder(w).Encode(data)
}

func jsonError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// compile guard
var _ = fmt.Sprintf