package app

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
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
	canonical, err := takeMihomoURIFields(scheme, query)
	if err != nil {
		return nil, err
	}
	name := u.Fragment
	if name == "" {
		name, _ = canonical["name"].(string)
		if name == "" {
			name = scheme + " 节点"
		}
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
			if _, exists := canonical["alterId"]; !exists {
				proxy["alterId"] = 0
			}
			if _, exists := canonical["cipher"]; !exists {
				proxy["cipher"] = "auto"
			}
			if cipher := takeURI(query, "encryption"); cipher != "" {
				proxy["cipher"] = cipher
			}
		} else {
			if encryption := takeURI(query, "encryption"); encryption != "" && encryption != "none" {
				proxy["encryption"] = encryption
			}
			if flow := takeURI(query, "flow"); flow != "" {
				proxy["flow"] = flow
			}
		}
		if err := parseURITransport(proxy, query, canonical); err != nil {
			return nil, err
		}
	case "trojan":
		if hasPassword || user == "" {
			return nil, errors.New("Trojan requires a password credential")
		}
		proxy["password"] = user
		if err := parseURITransport(proxy, query, canonical); err != nil {
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
		if packet := takeURI(query, "packetEncoding"); packet != "" {
			if packet == "packet" {
				packet = "packetaddr"
			}
			if packet != "none" {
				if err := mergeMihomoURIFields(proxy, map[string]any{"packet-encoding": packet}); err != nil {
					return nil, err
				}
			}
		}
		if reality, exists := canonical["reality-opts"]; exists {
			proxy["reality-opts"] = reality
		}
		if err := parseURITLS(proxy, query); err != nil {
			return nil, err
		}
	}
	if err := mergeMihomoURIFields(proxy, canonical); err != nil {
		return nil, err
	}
	if scheme == "vless" || scheme == "vmess" {
		if _, exists := proxy["tls"]; !exists {
			proxy["tls"] = false
		}
	}
	if err := validateMihomoURIFields(proxy); err != nil {
		return nil, err
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
		if security != "" {
			proxy["tls"] = security == "tls" || security == "reality"
		}
	} else if security == "none" || (security == "reality" && kind != "trojan") {
		return errors.New("unsupported security mode for this protocol")
	}
	if sni := takeURI(query, "sni"); sni != "" {
		key := "sni"
		if kind == "vless" || kind == "vmess" {
			key = "servername"
		}
		proxy[key] = sni
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
		if kind != "vless" && kind != "vmess" && kind != "trojan" {
			return errors.New("unsupported Reality protocol")
		}
		opts := map[string]any{}
		if existing, ok := proxy["reality-opts"].(map[string]any); ok {
			for key, value := range existing {
				opts[key] = value
			}
		}
		if publicKey := takeURI(query, "pbk"); publicKey != "" {
			if err := mergeMihomoURIFields(opts, map[string]any{"public-key": publicKey}); err != nil {
				return err
			}
		}
		if publicKey, _ := opts["public-key"].(string); publicKey == "" {
			return errors.New("Reality requires a public key")
		}
		if shortID := takeURI(query, "sid"); shortID != "" {
			if err := mergeMihomoURIFields(opts, map[string]any{"short-id": shortID}); err != nil {
				return err
			}
		}
		if err := takeURIBool(query, opts, "support-x25519mlkem768", "support-x25519mlkem768"); err != nil {
			return err
		}
		takeURI(query, "spx")
		takeURI(query, "spiderX")
		proxy["reality-opts"] = opts
	}
	return nil
}

