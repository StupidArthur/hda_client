package catalog

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// PresetConfig is the read-only subset of a preset config.yaml that the portal
// is allowed to interpret. Unknown keys are intentionally tolerated.
type PresetConfig struct {
	Server struct {
		Endpoint    string `yaml:"endpoint"`
		NS          *int   `yaml:"ns"`
		Namespace   string `yaml:"namespace"`
		MaxPageSize int    `yaml:"max_page_size"`
	} `yaml:"server"`
	Diagnostic struct {
		Listen string `yaml:"listen"`
	} `yaml:"diagnostic"`
	Playback struct {
		Files map[string]struct {
			PeriodMS int `yaml:"period_ms"`
		} `yaml:"files"`
	} `yaml:"playback"`
	History struct {
		RetentionDays int `yaml:"retention_days"`
	} `yaml:"history"`
}

func parseConfig(raw string) (PresetConfig, error) {
	var cfg PresetConfig
	if strings.TrimSpace(raw) == "" {
		return cfg, fmt.Errorf("config.yaml 为空")
	}
	if err := yaml.Unmarshal([]byte(raw), &cfg); err != nil {
		return cfg, fmt.Errorf("config.yaml 解析失败: %w", err)
	}
	return cfg, nil
}

// Endpoint is the validated form of server.endpoint.
type Endpoint struct {
	Raw      string `json:"raw"`
	Valid    bool   `json:"valid"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Wildcard bool   `json:"wildcard"`
	Loopback bool   `json:"loopback"`
	Error    string `json:"error,omitempty"`
}

var allowedSchemes = map[string]bool{
	"opc.tcp":   true,
	"opc.https": true,
	"opc.wss":   true,
}

func parseEndpoint(raw string) Endpoint {
	ep := Endpoint{Raw: strings.TrimSpace(raw)}
	if ep.Raw == "" {
		ep.Error = "未配置 server.endpoint"
		return ep
	}
	u, err := url.Parse(ep.Raw)
	if err != nil {
		ep.Error = "server.endpoint 格式错误"
		return ep
	}
	if !allowedSchemes[strings.ToLower(u.Scheme)] {
		ep.Error = fmt.Sprintf("不支持的地址协议 %q", u.Scheme)
		return ep
	}
	host := u.Hostname()
	if host == "" {
		ep.Error = "server.endpoint 缺少主机名"
		return ep
	}
	portText := u.Port()
	if portText == "" {
		ep.Error = "server.endpoint 缺少端口"
		return ep
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		ep.Error = "server.endpoint 端口无效"
		return ep
	}
	ep.Host = host
	ep.Port = port
	ep.Wildcard = isWildcardHost(host)
	ep.Loopback = isLoopbackHost(host)
	ep.Valid = true
	return ep
}

// DiagTarget is derived from diagnostic.listen, combined with the UA host only
// when the diagnostic listener itself is a wildcard address.
type DiagTarget struct {
	Listen   string `json:"listen"`
	URL      string `json:"url"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	State    string `json:"state"` // monitored | unmonitored | incomplete | invalid
	Loopback bool   `json:"loopback"`
	Reason   string `json:"reason,omitempty"`
}

func deriveDiagTarget(listen string, ua Endpoint) DiagTarget {
	target := DiagTarget{Listen: strings.TrimSpace(listen)}
	if target.Listen == "" {
		target.State = DiagUnmonitored
		target.Reason = "未配置 diagnostic.listen"
		return target
	}
	host, portText, err := net.SplitHostPort(target.Listen)
	if err != nil {
		target.State = DiagInvalid
		target.Reason = "diagnostic.listen 需为 host:port"
		return target
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		target.State = DiagInvalid
		target.Reason = "diagnostic.listen 端口无效"
		return target
	}
	host = strings.Trim(host, "[]")
	if isLoopbackHost(host) && (!ua.Valid || ua.Wildcard || !isLocalHost(ua.Host)) {
		target.State = DiagIncomplete
		target.Reason = "诊断监听为环回地址，但无法确认 UA 服务位于门户本机；为避免采集错误实例，不进行探测"
		return target
	}
	if isWildcardHost(host) {
		if ua.Valid && !ua.Wildcard {
			target.Host = ua.Host
			target.Port = port
			target.URL = "http://" + net.JoinHostPort(ua.Host, strconv.Itoa(port))
			target.Loopback = isLoopbackHost(ua.Host)
			target.State = DiagMonitored
			target.Reason = "诊断监听为通配地址，按同一 config 的 server.endpoint 主机推导"
			return target
		}
		target.State = DiagIncomplete
		target.Reason = "诊断监听与 server.endpoint 均为通配地址，无法确定可访问主机"
		return target
	}
	target.Host = host
	target.Port = port
	target.URL = "http://" + net.JoinHostPort(host, strconv.Itoa(port))
	target.Loopback = isLoopbackHost(host)
	target.State = DiagMonitored
	return target
}

const (
	DiagMonitored   = "monitored"
	DiagUnmonitored = "unmonitored"
	DiagIncomplete  = "incomplete"
	DiagInvalid     = "invalid"
)

// Only positive local evidence permits probing a loopback diagnostic listener.
// Unknown hostnames are not guessed or resolved against potentially remote DNS.
func isLocalHost(host string) bool {
	if isLoopbackHost(host) {
		return true
	}
	if name, err := os.Hostname(); err == nil && strings.EqualFold(host, name) {
		return true
	}
	ip := net.ParseIP(strings.Split(host, "%")[0])
	if ip == nil || ip.IsUnspecified() {
		return false
	}
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		return false
	}
	for _, address := range addresses {
		local, _, err := net.ParseCIDR(address.String())
		if err == nil && local.Equal(ip) {
			return true
		}
	}
	return false
}

func isWildcardHost(host string) bool {
	host = strings.Trim(host, "[]")
	if host == "" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsUnspecified()
	}
	return false
}

func isLoopbackHost(host string) bool {
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}
