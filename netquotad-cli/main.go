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
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	socket := defaultSocket
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

	case "bypass":
		if len(args) < 2 {
			fatal("用法: netquotad-cli bypass <mac> on|off")
		}
		bypass := args[1] == "on" || args[1] == "true" || args[1] == "1"
		callPost(client, "/api/v1/bypass", map[string]interface{}{
			"mac":    args[0],
			"bypass": bypass,
		})

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
  netquotad-cli block <mac> [ip]         阻断设备
  netquotad-cli unblock <mac> [ip]       解除阻断
  netquotad-cli scan                     扫描局域网设备
  netquotad-cli reset                    手动重置今日统计
  netquotad-cli bypass <mac> on|off      今日放行/取消放行
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
	b, _ := json.Marshal(body)
	resp, err := c.httpClient.Post(c.baseURL+path, "application/json", bytes.NewReader(b))
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