// Package config 定义用户参数、原生 Xray 配置及持久化状态。
package config

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// Profile 是简单模式和高级表单共用的参数。敏感字段只能写入受限文件，禁止记录到日志。
type Profile struct {
	Address     string `json:"address"`     // 客户端连接地址；可为公网 IPv4 或域名，不是 REALITY 的目标站。
	Port        int    `json:"port"`        // VPS 的 TCP 入站端口，默认 443；冲突时推荐备用端口。
	Listen      string `json:"listen"`      // 服务端绑定的 IPv4，默认 0.0.0.0；第一版限定 IPv4。
	Target      string `json:"target"`      // REALITY 目标站，格式为域名:端口；与入站端口独立。
	ServerName  string `json:"server_name"` // 客户端 SNI；同时作为服务端允许的 serverNames。
	UUID        string `json:"uuid"`        // VLESS 用户身份；再次配置时保持不变。
	PrivateKey  string `json:"private_key"` // REALITY X25519 私钥，仅存于服务器。
	PublicKey   string `json:"public_key"`  // 对应的公钥；分享链接使用 pbk，新版 Xray 称为 password。
	ShortID     string `json:"short_id"`    // REALITY shortId，偶数长度的十六进制字符串。
	Path        string `json:"path"`        // XHTTP 请求路径，服务端与客户端保持一致。
	Mode        string `json:"mode"`        // XHTTP 模式；auto 在默认 REALITY 组合下使用 stream-one。
	Fingerprint string `json:"fingerprint"` // 客户端 TLS 指纹，默认 chrome，仅用于分享链接。
	LogLevel    string `json:"log_level"`   // Xray 日志级别，默认 warning，避免输出过多连接信息。
}

// State 保存已确认的参数及完整原生配置。Config 保留高级 JSON 中的未知字段和额外入站。
type State struct {
	Schema  int             `json:"schema"`  // 持久化格式版本，加载时校验，避免误读其他格式。
	Version string          `json:"version"` // 已安装的官方 Xray 版本，用于界面展示。
	Profile Profile         `json:"profile"` // 用于表单和主节点分享链接的参数。
	Config  json.RawMessage `json:"config"`  // 完整 Xray JSON，禁止表单操作静默丢弃自定义字段。
}

func Generate() (Profile, error) {
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return Profile{}, err
	}
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return Profile{}, err
	}
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	short := make([]byte, 8)
	path := make([]byte, 12)
	if _, err := rand.Read(short); err != nil {
		return Profile{}, err
	}
	if _, err := rand.Read(path); err != nil {
		return Profile{}, err
	}
	return Profile{
		Port: 443, Listen: "0.0.0.0", Target: "www.microsoft.com:443", ServerName: "www.microsoft.com",
		UUID:       fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:]),
		PrivateKey: base64.RawURLEncoding.EncodeToString(key.Bytes()),
		PublicKey:  base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()),
		ShortID:    hex.EncodeToString(short), Path: "/xhttp-" + hex.EncodeToString(path) + "/",
		Mode: "auto", Fingerprint: "chrome", LogLevel: "warning",
	}, nil
}

var uuidPattern = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var hostnamePattern = regexp.MustCompile(`(?i)^[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?$`)

func ValidHostname(value string) bool {
	if len(value) > 253 || !hostnamePattern.MatchString(value) {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if len(label) == 0 || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
	}
	return true
}

