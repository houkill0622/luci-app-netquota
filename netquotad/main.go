// netquotad — OpenWrt 每日联网时长管理守护程序
//
// 主入口：解析命令行参数，启动监控循环和 RPC 服务
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
)

var version = "1.0.0"

func main() {
	configPath := flag.String("config", DefaultConfigPath, "UCI 配置文件路径")
	stateDir := flag.String("state", DefaultStateDir, "状态文件目录")
	socketPath := flag.String("socket", "/var/run/netquotad.sock", "Unix Socket 路径")
	showVersion := flag.Bool("version", false, "显示版本号")
	flag.Parse()

	if *showVersion {
		fmt.Printf("netquotad v%s\n", version)
		os.Exit(0)
	}

	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	log.Printf("[netquotad] 启动 v%s, 配置: %s", version, *configPath)

	// 加载配置
	config, err := ParseConfig(*configPath)
	if err != nil {
		log.Printf("[netquotad] 加载配置警告: %v", err)
	}
	log.Printf("[netquotad] 已加载 %d 个设备规则", len(config.Devices))
	config.StateDir = *stateDir
	config.ConfigPath = *configPath

	// 初始化状态管理器
	state := NewStateManager(*stateDir, *configPath)
	state.SyncConfig(config)

	// 初始化监控器
	monitor := NewMonitor(state, config)

	// 启动监控循环
	stopMonitor := make(chan struct{})
	go monitor.Run(stopMonitor)

	// 启动 RPC 服务（Unix Socket + TCP）
	rpcServer := NewRPCServer(state, monitor, *socketPath, 9800)
	go func() {
		if err := rpcServer.Start(); err != nil {
			log.Printf("[netquotad] RPC 服务异常退出: %v", err)
		}
	}()

	// 等待退出信号
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	sig := <-sigCh
	log.Printf("[netquotad] 收到信号 %v, 正在退出...", sig)

	// 清理
	close(stopMonitor)
	state.Save()
	os.Remove(*socketPath)
	log.Println("[netquotad] 已退出")
}