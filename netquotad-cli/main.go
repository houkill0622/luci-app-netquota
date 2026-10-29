// netquotad-cli — netquotad 的命令行客户端
// 通过 Unix Socket 与守护程序通信，供 rpcd 和 shell 脚本调用
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultSocket = "/var/run/netquotad.sock"
	socketTimeout = 5 * time.Second
)

func main() {
	// 提取 "-socket /path" 参数（任意位置），其余作为命令参数
	var clean []string
	socket := defaultSocket
	for i := 0; i < len(os.Args); i++ {
		if os.Args[i] == "-socket" && i+1 < len(os.Args) {
			socket = os.Args[i+1]
			i++
			continue
		}
		clean = append(clean, os.Args[i])
	}
	os.Args = clean

	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	client := newSocketClient(socket)

	switch cmd {
	case "status":
		callGet(client, "/api/v1/status")

	case "devices":
		if len(args) == 0 {
			// 列出所有设备
			callGet(client, "/api/v1/devices")
		} else {
			sub := args[0]
			rest := args[1:]
			switch sub {
			case "list":
				callGet(client, "/api/v1/devices")
			case "add":
				// 读取 JSON 或参数格式
				var data map[string]interface{}
				if len(rest) > 0 && strings.HasPrefix(rest[0], "{") {
					json.Unmarshal([]byte(rest[0]), &data)
				} else if len(rest) >= 2 {
					data = map[string]interface{}{
						"mac":     rest[0],
						"name":    rest[1],
						"quota":   60,
						"mode":    2,
						"enabled": true,
					}
					if len(rest) >= 3 {
						var q int
						fmt.Sscanf(rest[2], "%d", &q)
						data["quota"] = q
					}
					if len(rest) >= 4 {
						var m int
						fmt.Sscanf(rest[3], "%d", &m)
						data["mode"] = m
					}
				} else {
					fatal("用法: netquotad-cli devices add <mac> <name> [quota] [mode]")
				}
				callPost(client, "/api/v1/devices", data)
			case "update":
				if len(rest) < 2 {
					fatal("用法: netquotad-cli devices update <mac> <json>")
				}
				var data map[string]interface{}
				json.Unmarshal([]byte(rest[1]), &data)
				data["mac"] = rest[0]
				callPost(client, "/api/v1/devices", data)
			case "delete":
				if len(rest) < 1 {
					fatal("用法: netquotad-cli devices delete <mac>")
				}
				callDelete(client, "/api/v1/devices?mac="+rest[0])
			default:
				// 直接指定 MAC 地址查询
				callGet(client, "/api/v1/devices?mac="+sub)
			}
		}

	case "blocked":
		callGet(client, "/api/v1/blocked")

	case "allow":
		// netquotad-cli allow <mac|名字> <分钟>  — 给奖励时间
		if len(args) < 2 {
			fatal("用法: netquotad-cli allow <mac|设备名> <分钟>")
		}
		minutes := 0
		fmt.Sscanf(args[1], "%d", &minutes)
		if minutes <= 0 {
			fatal("分钟数无效: " + args[1])
		}
		// 名字/MAC 解析交给 daemon 端，CLI 透传
		data, _ := json.Marshal(map[string]interface{}{"mac": args[0], "minutes": minutes})
		callPost(client, "/api/v1/reward", data)

	case "rewards":
		callGet(client, "/api/v1/reward")

	case "unreward":
		// 提前结束奖励，立即恢复阻断
		if len(args) < 1 {
			fatal("用法: netquotad-cli unreward <mac>")
		}
		callDelete(client, "/api/v1/reward?mac="+args[0])

	case "block":
		if len(args) < 1 {
			fatal("用法: netquotad-cli block <mac> [ip]")
		}
		data := map[string]interface{}{
			"action": "block",
			"mac":    args[0],
		}
		if len(args) >= 2 {
			data["ip"] = args[1]
		}
		callPost(client, "/api/v1/blocked", data)

	case "unblock":
		if len(args) < 1 {
			fatal("用法: netquotad-cli unblock <mac> [ip]")
		}
		data := map[string]interface{}{
			"action": "unblock",
			"mac":    args[0],
		}
		if len(args) >= 2 {
			data["ip"] = args[1]
		}
		callPost(client, "/api/v1/blocked", data)

	case "scan":
		callGet(client, "/api/v1/scan")

	case "reset":
		callPost(client, "/api/v1/reset", map[string]string{})

	case "config":
		callGet(client, "/api/v1/config")

	default:
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `netquotad-cli — 网络时长管理命令行工具

用法:
  netquotad-cli status                   查看整体状态
  netquotad-cli devices [list]           列出所有设备
  netquotad-cli devices add <mac> <name> [quota] [mode]  添加设备
  netquotad-cli devices update <mac> <json>  更新设备
  netquotad-cli devices delete <mac>     删除设备
  netquotad-cli blocked                  查看被阻断设备
  netquotad-cli allow <mac|名> <分钟>     给奖励时间（临时放行，到点自动恢复阻断）
  netquotad-cli rewards                  查看奖励中的设备及剩余时间
  netquotad-cli block <mac> [ip]         阻断设备
  netquotad-cli unblock <mac> [ip]       解除阻断
  netquotad-cli scan                     扫描局域网设备
  netquotad-cli reset                    手动重置今日统计
  netquotad-cli config                   查看配置
`)
}

type socketClient struct {
	httpClient *http.Client
	baseURL    string
}

func newSocketClient(socketPath string) *socketClient {
	dialer := &net.Dialer{Timeout: socketTimeout}
	tr := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer.DialContext(ctx, "unix", socketPath)
		},
	}
	return &socketClient{
		httpClient: &http.Client{
			Transport: tr,
			Timeout:   socketTimeout,
		},
		baseURL: "http://localhost",
	}
}

func (c *socketClient) get(path string) ([]byte, error) {
	resp, err := c.httpClient.Get(c.baseURL + path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func (c *socketClient) post(path string, body interface{}) ([]byte, error) {
	var b []byte
	switch v := body.(type) {
	case []byte:
		b = v
	default:
		b, _ = json.Marshal(body)
	}
	req, err := http.NewRequest("POST", c.baseURL+path, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func (c *socketClient) delete(path string) ([]byte, error) {
	req, _ := http.NewRequest("DELETE", c.baseURL+path, nil)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func callGet(client *socketClient, path string) {
	data, err := client.get(path)
	if err != nil {
		fatal("请求失败: " + err.Error())
	}
	prettyPrint(data)
}

func callPost(client *socketClient, path string, body interface{}) {
	data, err := client.post(path, body)
	if err != nil {
		fatal("请求失败: " + err.Error())
	}
	prettyPrint(data)
}

func callDelete(client *socketClient, path string) {
	data, err := client.delete(path)
	if err != nil {
		fatal("请求失败: " + err.Error())
	}
	prettyPrint(data)
}

func prettyPrint(data []byte) {
	var out bytes.Buffer
	if json.Indent(&out, data, "", "  ") == nil {
		fmt.Println(out.String())
	} else {
		fmt.Println(string(data))
	}
}

func fatal(msg string) {
	fmt.Fprintf(os.Stderr, "错误: %s\n", msg)
	os.Exit(1)
}

// keep import
var _ = filepath.Join
