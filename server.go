package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
)

func newHandler(configPath string) http.Handler {
	mux := http.NewServeMux()
	var configMu sync.RWMutex
	sourceManager := newSourceManager(filepath.Dir(configPath))
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(indexHTML)
	})
	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) {
		configMu.RLock()
		defer configMu.RUnlock()
		config, err := readConfig(configPath)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot read configuration"})
			return
		}
		enabled := 0
		sources := make([]SourceView, 0, len(config.Sources))
		for _, source := range config.Sources {
			if source.Enabled {
				enabled++
			}
			sources = append(sources, sourceManager.view(source))
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"capabilities": map[string]bool{"conversion": false, "uri_import": false}, "sources": sources,
			"counts": map[string]int{"sources": len(config.Sources), "enabled": enabled, "self_hosted": len(config.SelfHostedNodes), "groups": len(config.Groups)},
		})
	})
	mux.HandleFunc("GET /api/options", func(w http.ResponseWriter, r *http.Request) {
		configMu.RLock()
		defer configMu.RUnlock()
		config, err := readConfig(configPath)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot read configuration"})
			return
		}
		groups, err := json.Marshal(config.Groups)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot read group settings"})
			return
		}
		if config.Groups == nil {
			groups = []byte("[]")
		}
		nodes := make([]map[string]string, 0, len(config.SelfHostedNodes))
		for _, node := range config.SelfHostedNodes {
			name, _ := node.Proxy["name"].(string)
			nodes = append(nodes, map[string]string{"id": node.ID, "name": name})
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"options": ConversionOptions{RenameRules: config.RenameRules, AI: config.AI, GroupsJSON: string(groups)}, "self_hosted_nodes": nodes,
		})
	})
	mux.HandleFunc("PUT /api/options", func(w http.ResponseWriter, r *http.Request) {
		reader := http.MaxBytesReader(w, r.Body, 4<<20)
		defer reader.Close()
		options, groups, err := decodeOptions(reader)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		configMu.Lock()
		defer configMu.Unlock()
		config, err := readConfig(configPath)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot read configuration"})
			return
		}
		config.RenameRules, config.AI, config.Groups = options.RenameRules, options.AI, groups
		if err := validateConfig(config); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if err := writeConfig(configPath, config); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot save conversion settings"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
	})
	mux.HandleFunc("GET /api/sources", func(w http.ResponseWriter, r *http.Request) {
		configMu.RLock()
		defer configMu.RUnlock()
		config, err := readConfig(configPath)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot read configuration"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"sources": config.Sources})
	})
	mux.HandleFunc("PUT /api/sources", func(w http.ResponseWriter, r *http.Request) {
		reader := http.MaxBytesReader(w, r.Body, 4<<20)
		defer reader.Close()
		var sources []Source
		decoder := json.NewDecoder(reader)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&sources); err != nil || sources == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "expected a JSON source array"})
			return
		}
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "expected one JSON source array"})
			return
		}
		configMu.Lock()
		defer configMu.Unlock()
		config, err := readConfig(configPath)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot read configuration"})
			return
		}
		config.Sources = sources
		if err := validateConfig(config); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if err := writeConfig(configPath, config); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot save sources"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
	})
	mux.HandleFunc("POST /api/sources/refresh", func(w http.ResponseWriter, r *http.Request) {
		configMu.RLock()
		config, err := readConfig(configPath)
		configMu.RUnlock()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot read configuration"})
			return
		}
		views := make([]SourceView, len(config.Sources))
		for index, source := range config.Sources {
			sourceManager.startRefresh(source, config.FetchConcurrency)
			views[index] = sourceManager.view(source)
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"sources": views})
	})
	mux.HandleFunc("GET /api/sources/{id}/preview", func(w http.ResponseWriter, r *http.Request) {
		configMu.RLock()
		config, err := readConfig(configPath)
		configMu.RUnlock()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot read configuration"})
			return
		}
		for _, source := range config.Sources {
			if source.ID != r.PathValue("id") {
				continue
			}
			cache, err := sourceManager.readCache(source)
			if err != nil || cache.LastSuccess == nil {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "source has no successful cache yet"})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"nodes": previewNodes(cache.Proxies), "source": sourceManager.view(source)})
			return
		}
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "source not found"})
	})
	mux.HandleFunc("POST /api/yaml/preview", func(w http.ResponseWriter, r *http.Request) {
		reader := http.MaxBytesReader(w, r.Body, maxSubscriptionSize)
		defer reader.Close()
		data, err := io.ReadAll(reader)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cannot read YAML; maximum size is 8 MiB"})
			return
		}
		proxies, err := parseSubscription(data)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"nodes": previewNodes(proxies), "node_count": len(proxies)})
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /api/config", func(w http.ResponseWriter, r *http.Request) {
		configMu.RLock()
		defer configMu.RUnlock()
		config, err := readConfig(configPath)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot read configuration"})
			return
		}
		writeJSON(w, http.StatusOK, config)
	})
	mux.HandleFunc("PUT /api/config", func(w http.ResponseWriter, r *http.Request) {
		reader := http.MaxBytesReader(w, r.Body, 4<<20)
		defer reader.Close()
		config, err := decodeConfig(reader)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		configMu.Lock()
		defer configMu.Unlock()
		if err := writeConfig(configPath, config); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot save configuration"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
	})
	mux.HandleFunc("GET /download", func(w http.ResponseWriter, r *http.Request) {

		if _, err := requestSources(r.URL.Query()); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "subscription generation is not implemented yet"})
	})

	mux.HandleFunc("GET /api/subscriptions", func(w http.ResponseWriter, r *http.Request) {
		sources, err := requestSources(r.URL.Query())
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		views := make([]SourceView, 0, len(sources))
		previews := make(map[string][]NodePreview)
		enabled := 0
		for _, source := range sources {
			if source.Enabled {
				enabled++
			}
			if r.URL.Query().Get("refresh") == "true" {
				sourceManager.startRefresh(source, 2)
			}
			views = append(views, sourceManager.view(source))
			if cache, err := sourceManager.readCache(source); err == nil && cache.LastSuccess != nil {
				previews[source.ID] = previewNodes(cache.Proxies)
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"sources": views, "previews": previews,
			"counts": map[string]int{"sources": len(sources), "enabled": enabled},
		})
	})
	return mux
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}

func requestSources(query url.Values) ([]Source, error) {
	sources := make([]Source, 0)
	seen := make(map[string]bool)
	for _, value := range query["url"] {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		parsed, err := url.Parse(value)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return nil, fmt.Errorf("expected HTTP / HTTPS subscription URLs")
		}
		seen[value] = true
		sources = append(sources, Source{
			ID: sourceKey(Source{URL: value}), URL: value, Name: fmt.Sprintf("订阅 %d", len(sources)+1),
			Enabled: query.Get("aggregate") != "false" || len(sources) == 0,
		})
	}
	if len(sources) == 0 {
		return nil, fmt.Errorf("at least one url parameter is required")
	}
	for _, key := range []string{"aggregate", "refresh"} {
		if value := query.Get(key); value != "" && value != "true" && value != "false" {
			return nil, fmt.Errorf("%s must be true or false", key)
		}
	}
	return sources, nil
}
