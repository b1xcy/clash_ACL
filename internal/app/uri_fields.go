package app

import (
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"reflect"
	"strconv"
	"strings"
)

var mihomoURIProtocols = map[string]string{
	"ss": "ShadowSocksOption", "tuic": "TuicOption", "vless": "VlessOption",
	"vmess": "VmessOption", "trojan": "TrojanOption", "hysteria2": "Hysteria2Option",
	"hy2": "Hysteria2Option", "anytls": "AnyTLSOption",
}

func decodeURIJSON(raw string) (any, error) {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil || value == nil {
		return nil, errors.New("invalid JSON node option")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, errors.New("expected one JSON node option")
	}
	return value, nil
}

func normalizeMihomoURIValue(value any, kind string) (any, error) {
	invalid := errors.New("invalid Mihomo node option type or value")
	if kind == "any" {
		return value, nil
	}
	if strings.HasPrefix(kind, "[]") {
		if raw, ok := value.(string); ok && kind == "[]string" {
			return strings.Split(raw, ","), nil
		}
		if kind == "[]string" {
			if values, err := stringList(value); err == nil {
				return values, nil
			}
		}
		values, ok := value.([]any)
		if !ok {
			return nil, invalid
		}
		out := make([]any, len(values))
		for i, item := range values {
			normalized, err := normalizeMihomoURIValue(item, strings.TrimPrefix(kind, "[]"))
			if err != nil {
				return nil, err
			}
			out[i] = normalized
		}
		return out, nil
	}
	if fields, known := mihomoURIFields[kind]; known || strings.HasPrefix(kind, "map:") {
		values, ok := value.(map[string]any)
		if !ok {
			return nil, invalid
		}
		out := make(map[string]any, len(values))
		for key, item := range values {
			child := strings.TrimPrefix(kind, "map:")
			if known {
				var exists bool
				child, exists = fields[key]
				if !exists {
					return nil, errors.New("unsupported nested Mihomo node option")
				}
			}
			normalized, err := normalizeMihomoURIValue(item, child)
			if err != nil {
				return nil, err
			}
			out[key] = normalized
		}
		return out, nil
	}
	switch kind {
	case "string":
		if value, ok := value.(string); ok {
			return value, nil
		}
		if value, ok := value.(json.Number); ok {
			return value.String(), nil
		}
	case "bool":
		if value, ok := value.(bool); ok {
			return value, nil
		}
	case "int", "int32", "uint16", "uint32", "uint64":
		raw := ""
		switch value := value.(type) {
		case json.Number:
			raw = value.String()
		case int:
			raw = strconv.Itoa(value)
		}
		bits := 64
		if kind == "int32" || kind == "uint32" {
			bits = 32
		} else if kind == "uint16" {
			bits = 16
		}
		if strings.HasPrefix(kind, "uint") {
			if _, err := strconv.ParseUint(raw, 10, bits); err == nil {
				return json.Number(raw), nil
			}
		} else if _, err := strconv.ParseInt(raw, 10, bits); err == nil {
			return json.Number(raw), nil
		}
	case "float64":
		if value, ok := value.(json.Number); ok {
			if _, err := value.Float64(); err == nil {
				return value, nil
			}
		}
	}
	return nil, invalid
}

func takeMihomoURIFields(protocol string, query url.Values) (map[string]any, error) {
	fields := mihomoURIFields[mihomoURIProtocols[protocol]]
	return takeMihomoURIOptions(fields, query)
}

func takeMihomoURIOptions(fields map[string]string, query url.Values) (map[string]any, error) {
	out := map[string]any{}
	for key, values := range query {
		switch key {
		case "sni", "alpn", "flow", "obfs", "obfs-password", "up", "down", "congestion_control", "udp_relay_mode":
			if values[0] == "" {
				delete(query, key)
				continue
			}
		}
		if key == "network" || key == "plugin" || key == "encryption" {
			continue
		}
		target := key
		if _, exists := fields[target]; !exists {
			target = strings.ReplaceAll(key, "_", "-")
		}
		kind, exists := fields[target]
		if !exists {
			continue
		}
		var value any = values[0]
		if kind != "string" {
			var err error
			if kind == "bool" {
				value, err = strconv.ParseBool(values[0])
			} else if kind == "[]string" && !strings.HasPrefix(strings.TrimSpace(values[0]), "[") {
				value = strings.Split(values[0], ",")
			} else {
				value, err = decodeURIJSON(values[0])
			}
			if err != nil {
				return nil, errors.New("invalid Mihomo node query option")
			}
		}
		normalized, err := normalizeMihomoURIValue(value, kind)
		if err != nil {
			return nil, err
		}
		if err := mergeMihomoURIFields(out, map[string]any{target: normalized}); err != nil {
			return nil, err
		}
		delete(query, key)
	}
	return out, nil
}

func mergeMihomoURIFields(target, source map[string]any) error {
	for key, value := range source {
		if previous, exists := target[key]; exists {
			previousMap, previousOK := previous.(map[string]any)
			valueMap, valueOK := value.(map[string]any)
			if previousOK && valueOK {
				if err := mergeMihomoURIFields(previousMap, valueMap); err != nil {
					return err
				}
				continue
			}
			before, _ := json.Marshal(previous)
			after, _ := json.Marshal(value)
			if !reflect.DeepEqual(before, after) {
				return errors.New("conflicting node parameters")
			}
		}
		target[key] = value
	}
	return nil
}

func validateMihomoURIFields(proxy map[string]any) error {
	protocol, _ := proxy["type"].(string)
	fields := mihomoURIFields[mihomoURIProtocols[protocol]]
	for key, value := range proxy {
		if kind, exists := fields[key]; exists {
			if _, err := normalizeMihomoURIValue(value, kind); err != nil {
				return err
			}
		} else if key != "type" {
			return errors.New("unsupported Mihomo option for this protocol")
		}
	}
	if network, _ := proxy["network"].(string); network != "" {
		allowed := map[string]string{"vless": "|tcp|raw|ws|http|h2|grpc|xhttp|", "vmess": "|tcp|raw|ws|http|h2|grpc|mkcp|mekya|", "trojan": "|tcp|raw|ws|grpc|"}
		if !strings.Contains(allowed[protocol], "|"+network+"|") {
			return errors.New("unsupported transport for this protocol")
		}
	}
	if plugin, _ := proxy["plugin"].(string); plugin != "" {
		kind := map[string]string{"obfs": "simpleObfsOption", "v2ray-plugin": "v2rayObfsOption", "gost-plugin": "gostObfsOption", "shadow-tls": "shadowTLSOption", "restls": "restlsOption", "jls": "jlsOption", "kcptun": "kcpTunOption"}[plugin]
		if kind == "" {
			return errors.New("unsupported Shadowsocks plugin")
		}
		if opts, exists := proxy["plugin-opts"]; exists {
			if _, err := normalizeMihomoURIValue(opts, kind); err != nil {
				return err
			}
		}
	}
	return nil
}
