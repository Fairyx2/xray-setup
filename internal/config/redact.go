package config

import (
	"encoding/json"
	"strings"
)

// RedactNative 同时隐藏额外用户/出站中的常见凭据，避免高级 JSON 预览与诊断暴露其他节点的密钥。
func RedactNative(data []byte, text string) string {
	var value any
	if json.Unmarshal(data, &value) != nil {
		return text
	}
	var secrets []string
	var collect func(any, bool)
	collect = func(value any, sensitive bool) {
		switch node := value.(type) {
		case map[string]any:
			for key, child := range node {
				key = strings.ToLower(key)
				private := sensitive || key == "id" || key == "shortids" || key == "shortid" || strings.Contains(key, "password") || strings.Contains(key, "privatekey") || strings.Contains(key, "secret") || strings.Contains(key, "token")
				collect(child, private)
			}
		case []any:
			for _, child := range node {
				collect(child, sensitive)
			}
		case string:
			if sensitive && node != "" {
				secrets = append(secrets, node)
			}
		}
	}
	collect(value, false)
	for _, secret := range secrets {
		text = strings.ReplaceAll(text, secret, "[REDACTED]")
	}
	return text
}
