package main

import (
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
)

func newHandler(configPath, adminToken, downloadToken string) http.Handler {
	mux := http.NewServeMux()
	var configMu sync.RWMutex
	sourceManager := newSourceManager(filepath.Dir(configPath))
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(indexHTML)
	})
	mux.Handle("GET /api/status", authorize(adminToken, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			"features": featureProgress(), "sources": sources,
			"counts": map[string]int{"sources": len(config.Sources), "enabled": enabled, "self_hosted": len(config.SelfHostedNodes), "groups": len(config.Groups)},
		})
	})))
	mux.Handle("GET /api/sources", authorize(adminToken, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		configMu.RLock()
		defer configMu.RUnlock()
		config, err := readConfig(configPath)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot read configuration"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"sources": config.Sources})
	})))
	mux.Handle("PUT /api/sources", authorize(adminToken, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	})))
	mux.Handle("POST /api/sources/refresh", authorize(adminToken, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	})))
	mux.Handle("GET /api/sources/{id}/preview", authorize(adminToken, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	})))
	mux.Handle("POST /api/yaml/preview", authorize(adminToken, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	})))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte("ok\n"))
	})
	mux.Handle("GET /api/config", authorize(adminToken, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		configMu.RLock()
		defer configMu.RUnlock()
		config, err := readConfig(configPath)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot read configuration"})
			return
		}
		writeJSON(w, http.StatusOK, config)
	})))
	mux.Handle("PUT /api/config", authorize(adminToken, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	})))
	mux.Handle("GET /download", authorize(downloadToken, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "subscription generation is not implemented yet"})
	})))
	return mux
}

func authorize(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		header := r.Header.Get("Authorization")
		value, ok := strings.CutPrefix(header, "Bearer ")
		if !ok || token == "" || subtle.ConstantTimeCompare([]byte(value), []byte(token)) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}
