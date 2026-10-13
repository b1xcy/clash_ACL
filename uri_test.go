package main

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestNodeURIProtocolFields(t *testing.T) {
	vmess := base64.StdEncoding.EncodeToString([]byte(`{"v":"2","ps":"VMess [倍率:0.5]","add":"example.invalid","port":"443","id":"test-uuid","aid":0,"scy":"auto","net":"ws","type":"none","host":"cdn.invalid","path":"/socket?ed=2048","tls":"tls","sni":"tls.invalid","alpn":"h2,http/1.1","fp":"chrome"}`))
	for _, test := range []struct {
		name string
		uri  string
		want map[string]any
	}{
		{"ss-sip002", "ss://" + base64.RawURLEncoding.EncodeToString([]byte("aes-128-gcm:p:a+ss")) + "@example.invalid:8388#SS%20%E9%A6%99%E6%B8%AF", map[string]any{"name": "SS 香港", "cipher": "aes-128-gcm", "password": "p:a+ss", "port": 8388}},
		{"ss-legacy", "ss://" + base64.StdEncoding.EncodeToString([]byte("aes-256-gcm:legacy@example.invalid:8388")) + "#Legacy", map[string]any{"name": "Legacy", "password": "legacy", "server": "example.invalid"}},
		{"ss-2022", "ss://2022-blake3-aes-128-gcm:p%2B%2F%3D@example.invalid:8388", map[string]any{"cipher": "2022-blake3-aes-128-gcm", "password": "p+/="}},
		{"ss-obfs", "ss://aes-128-gcm:p@example.invalid:8388?plugin=obfs-local%3Bobfs%3Dhttp%3Bobfs-host%3Dcdn.invalid", map[string]any{"plugin": "obfs", "plugin-opts": map[string]any{"mode": "http", "host": "cdn.invalid"}}},
		{"ss-v2ray", "ss://aes-128-gcm:p@example.invalid:8388?plugin=v2ray-plugin%3Bmode%3Dwebsocket%3Btls%3Bhost%3Dcdn.invalid%3Bpath%3D%2Fws", map[string]any{"plugin": "v2ray-plugin", "plugin-opts": map[string]any{"mode": "websocket", "tls": true, "host": "cdn.invalid", "path": "/ws"}}},
		{"tuic-v5", "tuic://test-uuid:p%40ss@example.invalid:443?congestion_control=bbr&udp_relay_mode=native&alpn=h3&disable_sni=1", map[string]any{"uuid": "test-uuid", "password": "p@ss", "congestion-controller": "bbr", "udp-relay-mode": "native", "disable-sni": true, "alpn": []string{"h3"}}},
		{"tuic-v4", "tuic://token@example.invalid:443?sni=tls.invalid", map[string]any{"token": "token", "sni": "tls.invalid"}},
		{"vless-reality", "vless://test-uuid@[2001:db8::1]:443?security=reality&sni=tls.invalid&fp=chrome&pbk=synthetic-key&sid=abcd&flow=xtls-rprx-vision&type=tcp&encryption=none#Fixed+IP", map[string]any{"name": "Fixed+IP", "type": "vless", "server": "2001:db8::1", "tls": true, "flow": "xtls-rprx-vision", "client-fingerprint": "chrome", "reality-opts": map[string]any{"public-key": "synthetic-key", "short-id": "abcd"}}},
		{"vless-ws", "vless://test-uuid@example.invalid:443?security=tls&type=ws&path=%2Fa%3Fb%3Dc&host=cdn.invalid&allowInsecure=0", map[string]any{"tls": true, "skip-cert-verify": false, "network": "ws", "ws-opts": map[string]any{"path": "/a?b=c", "headers": map[string]any{"Host": "cdn.invalid"}}}},
		{"vless-h2", "vless://test-uuid@example.invalid:443?security=tls&type=http&path=%2Fh2&host=cdn.invalid", map[string]any{"network": "h2", "h2-opts": map[string]any{"path": "/h2", "host": []string{"cdn.invalid"}}}},
		{"vless-tcp-http", "vless://test-uuid@example.invalid:443?type=tcp&headerType=http&path=%2Fa%2C%2Fb&host=cdn.invalid", map[string]any{"network": "http", "http-opts": map[string]any{"path": []string{"/a", "/b"}, "headers": map[string]any{"Host": []string{"cdn.invalid"}}}}},
		{"vmess-json", "vmess://" + vmess, map[string]any{"name": "VMess [倍率:0.5]", "type": "vmess", "uuid": "test-uuid", "alterId": 0, "tls": true, "servername": "tls.invalid", "alpn": []string{"h2", "http/1.1"}, "ws-opts": map[string]any{"path": "/socket?ed=2048", "headers": map[string]any{"Host": "cdn.invalid"}}}},
		{"vmess-aead", "vmess://test-uuid@example.invalid:443?encryption=aes-128-gcm&security=tls&type=grpc&serviceName=proxy", map[string]any{"cipher": "aes-128-gcm", "alterId": 0, "network": "grpc", "grpc-opts": map[string]any{"grpc-service-name": "proxy"}}},
		{"trojan", "trojan://p%3Aa%40ss@example.invalid:443?type=grpc&serviceName=my%2Fservice&sni=tls.invalid&allowInsecure=false", map[string]any{"password": "p:a@ss", "sni": "tls.invalid", "skip-cert-verify": false, "grpc-opts": map[string]any{"grpc-service-name": "my/service"}}},
		{"hy2", "hy2://user:pass@example.invalid/?obfs=salamander&obfs-password=p%2Ba&sni=tls.invalid&insecure=1&pinSHA256=synthetic-pin#HY2", map[string]any{"type": "hysteria2", "port": 443, "password": "user:pass", "obfs": "salamander", "obfs-password": "p+a", "fingerprint": "synthetic-pin", "skip-cert-verify": true}},
		{"hysteria2", "hysteria2://p%3Aa%2Bss@[2001:db8::2]:8443?up=50&down=100&alpn=h3", map[string]any{"type": "hysteria2", "port": 8443, "password": "p:a+ss", "up": "50", "down": "100"}},
		{"anytls", "anytls://p%40a%3Ass@example.invalid/?sni=tls.invalid&insecure=0", map[string]any{"type": "anytls", "port": 443, "password": "p@a:ss", "sni": "tls.invalid", "skip-cert-verify": false}},
	} {
		t.Run(test.name, func(t *testing.T) {
			proxy, err := parseNodeURI(test.uri)
			if err != nil {
				t.Fatal(err)
			}
			for key, expected := range test.want {
				if !reflect.DeepEqual(proxy[key], expected) {
					t.Errorf("field %s: got %#v, want %#v", key, proxy[key], expected)
				}
			}
		})
	}
}

