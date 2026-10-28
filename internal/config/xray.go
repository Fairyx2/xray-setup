package config

import (
	"bytes"
	"encoding/json"
	"fmt"
)

const InboundTag = "my-vpn"

// XrayConfig 对应官方原生配置。这里只定义默认模板用到的字段；高级编辑使用 RawMessage 保留扩展字段。
type XrayConfig struct {
	Log       LogConfig  `json:"log"`       // 日志策略，默认仅输出 warning 及以上级别。
	Inbounds  []Inbound  `json:"inbounds"`  // 入站列表；tag=my-vpn 的入站由表单管理。
	Outbounds []Outbound `json:"outbounds"` // 出站列表；默认 freedom 直连。
}

type LogConfig struct {
	LogLevel string `json:"loglevel"` // Xray 原生日志级别字段名为 loglevel。
}

type Inbound struct {
	Tag            string         `json:"tag"`            // 主节点固定为 my-vpn，供原生 JSON 与表单同步定位。
	Listen         string         `json:"listen"`         // 监听地址，默认绑定全部 IPv4 网卡。
	Port           int            `json:"port"`           // 服务器监听端口，不影响 REALITY 目标站端口。
	Protocol       string         `json:"protocol"`       // 本工具主节点固定使用 vless。
	Settings       VLESSSettings  `json:"settings"`       // VLESS 用户和加密设置。
	StreamSettings StreamSettings `json:"streamSettings"` // XHTTP 传输及 REALITY 安全层。
}

type VLESSSettings struct {
	Clients    []Client `json:"clients"`    // 客户端身份列表；表单仅修改第一个用户，其余用户保留。
	Decryption string   `json:"decryption"` // 默认 none；不启用额外的 VLESS Encryption。
}

type Client struct {
	ID   string `json:"id"`   // 客户端 UUID。
	Flow string `json:"flow"` // XHTTP 下必须为空，不使用 xtls-rprx-vision。
}

type StreamSettings struct {
	Network         string          `json:"network"`         // 传输固定为 xhttp。
	Security        string          `json:"security"`        // 安全层固定为 reality。
	XHTTPSettings   XHTTPSettings   `json:"xhttpSettings"`   // XHTTP 路径和工作模式。
	RealitySettings RealitySettings `json:"realitySettings"` // REALITY 目标、密钥和认证参数。
}

type XHTTPSettings struct {
	Path string `json:"path"` // 请求路径，客户端分享链接同步此值。
	Mode string `json:"mode"` // 默认 auto；额外配置可通过原生 JSON 添加。
}

type RealitySettings struct {
	Show        bool     `json:"show"`        // 是否输出 REALITY 调试信息，默认关闭。
	Target      string   `json:"target"`      // 目标 TLS 站点，通常使用独立的 443 端口。
	Xver        int      `json:"xver"`        // PROXY protocol 版本，默认 0，即不发送。
	ServerNames []string `json:"serverNames"` // 接受的 SNI；第一个值用于生成主节点分享链接。
	PrivateKey  string   `json:"privateKey"`  // X25519 私钥，禁止导出到客户端或普通日志。
	ShortIDs    []string `json:"shortIds"`    // 允许的 Short ID；第一个值用于分享链接。
}

type Outbound struct {
	Protocol string `json:"protocol"` // 默认 freedom，将流量直接发送到目标网站。
	Tag      string `json:"tag"`      // 默认 direct，供用户在原生路由中引用。
}

func Build(p Profile) ([]byte, error) {
	return json.MarshalIndent(XrayConfig{
		Log: LogConfig{LogLevel: p.LogLevel},
		Inbounds: []Inbound{{Tag: InboundTag, Listen: p.Listen, Port: p.Port, Protocol: "vless",
			Settings: VLESSSettings{Clients: []Client{{ID: p.UUID, Flow: ""}}, Decryption: "none"},
			StreamSettings: StreamSettings{Network: "xhttp", Security: "reality",
				XHTTPSettings:   XHTTPSettings{Path: p.Path, Mode: p.Mode},
				RealitySettings: RealitySettings{Target: p.Target, ServerNames: []string{p.ServerName}, PrivateKey: p.PrivateKey, ShortIDs: []string{p.ShortID}},
			},
		}},
		Outbounds: []Outbound{{Protocol: "freedom", Tag: "direct"}},
	}, "", "  ")
}