func (p Profile) Validate() error {
	if ip, err := netip.ParseAddr(p.Address); err != nil {
		if !ValidHostname(p.Address) {
			return fmt.Errorf("address")
		}
	} else if !ip.Is4() || !PublicIPv4(ip) {
		return fmt.Errorf("address")
	}
	if p.Port < 1 || p.Port > 65535 {
		return fmt.Errorf("port")
	}
	if ip, err := netip.ParseAddr(p.Listen); err != nil || !ip.Is4() {
		return fmt.Errorf("listen")
	}
	host, port, err := net.SplitHostPort(p.Target)
	if err != nil || !ValidHostname(host) {
		return fmt.Errorf("target")
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("target")
	}
	if !ValidHostname(p.ServerName) {
		return fmt.Errorf("server_name")
	}
	if !uuidPattern.MatchString(p.UUID) {
		return fmt.Errorf("uuid")
	}
	if p.ShortID == "" || len(p.ShortID) > 16 || len(p.ShortID)%2 != 0 {
		return fmt.Errorf("short_id")
	}
	if _, err := hex.DecodeString(p.ShortID); err != nil {
		return fmt.Errorf("short_id")
	}
	if len(p.Path) < 2 || len(p.Path) > 256 || !strings.HasPrefix(p.Path, "/") || strings.ContainsAny(p.Path, "?# \t\r\n") {
		return fmt.Errorf("path")
	}
	if !oneOf(p.Mode, "auto", "stream-one", "stream-up", "packet-up") {
		return fmt.Errorf("mode")
	}
	if !oneOf(p.LogLevel, "debug", "info", "warning", "error", "none") {
		return fmt.Errorf("log_level")
	}
	if !oneOf(p.Fingerprint, "chrome", "firefox", "safari", "edge", "ios", "android", "random", "randomized") {
		return fmt.Errorf("fingerprint")
	}
	privateBytes, err := base64.RawURLEncoding.DecodeString(p.PrivateKey)
	if err != nil {
		return fmt.Errorf("private_key")
	}
	private, err := ecdh.X25519().NewPrivateKey(privateBytes)
	if err != nil {
		return fmt.Errorf("private_key")
	}
	if base64.RawURLEncoding.EncodeToString(private.PublicKey().Bytes()) != p.PublicKey {
		return fmt.Errorf("public_key")
	}
	return nil
}

func oneOf(value string, options ...string) bool {
	for _, option := range options {
		if value == option {
			return true
		}
	}
	return false
}

func PublicFromPrivate(value string) (string, error) {
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return "", fmt.Errorf("private_key")
	}
	key, err := ecdh.X25519().NewPrivateKey(data)
	if err != nil {
		return "", fmt.Errorf("private_key")
	}
	return base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()), nil
}

func PublicIPv4(ip netip.Addr) bool {
	return ip.Is4() && ip.IsGlobalUnicast() && !ip.IsPrivate() && !netip.MustParsePrefix("100.64.0.0/10").Contains(ip)
}

func (p Profile) ShareLink() string {
	query := url.Values{
		"encryption": {"none"}, "security": {"reality"}, "type": {"xhttp"},
		"sni": {p.ServerName}, "fp": {p.Fingerprint}, "pbk": {p.PublicKey}, "sid": {p.ShortID},
		"path": {p.Path}, "mode": {p.Mode},
	}
	return (&url.URL{Scheme: "vless", User: url.User(p.UUID), Host: net.JoinHostPort(p.Address, strconv.Itoa(p.Port)), RawQuery: query.Encode(), Fragment: "my-vpn"}).String()
}

// ShareLinkFor 同步原生 xhttpSettings 的额外参数，避免高级 JSON 中的客户端设置在导出时丢失。
func (p Profile) ShareLinkFor(data []byte) string {
	u, _ := url.Parse(p.ShareLink())
	var root map[string]json.RawMessage
	var ins []map[string]json.RawMessage
	if json.Unmarshal(data, &root) != nil || json.Unmarshal(root["inbounds"], &ins) != nil {
		return u.String()
	}
	for _, in := range ins {
		var tag string
		_ = json.Unmarshal(in["tag"], &tag)
		if tag != InboundTag {
			continue
		}
		var stream, xhttp map[string]json.RawMessage
		_ = json.Unmarshal(in["streamSettings"], &stream)
		_ = json.Unmarshal(stream["xhttpSettings"], &xhttp)
		// v2rayN 的分享字段 extra 对应 xhttpSettings.extra 的内容，不能再包一层 extra。
		var nested map[string]json.RawMessage
		_ = json.Unmarshal(xhttp["extra"], &nested)
		delete(xhttp, "extra")
		for key, value := range nested {
			xhttp[key] = value
		}
		delete(xhttp, "path")
		delete(xhttp, "mode")
		delete(xhttp, "host")
		if len(xhttp) > 0 {
			extra, _ := json.Marshal(xhttp)
			q := u.Query()
			q.Set("extra", string(extra))
			u.RawQuery = q.Encode()
		}
	}
	return u.String()
}

// Redact 用于展示外部进程诊断前的最后一道过滤；私钥、身份和分享链接不进入普通日志。
func (p Profile) Redact(text string) string {
	for _, secret := range []string{p.PrivateKey, p.UUID, p.ShortID, p.ShareLink()} {
		if secret != "" {
			text = strings.ReplaceAll(text, secret, "[REDACTED]")
		}
	}
	return text
}
