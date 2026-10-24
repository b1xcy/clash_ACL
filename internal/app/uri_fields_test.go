package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const syntheticRealityURI = "vless://11111111-1111-4111-8111-111111111111@192.0.2.1:443?type=tcp&encryption=none&security=reality&support-x25519mlkem768=true&pbk=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&fp=chrome&sni=tls.example.invalid&sid=0123456789abcdef&spx=%2Fsynthetic&flow=xtls-rprx-vision#Reality"
const syntheticHY2URI = "hysteria2://synthetic+password%3D@192.0.2.1:443?security=tls&alpn=h3&sni=tls.example.invalid&obfs=salamander&obfs-password=synthetic%3D%3D#HY2"
const syntheticXHTTPURI = "vless://22222222-2222-4222-8222-222222222222@192.0.2.1:18080?type=xhttp&encryption=none&path=%2Fsynthetic%2F&host=&mode=packet-up&x_padding_bytes=100-1000&extra=%7B%22mode%22%3A%22packet-up%22%2C%22xPaddingBytes%22%3A%22100-1000%22%7D&security=none#XHTTP"

func TestReportedURIShapesGenerateCorrectMihomoOptions(t *testing.T) {
	links := strings.Join([]string{syntheticRealityURI, syntheticHY2URI, syntheticXHTTPURI}, "\n")
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := writeConfig(configPath, defaultConfig()); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	newHandler(configPath).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/uri/preview", strings.NewReader(links)))
	var result struct {
		Valid int `json:"valid_count"`
		Error int `json:"error_count"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || response.Code != http.StatusOK || result.Valid != 3 || result.Error != 0 {
		t.Fatal("reported link shapes failed API preview", err)
	}
	if strings.Contains(response.Body.String(), "192.0.2.1") || strings.Contains(response.Body.String(), "synthetic+password") {
		t.Fatal("preview leaked node parameters")
	}
	output, preview, err := generateConfig(context.Background(), newSourceManager(t.TempDir()), defaultConfig(), GenerationRequest{DirectLinks: links, Rules: RuleRequest{Preset: "none"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	proxies, err := parseSubscription(output)
	if err != nil || len(proxies) != 3 || len(preview.Nodes) != 3 {
		t.Fatal("mixed protocol generation failed", err)
	}
	for _, proxy := range proxies {
		switch proxy["name"] {
		case "Reality":
			if proxy["servername"] != "tls.example.invalid" || proxy["reality-opts"].(map[string]any)["support-x25519mlkem768"] != true || proxy["flow"] != "xtls-rprx-vision" || proxy["sni"] != nil {
				t.Fatal("Reality options changed")
			}
		case "HY2":
			if proxy["password"] != "synthetic+password=" || proxy["obfs-password"] != "synthetic==" || proxy["sni"] != "tls.example.invalid" {
				t.Fatal("HY2 credentials or TLS settings changed")
			}
		case "XHTTP":
			opts := proxy["xhttp-opts"].(map[string]any)
			if proxy["network"] != "xhttp" || proxy["tls"] != false || opts["path"] != "/synthetic/" || opts["mode"] != "packet-up" || opts["x-padding-bytes"] != "100-1000" || opts["host"] != nil {
				t.Fatal("XHTTP options changed")
			}
		}
	}
	validateImportedAIWithMihomo(t, output)
}

func TestOfficialMihomoURIFieldsRoundTrip(t *testing.T) {
	links := map[string]string{
		"ss":        "ss://aes-128-gcm:synthetic-password@192.0.2.1:443#Node",
		"tuic":      "tuic://11111111-1111-4111-8111-111111111111:synthetic-password@192.0.2.1:443#Node",
		"vless":     "vless://11111111-1111-4111-8111-111111111111@192.0.2.1:443#Node",
		"vmess":     "vmess://11111111-1111-4111-8111-111111111111@192.0.2.1:443#Node",
		"trojan":    "trojan://synthetic-password@192.0.2.1:443#Node",
		"hysteria2": "hysteria2://synthetic-password@192.0.2.1:443#Node",
		"anytls":    "anytls://synthetic-password@192.0.2.1:443#Node",
	}
	for protocol, link := range links {
		for field, kind := range mihomoURIFields[mihomoURIProtocols[protocol]] {
			t.Run(protocol+"/"+field, func(t *testing.T) {
				value := sampleMihomoURIValue(kind)
				switch field {
				case "name":
					value = "Node"
				case "server":
					value = "192.0.2.1"
				case "port":
					value = json.Number("443")
				case "uuid":
					value = "11111111-1111-4111-8111-111111111111"
				case "password":
					value = "synthetic-password"
				case "cipher":
					value = "auto"
					if protocol == "ss" {
						value = "aes-128-gcm"
					}
				case "plugin":
					value = "obfs"
				case "network":
					value = "tcp"
				}
				raw, _ := json.Marshal(value)
				if text, ok := value.(string); ok {
					raw = []byte(text)
				}
				u, _ := url.Parse(link)
				query := u.Query()
				query.Set(field, string(raw))
				u.RawQuery = query.Encode()
				proxy, err := parseNodeURI(u.String())
				if err != nil {
					t.Fatal(err)
				}
				data, err := marshalMihomo(map[string]any{"proxies": []map[string]any{proxy}})
				if err != nil {
					t.Fatal(err)
				}
				proxies, err := parseSubscription(data)
				if err != nil {
					t.Fatal(err)
				}
				expected, err := normalizeMihomoURIValue(value, kind)
				if err != nil {
					t.Fatal(err)
				}
				before, _ := json.Marshal(expected)
				after, _ := json.Marshal(proxies[0][field])
				if string(before) != string(after) {
					t.Fatalf("field lost during URI/YAML conversion: got %s, want %s", after, before)
				}
			})
		}
	}
}

func sampleMihomoURIValue(kind string) any {
	if fields, exists := mihomoURIFields[kind]; exists {
		value := map[string]any{}
		for field, child := range fields {
			value[field] = sampleMihomoURIValue(child)
		}
		return value
	}
	if strings.HasPrefix(kind, "[]") {
		return []any{sampleMihomoURIValue(strings.TrimPrefix(kind, "[]"))}
	}
	if strings.HasPrefix(kind, "map:") {
		return map[string]any{"X-Synthetic": sampleMihomoURIValue(strings.TrimPrefix(kind, "map:"))}
	}
	switch kind {
	case "string", "any":
		return "synthetic"
	case "bool":
		return true
	default:
		return json.Number("1")
	}
}

func TestMihomoURIConflictsAndInvalidFieldsAreRedacted(t *testing.T) {
	for _, query := range []string{
		"security=reality&pbk=synthetic&support-x25519mlkem768=invalid",
		"type=xhttp&mode=packet-up&extra=" + url.QueryEscape(`{"mode":"stream-up"}`),
		"type=xhttp&extra=" + url.QueryEscape(`{"unknown":"private-secret"}`),
		"type=xhttp&extra=" + url.QueryEscape(`{"xPaddingBytes":[]}`),
		"type=xhttp&extra=" + url.QueryEscape(`null`),
		"type=xhttp&extra=" + url.QueryEscape(`{} {}`),
		"type=xhttp&extra=" + url.QueryEscape(`{"downloadSettings":{"security":"invalid"}}`),
		"type=mkcp",
		"tls=true&security=none",
		"sni=one.invalid&servername=two.invalid",
		"type=ws&path=%2Fone&ws-opts=" + url.QueryEscape(`{"path":"/two"}`),
		"reality-opts=" + url.QueryEscape(`{"public-key":"private-secret","unknown":true}`),
		"routing-mark=1.5", "routing-mark=9223372036854775808",
		"ws-opts=" + url.QueryEscape(`{"headers":{"X-Test":false}}`),
	} {
		proxy, err := parseNodeURI("vless://private-secret@192.0.2.1:443?" + query)
		if err == nil || proxy != nil {
			t.Fatal("invalid or conflicting options accepted", query)
		}
		if strings.Contains(err.Error(), "private-secret") || strings.Contains(err.Error(), "192.0.2.1") {
			t.Fatal("node error leaked credentials or address")
		}
	}
}

func TestVMessJSONPreservesOfficialNestedFields(t *testing.T) {
	input := `{"add":"192.0.2.1","port":443,"id":"11111111-1111-4111-8111-111111111111","tls":true,"alpn":["h2"],"network":"ws","servername":"tls.example.invalid","ws-opts":{"path":"/synthetic","headers":{"Host":"cdn.example.invalid"},"max-early-data":2048},"smux":{"enabled":false},"global-padding":true}`
	proxy, err := parseNodeURI("vmess://" + base64.RawStdEncoding.EncodeToString([]byte(input)))
	if err != nil || proxy["tls"] != true || proxy["servername"] != "tls.example.invalid" || proxy["global-padding"] != true {
		t.Fatal("VMess JSON official options were lost", err)
	}
	if proxy["ws-opts"].(map[string]any)["path"] != "/synthetic" {
		t.Fatal("VMess JSON nested options were lost")
	}
	native := `{"type":"vmess","name":"Native","server":"192.0.2.1","port":443,"uuid":"11111111-1111-4111-8111-111111111111","network":"ws","tls":true,"cipher":"auto","alterId":0,"ws-opts":{"path":"/native"}}`
	proxy, err = parseNodeURI("vmess://" + base64.RawStdEncoding.EncodeToString([]byte(native)))
	if err != nil || proxy["name"] != "Native" || proxy["server"] != "192.0.2.1" || proxy["ws-opts"].(map[string]any)["path"] != "/native" {
		t.Fatal("native VMess JSON fields were lost", err)
	}
}

func TestSevenProtocolsAndTransportsWithMihomo(t *testing.T) {
	const uuid = "11111111-1111-4111-8111-111111111111"
	const baseVLESS = "vless://" + uuid + "@192.0.2.1:443?"
	const baseVMess = "vmess://" + uuid + "@192.0.2.1:443?"
	xhttpExtra := `{"mode":"packet-up","xPaddingBytes":"100-1000","headers":{"X-Test":"synthetic"},"noGRPCHeader":false,"xPaddingObfsMode":false,"sessionIDPlacement":"path","seqPlacement":"path","uplinkDataPlacement":"body","scMaxEachPostBytes":1000000,"scMinPostsIntervalMs":30,"xmux":{"maxConcurrency":"16-32","maxConnections":0,"cMaxReuseTimes":0,"hMaxRequestTimes":"600-900","hMaxReusableSecs":"1800-3000","hKeepAlivePeriod":30},"downloadSettings":{"address":"192.0.2.2","port":443,"security":"tls","tlsSettings":{"serverName":"download.example.invalid","fingerprint":"chrome","alpn":["h2"],"allowInsecure":false},"xhttpSettings":{"host":"download.example.invalid","path":"/download","headers":{"X-Download":"synthetic"},"extra":{"xmux":{"maxConcurrency":"8-16","hKeepAlivePeriod":30}}}}}`
	for _, test := range []struct{ name, link string }{
		{"ss", "ss://aes-128-gcm:synthetic-password@192.0.2.1:443?udp=true&tfo=false&plugin=obfs-local%3Bobfs%3Dhttp%3Bobfs-host%3Dcdn.example.invalid"},
		{"ss-v2ray", "ss://aes-128-gcm:synthetic-password@192.0.2.1:443?plugin=v2ray-plugin%3Bmode%3Dwebsocket%3Btls%3Bhost%3Dcdn.example.invalid&plugin-opts=" + url.QueryEscape(`{"headers":{"X-Test":"synthetic"},"mux":false}`)},
		{"tuic", "tuic://" + uuid + ":synthetic-password@192.0.2.1:443?congestion_control=bbr&udp_relay_mode=native&alpn=h3&heartbeat-interval=10000&reduce-rtt=true&disable-sni=false&udp-over-stream=false"},
		{"hy2", syntheticHY2URI},
		{"hy2-hopping", "hysteria2://synthetic-password@192.0.2.1:443?ports=443,8443,10000-10100&hop-interval=30&alpn=h3&initial-stream-receive-window=8388608&max-stream-receive-window=8388608&initial-connection-receive-window=20971520&max-connection-receive-window=20971520"},
		{"anytls", "anytls://synthetic-password@192.0.2.1:443?sni=tls.example.invalid&idle-session-check-interval=30&idle-session-timeout=30&min-idle-session=0&disable-reuse=false&client-fingerprint=chrome"},
		{"trojan-grpc", "trojan://synthetic-password@192.0.2.1:443?network=grpc&grpc-opts=" + url.QueryEscape(`{"grpc-service-name":"synthetic","grpc-user-agent":"synthetic","ping-interval":30,"max-connections":1}`)},
		{"trojan-reality", "trojan://synthetic-password@192.0.2.1:443?security=reality&pbk=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&sid=abcd&fp=chrome&sni=tls.example.invalid"},
		{"reality", syntheticRealityURI},
		{"xhttp-cleartext", syntheticXHTTPURI},
		{"xhttp-download", baseVLESS + "type=xhttp&security=tls&sni=tls.example.invalid&extra=" + url.QueryEscape(xhttpExtra)},
		{"xhttp-native", baseVLESS + "network=xhttp&tls=true&servername=tls.example.invalid&xhttp-opts=" + url.QueryEscape(`{"mode":"stream-one","path":"/synthetic","x-padding-bytes":"100-1000","reuse-settings":{"max-concurrency":"16-32"}}`)},
		{"ws", baseVLESS + "type=ws&security=tls&path=%2Fsynthetic&ed=2048&eh=Sec-WebSocket-Protocol&ws-opts=" + url.QueryEscape(`{"headers":{"Host":"cdn.example.invalid"},"v2ray-http-upgrade":false}`)},
		{"httpupgrade", baseVLESS + "type=httpupgrade&security=tls&path=%2Fsynthetic&host=cdn.example.invalid"},
		{"http", baseVMess + "network=http&http-opts=" + url.QueryEscape(`{"method":"GET","path":["/synthetic"],"headers":{"Host":["cdn.example.invalid"]}}`)},
		{"h2", baseVMess + "network=h2&tls=true&h2-opts=" + url.QueryEscape(`{"host":["cdn.example.invalid"],"path":"/synthetic"}`)},
		{"mkcp", baseVMess + "type=kcp&headerType=none&seed=synthetic&mkcp-opts=" + url.QueryEscape(`{"mtu":1350,"tti":50,"uplink-capacity":5,"downlink-capacity":20,"congestion":false,"write-buffer":2,"read-buffer":2}`)},
		{"mekya", baseVMess + "network=mekya&mekya-opts=" + url.QueryEscape(`{"url":"https://mekya.example.invalid/","h2-pool-size":1,"max-write-delay":10,"max-request-size":1048576,"polling-interval-initial":100,"kcp":{"seed":"synthetic"}}`)},
		{"smux", baseVMess + "tls=true&smux=" + url.QueryEscape(`{"enabled":true,"protocol":"h2mux","max-connections":4,"min-streams":4,"max-streams":0,"statistic":false,"only-tcp":false,"padding":true,"brutal-opts":{"enabled":false,"up":"50","down":"100"}}`)},
	} {
		t.Run(test.name, func(t *testing.T) {
			output, _, err := generateConfig(context.Background(), newSourceManager(t.TempDir()), defaultConfig(), GenerationRequest{DirectLinks: strings.Split(test.link, "#")[0] + "#" + test.name, Rules: RuleRequest{Preset: "none"}}, false)
			if err != nil {
				t.Fatal(err)
			}
			if test.name == "xhttp-download" {
				proxies, err := parseSubscription(output)
				if err != nil {
					t.Fatal(err)
				}
				opts := proxies[0]["xhttp-opts"].(map[string]any)
				download := opts["download-settings"].(map[string]any)
				if download["server"] != "192.0.2.2" || download["servername"] != "download.example.invalid" || download["reuse-settings"].(map[string]any)["max-concurrency"] != "8-16" || opts["sc-max-each-post-bytes"] != "1000000" {
					t.Fatal("Xray extra mapping lost download, reuse or numeric options")
				}
			}
			validateImportedAIWithMihomo(t, output)
		})
	}
}

func TestExistingURITransportDefaultsKeepIdentity(t *testing.T) {
	for _, test := range []struct {
		network, field string
		options        map[string]any
	}{
		{"ws", "ws-opts", map[string]any{"path": ""}},
		{"grpc", "grpc-opts", map[string]any{"grpc-service-name": ""}},
		{"h2", "h2-opts", map[string]any{"path": ""}},
		{"tcp&headerType=http", "http-opts", map[string]any{"path": []string{""}}},
	} {
		t.Run(test.field, func(t *testing.T) {
			proxy, err := parseNodeURI("vless://synthetic-uuid@192.0.2.1:443?type=" + test.network + "#Legacy")
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(proxy[test.field], test.options) || proxy["tls"] != false {
				t.Fatal("legacy transport defaults changed node identity")
			}
		})
	}
	for _, link := range []string{
		"vless://synthetic-uuid@192.0.2.1:443?type=tcp",
		"tuic://synthetic-token@192.0.2.1:443?congestion_control=&udp_relay_mode=",
		"hysteria2://synthetic-password@192.0.2.1:443?obfs=&obfs-password=&up=&down=",
		"trojan://synthetic-password@192.0.2.1:443?",
		"anytls://synthetic-password@192.0.2.1:443?",
	} {
		before, err := parseNodeURI(link)
		if err != nil {
			t.Fatal(err)
		}
		suffix := "&sni=&alpn="
		if strings.HasPrefix(link, "vless:") {
			suffix += "&flow="
		}
		after, err := parseNodeURI(link + suffix)
		if err != nil || !reflect.DeepEqual(before, after) {
			t.Fatal("empty legacy TLS options changed node identity", err)
		}
	}
}