// ParseNative 从完整 JSON 同步表单参数。额外字段保持在原始 JSON 中，不经过结构体重编码。
func ParseNative(data []byte, previous Profile) (Profile, error) {
	var root XrayConfig
	decoder := json.NewDecoder(bytes.NewReader(data))
	if !json.Valid(data) || decoder.Decode(&root) != nil {
		return previous, fmt.Errorf("json")
	}
	var primary *Inbound
	for i := range root.Inbounds {
		if root.Inbounds[i].Tag == InboundTag {
			if primary != nil {
				return previous, fmt.Errorf("duplicate tag=my-vpn")
			}
			primary = &root.Inbounds[i]
		}
	}
	if primary == nil {
		return previous, fmt.Errorf("missing tag=my-vpn")
	}
	s := primary.StreamSettings
	if primary.Protocol != "vless" || s.Network != "xhttp" || s.Security != "reality" ||
		primary.Settings.Decryption != "none" || len(primary.Settings.Clients) == 0 ||
		primary.Settings.Clients[0].Flow != "" || len(s.RealitySettings.ServerNames) == 0 || len(s.RealitySettings.ShortIDs) == 0 {
		return previous, fmt.Errorf("primary inbound must use VLESS + XHTTP + REALITY, decryption=none, flow empty")
	}
	// 分享链接只描述标准单连接主节点。会改变连接结构的高级选项暂不接受，以免导出不可用链接。
	var raw map[string]json.RawMessage
	_ = json.Unmarshal(data, &raw)
	var ins []map[string]json.RawMessage
	_ = json.Unmarshal(raw["inbounds"], &ins)
	for _, in := range ins {
		var tag string
		_ = json.Unmarshal(in["tag"], &tag)
		if tag != InboundTag {
			continue
		}
		var stream map[string]json.RawMessage
		_ = json.Unmarshal(in["streamSettings"], &stream)
		if rawMethod, ok := stream["method"]; ok {
			var method string
			if json.Unmarshal(rawMethod, &method) != nil || method != "xhttp" {
				return previous, fmt.Errorf("primary method must be xhttp")
			}
		}
		var xhttp map[string]json.RawMessage
		_ = json.Unmarshal(stream["xhttpSettings"], &xhttp)
		var extra map[string]json.RawMessage
		if value, ok := xhttp["extra"]; ok {
			if err := json.Unmarshal(value, &extra); err != nil {
				return previous, fmt.Errorf("xhttp extra must be an object")
			}
			for _, key := range []string{"host", "path", "mode", "downloadSettings"} {
				if _, ok := extra[key]; ok {
					return previous, fmt.Errorf("primary extra cannot override %s", key)
				}
			}
		}
		if host, ok := xhttp["host"]; ok && string(host) != `""` {
			return previous, fmt.Errorf("primary xhttp host must be empty")
		}
		if _, ok := xhttp["downloadSettings"]; ok {
			return previous, fmt.Errorf("primary downloadSettings is not supported")
		}
	}
	p := previous
	p.Port, p.Listen = primary.Port, primary.Listen
	p.UUID = primary.Settings.Clients[0].ID
	p.Target, p.ServerName = s.RealitySettings.Target, s.RealitySettings.ServerNames[0]
	p.PrivateKey, p.ShortID = s.RealitySettings.PrivateKey, s.RealitySettings.ShortIDs[0]
	p.Path, p.Mode, p.LogLevel = s.XHTTPSettings.Path, s.XHTTPSettings.Mode, root.Log.LogLevel
	if p.Listen == "" {
		p.Listen = "0.0.0.0"
	}
	if p.Mode == "" {
		p.Mode = "auto"
	}
	if p.LogLevel == "" {
		p.LogLevel = "warning"
	}
	public, err := PublicFromPrivate(p.PrivateKey)
	if err != nil {
		return previous, err
	}
	p.PublicKey = public
	return p, p.Validate()
}

// Patch 只更新表单拥有的字段，保留 routing、DNS、额外用户/入站及所有未知配置。
func Patch(data []byte, p Profile) ([]byte, error) {
	if len(data) == 0 {
		return Build(p)
	}
	var root map[string]any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&root); err != nil {
		return nil, err
	}
	list, ok := root["inbounds"].([]any)
	if !ok {
		return nil, fmt.Errorf("inbounds")
	}
	var primary map[string]any
	for _, item := range list {
		in, ok := item.(map[string]any)
		if ok && in["tag"] == InboundTag {
			primary = in
			break
		}
	}
	if primary == nil {
		return nil, fmt.Errorf("missing tag=my-vpn")
	}
	log := object(root, "log")
	log["loglevel"] = p.LogLevel
	primary["listen"], primary["port"] = p.Listen, p.Port
	settings := object(primary, "settings")
	clients, ok := settings["clients"].([]any)
	if !ok || len(clients) == 0 {
		return nil, fmt.Errorf("clients")
	}
	client, ok := clients[0].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("client")
	}
	client["id"], client["flow"] = p.UUID, ""
	stream := object(primary, "streamSettings")
	xhttp := object(stream, "xhttpSettings")
	xhttp["path"], xhttp["mode"] = p.Path, p.Mode
	reality := object(stream, "realitySettings")
	reality["target"], reality["privateKey"] = p.Target, p.PrivateKey
	replaceFirst(reality, "serverNames", p.ServerName)
	replaceFirst(reality, "shortIds", p.ShortID)
	return json.MarshalIndent(root, "", "  ")
}

func object(parent map[string]any, key string) map[string]any {
	child, ok := parent[key].(map[string]any)
	if !ok {
		child = make(map[string]any)
		parent[key] = child
	}
	return child
}

func replaceFirst(parent map[string]any, key, value string) {
	list, ok := parent[key].([]any)
	if !ok || len(list) == 0 {
		list = []any{value}
	} else {
		list[0] = value
	}
	parent[key] = list
}
