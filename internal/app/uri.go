package app

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
)

type URILine struct {
	Line   int            `json:"line"`
	ID     string         `json:"id,omitempty"`
	NodeID string         `json:"node_id,omitempty"`
	Name   string         `json:"name,omitempty"`
	Type   string         `json:"type,omitempty"`
	Error  string         `json:"error,omitempty"`
	Proxy  map[string]any `json:"-"`
}

func parseURILines(text string) []URILine {
	lines := make([]URILine, 0)
	for index, raw := range strings.Split(text, "\n") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		line := URILine{Line: index + 1}
		proxy, err := parseNodeURI(raw)
		if err != nil {
			line.Error = err.Error()
		} else {
			line.ID = "uri-" + sourceKey(Source{URL: raw})
			line.NodeID, _ = nodeIdentity(proxy)
			line.Proxy = proxy
			line.Name, _ = proxy["name"].(string)
			line.Type, _ = proxy["type"].(string)
		}
		lines = append(lines, line)
	}
	return lines
}

func decodeURIBase64(value string) ([]byte, error) {
	for _, encoding := range []*base64.Encoding{base64.RawURLEncoding, base64.URLEncoding, base64.RawStdEncoding, base64.StdEncoding} {
		if decoded, err := encoding.DecodeString(value); err == nil {
			return decoded, nil
		}
	}
	return nil, errors.New("invalid Base64 node data")
}

func parseNodeURI(raw string) (map[string]any, error) {
	scheme, body, found := strings.Cut(raw, "://")
	if !found {
		return nil, errors.New("expected a node protocol link")
	}
	scheme = strings.ToLower(scheme)
	if scheme == "vmess" && !strings.Contains(body, "@") {
		return parseVMessJSON(body)
	}
	if scheme == "ss" && !strings.Contains(strings.Split(body, "#")[0], "@") {
		encoded, fragment, _ := strings.Cut(body, "#")
		decoded, err := decodeURIBase64(encoded)
		if err != nil {
			return nil, err
		}
		raw = "ss://" + string(decoded)
		if fragment != "" {
			raw += "#" + fragment
		}
	}
	switch scheme {
	case "ss", "tuic", "vless", "vmess", "trojan", "hysteria2", "hy2", "anytls":
	default:
		return nil, errors.New("unsupported node protocol")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User == nil || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("invalid node address or credentials")
	}
	port := u.Port()
	if port == "" && (scheme == "hy2" || scheme == "hysteria2" || scheme == "anytls") {
		port = "443"
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return nil, errors.New("node port must be between 1 and 65535")
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return nil, errors.New("invalid node query parameters")
	}
	for _, values := range query {
		if len(values) != 1 {
			return nil, errors.New("duplicate node query parameter")
		}
	}
	name := u.Fragment
	if name == "" {
		name = scheme + " 节点"
	}
	proxy := map[string]any{"name": name, "type": scheme, "server": u.Hostname(), "port": portNumber}
	user := u.User.Username()
	password, hasPassword := u.User.Password()
	switch scheme {
	case "ss":
		if !hasPassword {
			decoded, err := decodeURIBase64(user)
			if err != nil {
				return nil, err
			}
			user, password, hasPassword = strings.Cut(string(decoded), ":")
		}
		if !hasPassword || user == "" || password == "" {
			return nil, errors.New("Shadowsocks requires a cipher and password")
		}
		proxy["cipher"], proxy["password"] = user, password
		if plugin := takeURI(query, "plugin"); plugin != "" {
			if err := parseSSPlugin(proxy, plugin); err != nil {
				return nil, err
			}
		}
	case "vless", "vmess":
		if hasPassword || user == "" {
			return nil, errors.New("node requires a UUID credential")
		}
		proxy["uuid"] = user
		if scheme == "vmess" {
			proxy["alterId"] = 0
			proxy["cipher"] = "auto"
			if cipher := takeURI(query, "encryption"); cipher != "" {
				proxy["cipher"] = cipher
			}
		} else {
			if encryption := takeURI(query, "encryption"); encryption != "" && encryption != "none" {
				return nil, errors.New("unsupported VLESS encryption")
			}
			if flow := takeURI(query, "flow"); flow != "" {
				proxy["flow"] = flow
			}
		}
		if err := parseURITransport(proxy, query); err != nil {
			return nil, err
		}
	case "trojan":
		if hasPassword || user == "" {
			return nil, errors.New("Trojan requires a password credential")
		}
		proxy["password"] = user
		if err := parseURITransport(proxy, query); err != nil {
			return nil, err
		}
	case "tuic":
		if user == "" || (hasPassword && password == "") {
			return nil, errors.New("TUIC requires a token or UUID and password")
		}
		if hasPassword {
			proxy["uuid"], proxy["password"] = user, password
		} else {
			proxy["token"] = user
		}
		for source, target := range map[string]string{"congestion_control": "congestion-controller", "udp_relay_mode": "udp-relay-mode"} {
			if value := takeURI(query, source); value != "" {
				proxy[target] = value
			}
		}
		if err := takeURIBool(query, proxy, "disable_sni", "disable-sni"); err != nil {
			return nil, err
		}
	case "hysteria2", "hy2", "anytls":
		if user == "" {
			return nil, errors.New("node requires a password credential")
		}
		if hasPassword {
			user += ":" + password
		}
		proxy["password"] = user
		if scheme != "anytls" {
			proxy["type"] = "hysteria2"
			for _, key := range []string{"obfs", "obfs-password", "up", "down"} {
				if value := takeURI(query, key); value != "" {
					proxy[key] = value
				}
			}
			if pin := takeURI(query, "pinSHA256"); pin != "" {
				proxy["fingerprint"] = pin
			}
		}
	}
	if scheme != "ss" {
		if err := parseURITLS(proxy, query); err != nil {
			return nil, err
		}
	}
	if len(query) != 0 {
		return nil, errors.New("unsupported node parameter; use Mihomo YAML to preserve custom settings")
	}
	return proxy, nil
}

