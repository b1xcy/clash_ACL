package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

func newHandler(configPath string) http.Handler {
	mux := http.NewServeMux()
	var configMu sync.RWMutex
	sourceManager := newSourceManager(filepath.Dir(configPath))
	snapshotStore := newSnapshotStore(filepath.Dir(configPath), sourceManager)
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
			"capabilities": map[string]bool{"conversion": true, "uri_import": true, "aggregation": true, "rule_import": true, "node_policy": true}, "sources": sources,
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
			"options": ConversionOptions{RenameRules: config.RenameRules, RatePattern: config.RatePattern, AI: config.AI, GroupsJSON: string(groups)}, "self_hosted_nodes": nodes,
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
		config.RatePattern = options.RatePattern
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
	mux.HandleFunc("GET /api/self-hosted", func(w http.ResponseWriter, r *http.Request) {
		configMu.RLock()
		defer configMu.RUnlock()
		config, err := readConfig(configPath)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot read configuration"})
			return
		}
		nodes := config.SelfHostedNodes
		if nodes == nil {
			nodes = []SelfHostedNode{}
		}
		data, err := json.MarshalIndent(nodes, "", "  ")
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot read self-hosted nodes"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"nodes_json": string(data)})
	})
	mux.HandleFunc("PUT /api/self-hosted", func(w http.ResponseWriter, r *http.Request) {
		reader := http.MaxBytesReader(w, r.Body, maxSubscriptionSize)
		defer reader.Close()
		var nodes []SelfHostedNode
		decoder := json.NewDecoder(reader)
		decoder.UseNumber()
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&nodes); err != nil || nodes == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "expected a self-hosted node array"})
			return
		}
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "expected one self-hosted node array"})
			return
		}
		for _, node := range nodes {
			name, _ := node.Proxy["name"].(string)
			kind, _ := node.Proxy["type"].(string)
			if name == "" || kind == "" {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "self-hosted proxies require a name and type"})
				return
			}
		}
		configMu.Lock()
		defer configMu.Unlock()
		config, err := readConfig(configPath)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot read configuration"})
			return
		}
		config.SelfHostedNodes = nodes
		if err := validateConfig(config); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if err := writeConfig(configPath, config); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot save self-hosted nodes"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
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
	mux.HandleFunc("POST /api/uri/preview", func(w http.ResponseWriter, r *http.Request) {
		reader := http.MaxBytesReader(w, r.Body, maxSubscriptionSize)
		defer reader.Close()
		data, err := io.ReadAll(reader)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cannot read node links; maximum size is 8 MiB"})
			return
		}
		lines := parseURILines(string(data))
		if len(lines) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "enter at least one node link"})
			return
		}
		valid := 0
		for _, line := range lines {
			if line.Error == "" {
				valid++
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"lines": lines, "valid_count": valid, "error_count": len(lines) - valid})
	})
	mux.HandleFunc("POST /api/aggregate/preview", func(w http.ResponseWriter, r *http.Request) {
		reader := http.MaxBytesReader(w, r.Body, maxSubscriptionSize)
		defer reader.Close()
		var request struct {
			URLs        []string          `json:"urls"`
			Aggregate   *bool             `json:"aggregate"`
			DirectLinks string            `json:"direct_links"`
			Policy      NodePolicyOptions `json:"policy"`
		}
		decoder := json.NewDecoder(reader)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "expected an aggregation request object"})
			return
		}
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "expected one aggregation request object"})
			return
		}
		inputs := make([]AggregatedNode, 0)
		views := make([]SourceView, 0)
		if len(request.URLs) != 0 {
			query := url.Values{"url": request.URLs}
			if request.Aggregate != nil {
				query.Set("aggregate", strconv.FormatBool(*request.Aggregate))
			}
			sources, err := requestSources(query)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			for _, source := range sources {
				view := sourceManager.view(source)
				views = append(views, view)
				if !source.Enabled {
					continue
				}
				cache, err := sourceManager.readCache(source)
				if err != nil || cache.LastSuccess == nil || view.State == "refreshing" {
					writeJSON(w, http.StatusConflict, map[string]string{"error": "read enabled subscriptions before aggregation"})
					return
				}
				for _, proxy := range cache.Proxies {
					name, _ := proxy["name"].(string)
					inputs = append(inputs, AggregatedNode{Proxy: proxy, Origins: []NodeOrigin{{SourceID: source.ID, SourceName: source.Name, OriginalName: name}}})
				}
			}
		}
		for _, line := range parseURILines(request.DirectLinks) {
			if line.Error != "" {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "direct links contain invalid lines; confirm valid nodes first"})
				return
			}
			inputs = append(inputs, AggregatedNode{Proxy: line.Proxy, Origins: []NodeOrigin{{SourceID: "direct-links", SourceName: "直接节点", OriginalName: line.Name, Line: line.Line}}})
		}
		if len(inputs) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "aggregation requires at least one node"})
			return
		}
		nodes, duplicates, err := aggregateNodes(inputs)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		nodes, err = applyNodePolicy(nodes, request.Policy)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		counts := map[string]int{"low": 0, "standard": 0, "high": 0, "unknown": 0, "conflicts": 0}
		for _, node := range nodes {
			counts[node.RateGroup]++
			if node.Rate == nil {
				counts["unknown"]++
			}
			if node.RateConflict {
				counts["conflicts"]++
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"nodes": nodes, "input_count": len(inputs), "node_count": len(nodes), "duplicate_count": duplicates, "sources": views, "rate_counts": counts})
	})
	mux.HandleFunc("GET /api/rules/presets", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"presets": rulePresets})
	})
	mux.HandleFunc("POST /api/generate/preview", func(w http.ResponseWriter, r *http.Request) {
		reader := http.MaxBytesReader(w, r.Body, maxSubscriptionSize)
		defer reader.Close()
		request, err := decodeGenerationRequest(reader)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		configMu.RLock()
		config, err := readConfig(configPath)
		configMu.RUnlock()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot read configuration"})
			return
		}
		_, preview, err := generateConfig(r.Context(), sourceManager, config, request, false)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, preview)
	})
	mux.HandleFunc("POST /api/rules/preview", func(w http.ResponseWriter, r *http.Request) {
		reader := http.MaxBytesReader(w, r.Body, maxSubscriptionSize)
		defer reader.Close()
		var request *RuleRequest
		decoder := json.NewDecoder(reader)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil || request == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "expected a rules request object"})
			return
		}
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "expected one rules request object"})
			return
		}
		result, err := compileRules(*request)
		if err != nil {
			if unmapped, ok := err.(unmappedPolicies); ok {
				writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": err.Error(), "unmapped_targets": unmapped.targets})
			} else {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			}
			return
		}
		writeJSON(w, http.StatusOK, result)
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
		refresh, err := requestRefresh(r.URL.Query())
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		id := r.URL.Query().Get("id")
		var plan GenerationPlan
		if id != "" {
			if !validSnapshotID(id) || len(r.URL.Query()["url"]) > 0 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "use a valid snapshot ID without url parameters"})
				return
			}
			snapshot, err := snapshotStore.read(id)
			if errors.Is(err, os.ErrNotExist) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "subscription snapshot not found"})
				return
			}
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot read subscription snapshot"})
				return
			}
			plan = snapshot.Plan
		} else {
			if _, err := requestSources(r.URL.Query()); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			configMu.RLock()
			config, err := readConfig(configPath)
			configMu.RUnlock()
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot read configuration"})
				return
			}
			aggregate := r.URL.Query().Get("aggregate") != "false"
			request := GenerationRequest{URLs: r.URL.Query()["url"], Aggregate: &aggregate, Rules: RuleRequest{Preset: r.URL.Query().Get("preset"), SkipUnsupported: true}}
			plan, id, err = snapshotPlan(config, request)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
		}
		snapshot, err := snapshotStore.get(r.Context(), id, plan, refresh)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="clash-acl.yaml"`)
		w.Header().Set("X-Clash-ACL-Snapshot", id)
		w.Header().Set("X-Clash-ACL-Generated-At", snapshot.GeneratedAt.Format(time.RFC3339))
		w.Header().Set("X-Clash-ACL-Using-Cache", strconv.FormatBool(snapshotUsingCache(snapshot)))
		w.Header().Set("X-Clash-ACL-Refresh-Succeeded", strconv.FormatBool(refresh && !snapshotUsingCache(snapshot)))
		w.Write([]byte(snapshot.YAML))
	})
	mux.HandleFunc("POST /api/generate", func(w http.ResponseWriter, r *http.Request) {
		refresh, err := requestRefresh(r.URL.Query())
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		reader := http.MaxBytesReader(w, r.Body, maxSubscriptionSize)
		defer reader.Close()
		request, err := decodeGenerationRequest(reader)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		configMu.RLock()
		config, err := readConfig(configPath)
		configMu.RUnlock()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot read configuration"})
			return
		}
		plan, id, err := snapshotPlan(config, request)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		snapshot, err := snapshotStore.get(r.Context(), id, plan, refresh)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": id, "download_path": "/download?id=" + id, "generated_at": snapshot.GeneratedAt, "using_cache": snapshotUsingCache(snapshot), "refresh_succeeded": refresh && !snapshotUsingCache(snapshot), "preview": snapshot.Preview})
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

func requestRefresh(query url.Values) (bool, error) {
	value := query.Get("refresh")
	if value != "" && value != "true" && value != "false" {
		return false, errors.New("refresh must be true or false")
	}
	return value == "true", nil
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