func parseURITransport(proxy map[string]any, query url.Values, canonical map[string]any) error {
	network := takeURI(query, "type")
	nativeNetwork := takeURI(query, "network")
	if network == "http" && nativeNetwork == "" {
		network = "h2"
	}
	if nativeNetwork != "" {
		if network != "" && network != nativeNetwork {
			return errors.New("conflicting transport parameters")
		}
		network = nativeNetwork
	}
	if network == "" || network == "tcp" || network == "raw" {
		if header := takeURI(query, "headerType"); header != "" && header != "none" {
			if header != "http" {
				return errors.New("unsupported TCP header type")
			}
			network = "http"
		} else {
			if nativeNetwork != "" {
				proxy["network"] = nativeNetwork
			}
			return nil
		}
	}
	path, host := takeURI(query, "path"), takeURI(query, "host")
	if proxy["type"] == "trojan" && network != "ws" && network != "httpupgrade" && network != "grpc" {
		return errors.New("unsupported Trojan transport; use tcp, ws or grpc")
	}
	switch network {
	case "xhttp":
		if proxy["type"] != "vless" {
			return errors.New("XHTTP transport is only supported for VLESS")
		}
		opts, err := parseURIXHTTP(query)
		if err != nil {
			return err
		}
		if path != "" {
			if err := mergeMihomoURIFields(opts, map[string]any{"path": path}); err != nil {
				return err
			}
		}
		if host != "" {
			if err := mergeMihomoURIFields(opts, map[string]any{"host": host}); err != nil {
				return err
			}
		}
		proxy["xhttp-opts"] = opts
	case "mkcp", "kcp", "mekya":
		if proxy["type"] != "vmess" {
			return errors.New("mKCP and Mekya transports are only supported for VMess")
		}
		if network == "kcp" {
			network = "mkcp"
		}
		if network == "mkcp" {
			if path != "" || host != "" {
				return errors.New("mKCP requires mkcp-opts instead of host or path")
			}
			opts := map[string]any{}
			if seed := takeURI(query, "seed"); seed != "" {
				opts["seed"] = seed
			}
			if header := takeURI(query, "headerType"); header != "" {
				opts["header"] = header
			}
			proxy["mkcp-opts"] = opts
		} else if path != "" || host != "" {
			return errors.New("Mekya requires mekya-opts instead of host or path")
		}
	case "ws", "httpupgrade":
		opts := map[string]any{"path": path}
		if native, ok := canonical["ws-opts"].(map[string]any); path == "" && ok {
			if _, exists := native["path"]; exists {
				delete(opts, "path")
			}
		}
		if network == "httpupgrade" {
			network = "ws"
			opts["v2ray-http-upgrade"] = true
		}
		if host != "" {
			opts["headers"] = map[string]any{"Host": host}
		}
		if earlyData := takeURI(query, "ed"); earlyData != "" {
			value, err := strconv.Atoi(earlyData)
			if err != nil || value < 0 {
				return errors.New("invalid WebSocket early data length")
			}
			opts["max-early-data"] = value
		}
		if header := takeURI(query, "earlyDataHeaderName"); header != "" {
			opts["early-data-header-name"] = header
		}
		if header := takeURI(query, "eh"); header != "" {
			if err := mergeMihomoURIFields(opts, map[string]any{"early-data-header-name": header}); err != nil {
				return err
			}
		}
		proxy["ws-opts"] = opts
	case "grpc":
		if host != "" || path != "" {
			return errors.New("unsupported gRPC host or path; use serviceName")
		}
		if mode := takeURI(query, "mode"); mode != "" && mode != "gun" {
			return errors.New("unsupported gRPC mode")
		}
		service := takeURI(query, "serviceName")
		opts := map[string]any{"grpc-service-name": service}
		if native, ok := canonical["grpc-opts"].(map[string]any); service == "" && ok {
			if _, exists := native["grpc-service-name"]; exists {
				delete(opts, "grpc-service-name")
			}
		}
		proxy["grpc-opts"] = opts
	case "http", "h2":
		opts := map[string]any{"path": path}
		if network == "h2" {
			if native, ok := canonical["h2-opts"].(map[string]any); path == "" && ok {
				if _, exists := native["path"]; exists {
					delete(opts, "path")
				}
			}
			if host != "" {
				opts["host"] = strings.Split(host, ",")
			}
			proxy["h2-opts"] = opts
		} else {
			opts["path"] = strings.Split(path, ",")
			if native, ok := canonical["http-opts"].(map[string]any); path == "" && ok {
				if _, exists := native["path"]; exists {
					delete(opts, "path")
				}
			}
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
	case "obfs", "v2ray-plugin", "gost-plugin", "shadow-tls", "restls", "jls", "kcptun":
		proxy["plugin"] = parts[0]
	default:
		return errors.New("unsupported Shadowsocks plugin")
	}
	kind := map[string]string{"obfs": "simpleObfsOption", "v2ray-plugin": "v2rayObfsOption", "gost-plugin": "gostObfsOption", "shadow-tls": "shadowTLSOption", "restls": "restlsOption", "jls": "jlsOption", "kcptun": "kcpTunOption"}[proxy["plugin"].(string)]
	options := url.Values{}
	for key, value := range opts {
		if flag, ok := value.(bool); ok {
			options.Set(key, strconv.FormatBool(flag))
		} else {
			options.Set(key, value.(string))
		}
	}
	normalized, err := takeMihomoURIOptions(mihomoURIFields[kind], options)
	if err != nil {
		return err
	}
	if len(options) != 0 {
		return errors.New("unsupported Shadowsocks plugin option")
	}
	proxy["plugin-opts"] = normalized
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
	canonical := map[string]any{}
	for key, value := range values {
		if !strings.Contains("|v|ps|add|port|id|aid|scy|net|type|host|path|tls|sni|alpn|fp|insecure|allowInsecure|", "|"+key+"|") {
			kind, exists := mihomoURIFields["VmessOption"][key]
			if !exists {
				return nil, errors.New("unsupported VMess JSON field")
			}
			normalized, err := normalizeMihomoURIValue(value, kind)
			if err != nil {
				return nil, err
			}
			canonical[key] = normalized
			continue
		}
		if key == "tls" {
			if enabled, ok := value.(bool); ok {
				canonical[key] = enabled
				continue
			}
		}
		if key == "alpn" {
			if _, ok := value.([]any); ok {
				normalized, err := normalizeMihomoURIValue(value, "[]string")
				if err != nil {
					return nil, err
				}
				canonical[key] = normalized
				continue
			}
		}
		if _, ok := value.(string); !ok {
			if _, ok := value.(json.Number); !ok {
				return nil, errors.New("invalid VMess JSON field type")
			}
		}
	}
	server, uuid := get("add"), get("id")
	if server == "" {
		server, _ = canonical["server"].(string)
	}
	if uuid == "" {
		uuid, _ = canonical["uuid"].(string)
	}
	port, err := strconv.Atoi(get("port"))
	if err != nil || port < 1 || port > 65535 || server == "" || uuid == "" {
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
		name, _ = canonical["name"].(string)
		if name == "" {
			name = "vmess 节点"
		}
	}
	cipher := get("scy")
	if cipher == "" {
		cipher = "auto"
	}
	proxy := map[string]any{"name": name, "type": "vmess", "server": server, "port": port, "uuid": uuid, "alterId": aid, "cipher": cipher}
	if _, exists := canonical["alterId"]; exists && get("aid") == "" {
		delete(proxy, "alterId")
	}
	if _, exists := canonical["cipher"]; exists && get("scy") == "" {
		delete(proxy, "cipher")
	}
	query := make(url.Values)
	for _, key := range []string{"host", "path", "sni", "alpn", "fp", "insecure", "allowInsecure"} {
		if get(key) != "" {
			query.Set(key, get(key))
		}
	}
	network := get("net")
	if native, ok := canonical["network"].(string); ok && network == "" {
		query.Set("network", native)
		network = native
	}
	if network == "http" && query.Get("network") == "" {
		network = "h2"
	}
	query.Set("type", network)
	if network == "grpc" {
		query.Set("serviceName", takeURI(query, "path"))
		if mode := get("type"); mode != "" && mode != "none" && mode != "vmess" {
			query.Set("mode", mode)
		}
	} else if network == "tcp" || network == "raw" || network == "" || network == "kcp" || network == "mkcp" {
		if header := get("type"); header != "" && header != "vmess" {
			query.Set("headerType", header)
		}
	} else if header := get("type"); header != "" && header != "none" && header != "vmess" {
		return nil, errors.New("unsupported VMess transport header")
	}
	query.Set("security", get("tls"))
	if err := parseURITransport(proxy, query, canonical); err != nil {
		return nil, err
	}
	if err := parseURITLS(proxy, query); err != nil {
		return nil, err
	}
	if err := mergeMihomoURIFields(proxy, canonical); err != nil {
		return nil, err
	}
	if _, exists := proxy["tls"]; !exists {
		proxy["tls"] = false
	}
	if err := validateMihomoURIFields(proxy); err != nil {
		return nil, err
	}
	if len(query) != 0 {
		return nil, errors.New("unsupported VMess transport parameters")
	}
	return proxy, nil
}