func takeURI(query url.Values, key string) string {
	value := query.Get(key)
	delete(query, key)
	return value
}

func takeURIBool(query url.Values, proxy map[string]any, key, target string) error {
	if _, exists := query[key]; !exists {
		return nil
	}
	value, err := strconv.ParseBool(takeURI(query, key))
	if err != nil {
		return errors.New("invalid boolean node parameter")
	}
	if previous, exists := proxy[target]; exists && previous != value {
		return errors.New("conflicting boolean node parameters")
	}
	proxy[target] = value
	return nil
}

func parseURITLS(proxy map[string]any, query url.Values) error {
	kind, _ := proxy["type"].(string)
	security := takeURI(query, "security")
	if security != "" && security != "none" && security != "tls" && security != "reality" {
		return errors.New("unsupported TLS security mode")
	}
	if kind == "vless" || kind == "vmess" {
		proxy["tls"] = security == "tls" || security == "reality"
	} else if security == "none" || security == "reality" {
		return errors.New("unsupported security mode for this protocol")
	}
	if sni := takeURI(query, "sni"); sni != "" {
		key := "sni"
		if kind == "vless" || kind == "vmess" {
			key = "servername"
		}
		proxy[key] = sni
		if kind == "anytls" && net.ParseIP(sni) != nil {
			return errors.New("AnyTLS IP SNI is not supported; use Mihomo YAML")
		}
	}
	if alpn := takeURI(query, "alpn"); alpn != "" {
		proxy["alpn"] = strings.Split(alpn, ",")
	}
	if fingerprint := takeURI(query, "fp"); fingerprint != "" {
		proxy["client-fingerprint"] = fingerprint
	}
	if pin := takeURI(query, "hpkp"); pin != "" {
		proxy["fingerprint"] = pin
	}
	for _, key := range []string{"insecure", "allowInsecure", "allow_insecure"} {
		if err := takeURIBool(query, proxy, key, "skip-cert-verify"); err != nil {
			return err
		}
	}
	if security == "reality" {
		if kind != "vless" && kind != "vmess" {
			return errors.New("unsupported Reality protocol")
		}
		publicKey := takeURI(query, "pbk")
		if publicKey == "" {
			return errors.New("Reality requires a public key")
		}
		opts := map[string]any{"public-key": publicKey}
		if shortID := takeURI(query, "sid"); shortID != "" {
			opts["short-id"] = shortID
		}
		proxy["reality-opts"] = opts
	}
	return nil
}

func parseURITransport(proxy map[string]any, query url.Values) error {
	network := takeURI(query, "type")
	if network == "http" {
		network = "h2"
	}
	if network == "" || network == "tcp" || network == "raw" {
		if header := takeURI(query, "headerType"); header != "" && header != "none" {
			if header != "http" {
				return errors.New("unsupported TCP header type")
			}
			network = "http"
		} else {
			return nil
		}
	}
	path, host := takeURI(query, "path"), takeURI(query, "host")
	switch network {
	case "ws":
		opts := map[string]any{"path": path}
		if host != "" {
			opts["headers"] = map[string]any{"Host": host}
		}
		proxy["ws-opts"] = opts
	case "grpc":
		if host != "" || path != "" {
			return errors.New("unsupported gRPC host or path; use serviceName")
		}
		if mode := takeURI(query, "mode"); mode != "" && mode != "gun" {
			return errors.New("unsupported gRPC mode")
		}
		proxy["grpc-opts"] = map[string]any{"grpc-service-name": takeURI(query, "serviceName")}
	case "http", "h2":
		opts := map[string]any{"path": path}
		if network == "h2" {
			if host != "" {
				opts["host"] = strings.Split(host, ",")
			}
			proxy["h2-opts"] = opts
		} else {
			opts["path"] = strings.Split(path, ",")
			if host != "" {
				opts["headers"] = map[string]any{"Host": strings.Split(host, ",")}
			}
			proxy["http-opts"] = opts
		}
	default:
		return errors.New("unsupported node transport; use Mihomo YAML")
	}
	proxy["network"] = network
	return nil
}