func TestURIUnsupportedSettingsDoNotSilentlyChangeNodes(t *testing.T) {
	for _, uri := range []string{
		"ssr://private-secret", "vmess://bad-base64!", "vless://private-secret@example.invalid:70000", "trojan://private-secret@example.invalid",
		"trojan://private-secret@example.invalid:443?allowInsecure=yes", "vless://private-secret@example.invalid:443?security=reality",
		"vless://private-secret@example.invalid:443?type=xhttp", "vless://private-secret@example.invalid:443?type=ws&private-secret=abc",
		"trojan://private-secret@example.invalid:443?sni=a&sni=b", "trojan://private-secret@example.invalid:443?insecure=1&allowInsecure=0",
		"ss://aes-128-gcm:private-secret@example.invalid:8388?plugin=unknown-plugin", "hy2://private-secret@example.invalid:443,8443",
		"anytls://private-secret@example.invalid:443?sni=127.0.0.1", "vless://private-secret@example.invalid:443?security=invalid",
		"ss://aes-128-gcm:private-secret@example.invalid:8388?plugin=obfs-local%3Bprivate-secret%3Dabc", "trojan://private-secret@example.invalid:443?bad=%XY",
	} {
		proxy, err := parseNodeURI(uri)
		if err == nil || proxy != nil {
			t.Fatalf("unsupported URI accepted: %s", uri)
		}
		if strings.Contains(err.Error(), "private-secret") || strings.Contains(err.Error(), "example.invalid") {
			t.Fatal("parse error exposes node credentials or address")
		}
	}
}

func TestURIAPIReportsOriginalLinesWithoutPersistingOrLeaking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := writeConfig(path, defaultConfig()); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	text := "\n  trojan://private-secret@example.invalid:443#One  \r\n\nssr://private-secret\nanytls://private-secret@example.invalid#Two\n"
	response := httptest.NewRecorder()
	newHandler(path).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/uri/preview", strings.NewReader(text)))
	var result struct {
		Lines []URILine `json:"lines"`
		Valid int       `json:"valid_count"`
		Error int       `json:"error_count"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || result.Valid != 2 || result.Error != 1 || len(result.Lines) != 3 || result.Lines[0].Line != 2 || result.Lines[1].Line != 4 || result.Lines[2].Line != 5 {
		t.Fatalf("incorrect per-line response: %s", response.Body.String())
	}
	if strings.Contains(response.Body.String(), "private-secret") || strings.Contains(response.Body.String(), "example.invalid") || strings.Contains(response.Body.String(), "trojan://") {
		t.Fatal("preview exposed protocol fields or input links")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("preview changed saved configuration")
	}
	parsed := parseURILines(text)
	if parsed[0].Proxy["password"] != "private-secret" || parsed[0].ID != result.Lines[0].ID {
		t.Fatal("internal parser lost credentials or unstable preview ID")
	}
	empty := httptest.NewRecorder()
	newHandler(path).ServeHTTP(empty, httptest.NewRequest(http.MethodPost, "/api/uri/preview", strings.NewReader("\n \n")))
	if empty.Code != http.StatusBadRequest {
		t.Fatal("empty input accepted")
	}
}

func TestVMessJSONRejectsInvalidOrUnmappedFields(t *testing.T) {
	for _, data := range []string{
		`null`, `{} {}`, `{"add":"example.invalid","port":443,"id":"private-secret","aid":-1}`,
		`{"add":"example.invalid","port":443,"id":"private-secret","unknown":"private-secret"}`,
		`{"add":"example.invalid","port":443,"id":"private-secret","tls":true}`,
	} {
		_, err := parseNodeURI("vmess://" + base64.RawStdEncoding.EncodeToString([]byte(data)))
		if err == nil || strings.Contains(err.Error(), "private-secret") {
			t.Fatal("invalid VMess input accepted or exposed")
		}
	}
}
