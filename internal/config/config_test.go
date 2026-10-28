package config

import (
	"encoding/json"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func testProfile(t *testing.T) Profile {
	t.Helper()
	p, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	p.Address = "203.0.113.10"
	return p
}

func TestGeneratedConfigAndShareAgree(t *testing.T) {
	p := testProfile(t)
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	data, err := Build(p)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := ParseNative(data, p)
	if err != nil {
		t.Fatal(err)
	}
	if actual != p {
		t.Fatalf("config roundtrip changed profile")
	}
	u, err := url.Parse(p.ShareLink())
	if err != nil {
		t.Fatal(err)
	}
	if u.Host != "203.0.113.10:443" || u.User.Username() != p.UUID {
		t.Fatal("wrong connection address or user")
	}
	for key, want := range map[string]string{"type": "xhttp", "security": "reality", "path": p.Path, "pbk": p.PublicKey, "sid": p.ShortID, "mode": "auto", "encryption": "none"} {
		if u.Query().Get(key) != want {
			t.Errorf("%s mismatch", key)
		}
	}
	if strings.Contains(p.ShareLink(), p.PrivateKey) || u.Query().Get("flow") != "" {
		t.Fatal("private key leak or incompatible flow")
	}
}

func TestPatchPreservesNativeExtensionsAndCredentials(t *testing.T) {
	p := testProfile(t)
	data, _ := Build(p)
	var root map[string]any
	json.Unmarshal(data, &root)
	root["routing"] = map[string]any{"rules": []any{map[string]any{"type": "field", "port": "65535", "outboundTag": "direct"}}}
	root["dns"] = map[string]any{"servers": []string{"1.1.1.1"}}
	primary := root["inbounds"].([]any)[0].(map[string]any)
	primary["sniffing"] = map[string]any{"enabled": true, "destOverride": []string{"http", "tls"}}
	settings := primary["settings"].(map[string]any)
	settings["clients"] = append(settings["clients"].([]any), map[string]any{"id": "3d2bd63a-7d1a-408b-ac51-ea87386c8214", "email": "second"})
	xhttp := primary["streamSettings"].(map[string]any)["xhttpSettings"].(map[string]any)
	xhttp["extra"] = map[string]any{"noSSEHeader": true}
	data, _ = json.Marshal(root)
	p.Port = 8443
	patched, err := Patch(data, p)
	if err != nil {
		t.Fatal(err)
	}
	var after map[string]any
	json.Unmarshal(patched, &after)
	for _, key := range []string{"routing", "dns"} {
		beforeJSON, _ := json.Marshal(root[key])
		afterJSON, _ := json.Marshal(after[key])
		if string(beforeJSON) != string(afterJSON) {
			t.Errorf("lost %s", key)
		}
	}
	decoded, err := ParseNative(patched, p)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.UUID != p.UUID || decoded.PrivateKey != p.PrivateKey || decoded.Port != 8443 {
		t.Fatal("patch rotated credentials or lost port")
	}
	primary = after["inbounds"].([]any)[0].(map[string]any)
	if primary["sniffing"] == nil || len(primary["settings"].(map[string]any)["clients"].([]any)) != 2 {
		t.Fatal("lost extensions or additional clients")
	}
	u, _ := url.Parse(p.ShareLinkFor(patched))
	var exported map[string]any
	if err := json.Unmarshal([]byte(u.Query().Get("extra")), &exported); err != nil {
		t.Fatal(err)
	}
	if exported["noSSEHeader"] != true || exported["extra"] != nil {
		t.Fatal("share link lost native xhttp settings")
	}
}

func TestRejectInvalidAndIncompatibleNativeConfig(t *testing.T) {
	p := testProfile(t)
	raw, _ := Build(p)
	for _, test := range []struct{ name, from, to string }{
		{"vision", `"flow": ""`, `"flow": "xtls-rprx-vision"`},
		{"transport", `"network": "xhttp"`, `"network": "tcp"`},
		{"tag", `"tag": "my-vpn"`, `"tag": "other"`},
		{"key", p.PrivateKey, "bad-key"},
		{"shortID", p.ShortID, "xyz"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ParseNative([]byte(strings.Replace(string(raw), test.from, test.to, 1)), p); err == nil {
				t.Fatal("accepted invalid config")
			}
		})
	}
	if _, err := ParseNative(append(raw, []byte(`{}`)...), p); err == nil {
		t.Fatal("accepted trailing JSON")
	}
}

func TestProfileValidation(t *testing.T) {
	base := testProfile(t)
	tests := []struct {
		name   string
		change func(*Profile)
	}{
		{"port", func(p *Profile) { p.Port = 65536 }}, {"address", func(p *Profile) { p.Address = "127.0.0.1" }},
		{"CGNAT", func(p *Profile) { p.Address = "100.64.1.1" }}, {"listen", func(p *Profile) { p.Listen = "::" }},
		{"target", func(p *Profile) { p.Target = "example.com:0" }}, {"hostname", func(p *Profile) { p.ServerName = "-bad.com" }},
		{"path", func(p *Profile) { p.Path = "/path?secret" }}, {"mode", func(p *Profile) { p.Mode = "invalid" }},
		{"mismatched-key", func(p *Profile) { other := testProfile(t); p.PublicKey = other.PublicKey }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			p := base
			test.change(&p)
			if p.Validate() == nil {
				t.Fatal("accepted invalid profile")
			}
		})
	}
}

func TestDiagnosticRedaction(t *testing.T) {
	p := testProfile(t)
	raw, _ := Build(p)
	data := []byte(strings.TrimSuffix(string(raw), "}") + `,"custom":{"password":"other-password","token":"other-token"}}`)
	text := p.PrivateKey + " " + p.UUID + " " + p.ShortID + " other-password other-token"
	text = RedactNative(data, p.Redact(text))
	for _, secret := range []string{p.PrivateKey, p.UUID, p.ShortID, "other-password", "other-token"} {
		if strings.Contains(text, secret) {
			t.Fatalf("secret leaked: %s", secret)
		}
	}
}

// 可选集成检查：指定本地官方 Xray 后，验证模板确实能被真实核心读取。
func TestOfficialXrayConfig(t *testing.T) {
	binary := os.Getenv("XRAY_TEST_BINARY")
	if binary == "" {
		t.Skip("set XRAY_TEST_BINARY for the optional official-core integration check")
	}
	p := testProfile(t)
	data, _ := Build(p)
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(binary, "run", "-test", "-config", path).CombinedOutput()
	if err != nil {
		t.Fatalf("official Xray rejected config: %v\n%s", err, p.Redact(string(output)))
	}
}