func parseSSPlugin(proxy map[string]any, plugin string) error {
	parts := strings.Split(plugin, ";")
	if strings.Contains(plugin, "\\") {
		return errors.New("escaped plugin options are not supported; use Mihomo YAML")
	}
	opts := make(map[string]any)
	for _, part := range parts[1:] {
		key, value, found := strings.Cut(part, "=")
		if key == "" || opts[key] != nil {
			return errors.New("invalid or duplicate plugin option")
		}
		if found {
			opts[key] = value
		} else {
			opts[key] = true
		}
	}
	switch parts[0] {
	case "simple-obfs", "obfs-local":
		converted := make(map[string]any)
		for key, value := range opts {
			switch key {
			case "obfs":
				converted["mode"] = value
			case "obfs-host":
				converted["host"] = value
			default:
				return errors.New("unsupported obfs plugin option")
			}
		}
		opts = converted
		proxy["plugin"] = "obfs"
	case "v2ray-plugin":
		for key := range opts {
			if key != "mode" && key != "host" && key != "path" && key != "tls" && key != "mux" {
				return errors.New("unsupported v2ray plugin option")
			}
		}
		proxy["plugin"] = "v2ray-plugin"
	default:
		return errors.New("unsupported Shadowsocks plugin")
	}
	proxy["plugin-opts"] = opts
	return nil
}

func parseVMessJSON(body string) (map[string]any, error) {
	data, err := decodeURIBase64(body)
	if err != nil {
		return nil, err
	}
	values := make(map[string]any)
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	if err := decoder.Decode(&values); err != nil || values == nil {
		return nil, errors.New("invalid VMess JSON node data")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, errors.New("expected one VMess JSON object")
	}
	get := func(key string) string {
		switch value := values[key].(type) {
		case string:
			return value
		case json.Number:
			return value.String()
		}
		return ""
	}
	for key, value := range values {
		if !strings.Contains("|v|ps|add|port|id|aid|scy|net|type|host|path|tls|sni|alpn|fp|insecure|allowInsecure|", "|"+key+"|") {
			return nil, errors.New("unsupported VMess JSON field; use Mihomo YAML")
		}
		if _, ok := value.(string); !ok {
			if _, ok := value.(json.Number); !ok {
				return nil, errors.New("invalid VMess JSON field type")
			}
		}
	}
	port, err := strconv.Atoi(get("port"))
	if err != nil || port < 1 || port > 65535 || get("add") == "" || get("id") == "" {
		return nil, errors.New("VMess requires a server, valid port and UUID")
	}
	aid := 0
	if get("aid") != "" {
		aid, err = strconv.Atoi(get("aid"))
		if err != nil || aid < 0 {
			return nil, errors.New("invalid VMess alterId")
		}
	}
	name := get("ps")
	if name == "" {
		name = "vmess 节点"
	}
	cipher := get("scy")
	if cipher == "" {
		cipher = "auto"
	}
	proxy := map[string]any{"name": name, "type": "vmess", "server": get("add"), "port": port, "uuid": get("id"), "alterId": aid, "cipher": cipher}
	query := make(url.Values)
	for _, key := range []string{"host", "path", "sni", "alpn", "fp", "insecure", "allowInsecure"} {
		if get(key) != "" {
			query.Set(key, get(key))
		}
	}
	network := get("net")
	if network == "http" {
		network = "h2"
	}
	query.Set("type", network)
	if network == "grpc" {
		query.Set("serviceName", takeURI(query, "path"))
		if mode := get("type"); mode != "" && mode != "none" {
			query.Set("mode", mode)
		}
	} else if network == "tcp" || network == "raw" || network == "" {
		query.Set("headerType", get("type"))
	} else if header := get("type"); header != "" && header != "none" {
		return nil, errors.New("unsupported VMess transport header")
	}
	query.Set("security", get("tls"))
	if err := parseURITransport(proxy, query); err != nil {
		return nil, err
	}
	if err := parseURITLS(proxy, query); err != nil {
		return nil, err
	}
	if len(query) != 0 {
		return nil, errors.New("unsupported VMess transport parameters")
	}
	return proxy, nil
}
