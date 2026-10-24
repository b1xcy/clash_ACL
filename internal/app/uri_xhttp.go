package app

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
)

var xhttpURIAliases = map[string]string{
	"noGRPCHeader": "no-grpc-header", "xPaddingBytes": "x-padding-bytes",
	"xPaddingObfsMode": "x-padding-obfs-mode", "xPaddingKey": "x-padding-key",
	"xPaddingHeader": "x-padding-header", "xPaddingPlacement": "x-padding-placement",
	"xPaddingMethod": "x-padding-method", "uplinkHTTPMethod": "uplink-http-method",
	"sessionIDPlacement": "session-placement", "sessionPlacement": "session-placement",
	"sessionIDKey": "session-key", "sessionKey": "session-key",
	"sessionIDTable": "session-table", "sessionIDLength": "session-length",
	"seqPlacement": "seq-placement", "seqKey": "seq-key",
	"uplinkDataPlacement": "uplink-data-placement", "uplinkDataKey": "uplink-data-key",
	"uplinkChunkSize": "uplink-chunk-size", "scMaxEachPostBytes": "sc-max-each-post-bytes",
	"scMinPostsIntervalMs": "sc-min-posts-interval-ms", "xmux": "reuse-settings",
	"downloadSettings": "download-settings",
}

var xhttpReuseAliases = map[string]string{
	"maxConcurrency": "max-concurrency", "maxConnections": "max-connections",
	"cMaxReuseTimes": "c-max-reuse-times", "hMaxRequestTimes": "h-max-request-times",
	"hMaxReusableSecs": "h-max-reusable-secs", "hKeepAlivePeriod": "h-keep-alive-period",
}

func parseURIXHTTP(query url.Values) (map[string]any, error) {
	out := map[string]any{}
	if raw := takeURI(query, "extra"); raw != "" {
		value, err := decodeURIJSON(raw)
		if err != nil {
			return nil, err
		}
		out, err = normalizeXHTTPOptions(value, "XHTTPOptions", xhttpURIAliases)
		if err != nil {
			return nil, err
		}
	}
	for key, values := range query {
		target := key
		if alias, exists := xhttpURIAliases[key]; exists {
			target = alias
		} else {
			target = strings.ReplaceAll(target, "_", "-")
		}
		kind, exists := mihomoURIFields["XHTTPOptions"][target]
		if !exists || target == "path" || target == "host" {
			continue
		}
		var value any = values[0]
		if kind != "string" {
			var err error
			if kind == "bool" {
				value, err = strconv.ParseBool(values[0])
			} else {
				value, err = decodeURIJSON(values[0])
			}
			if err != nil {
				return nil, errors.New("invalid XHTTP query option")
			}
		}
		options, err := normalizeXHTTPOptions(map[string]any{target: value}, "XHTTPOptions", xhttpURIAliases)
		if err != nil {
			return nil, err
		}
		if err := mergeMihomoURIFields(out, options); err != nil {
			return nil, err
		}
		delete(query, key)
	}
	if mode, _ := out["mode"].(string); mode != "" && mode != "auto" && mode != "packet-up" && mode != "stream-up" && mode != "stream-one" {
		return nil, errors.New("unsupported XHTTP mode")
	}
	return out, nil
}

func normalizeXHTTPOptions(value any, kind string, aliases map[string]string) (map[string]any, error) {
	input, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("XHTTP options must be a JSON object")
	}
	out := map[string]any{}
	for key, value := range input {
		target := key
		if alias, exists := aliases[key]; exists {
			target = alias
		}
		var err error
		switch target {
		case "reuse-settings":
			value, err = normalizeXHTTPOptions(value, "XHTTPReuseSettings", xhttpReuseAliases)
		case "download-settings":
			value, err = normalizeXHTTPDownload(value)
		}
		if err != nil {
			return nil, err
		}
		if err := mergeMihomoURIFields(out, map[string]any{target: value}); err != nil {
			return nil, err
		}
	}
	value, err := normalizeMihomoURIValue(out, kind)
	if err != nil {
		return nil, err
	}
	return value.(map[string]any), nil
}

func normalizeXHTTPDownload(value any) (map[string]any, error) {
	input, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("XHTTP download settings must be a JSON object")
	}
	out := map[string]any{}
	for key, value := range input {
		options := map[string]any{}
		switch key {
		case "address":
			options["server"] = value
		case "security":
			security, ok := value.(string)
			if !ok || (security != "none" && security != "tls" && security != "reality" && security != "") {
				return nil, errors.New("unsupported XHTTP download security")
			}
			options["tls"] = security == "tls" || security == "reality"
		case "tlsSettings", "realitySettings":
			settings, ok := value.(map[string]any)
			if !ok {
				return nil, errors.New("invalid XHTTP download TLS settings")
			}
			reality := map[string]any{}
			for name, item := range settings {
				switch name {
				case "serverName":
					options["servername"] = item
				case "fingerprint":
					options["client-fingerprint"] = item
				case "allowInsecure":
					options["skip-cert-verify"] = item
				case "publicKey":
					reality["public-key"] = item
				case "shortId":
					reality["short-id"] = item
				case "supportX25519MLKEM768", "support-x25519mlkem768":
					reality["support-x25519mlkem768"] = item
				case "spiderX":
					if key != "realitySettings" {
						return nil, errors.New("unsupported XHTTP download TLS option")
					}
				default:
					if key == "realitySettings" {
						return nil, errors.New("unsupported XHTTP download Reality option")
					}
					options[name] = item
				}
			}
			if len(reality) > 0 {
				options["reality-opts"] = reality
			}
		case "xhttpSettings":
			settings, ok := value.(map[string]any)
			if !ok {
				return nil, errors.New("invalid XHTTP download transport settings")
			}
			for name, item := range settings {
				if name == "extra" {
					nested, err := normalizeXHTTPOptions(item, "XHTTPDownloadSettings", xhttpURIAliases)
					if err != nil {
						return nil, err
					}
					if err := mergeMihomoURIFields(options, nested); err != nil {
						return nil, err
					}
				} else {
					options[name] = item
				}
			}
		default:
			options[key] = value
		}
		if err := mergeMihomoURIFields(out, options); err != nil {
			return nil, err
		}
	}
	return normalizeXHTTPOptions(out, "XHTTPDownloadSettings", xhttpURIAliases)
}
