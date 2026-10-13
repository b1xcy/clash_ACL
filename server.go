package main

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
)

func newHandler(configPath, adminToken, downloadToken string) http.Handler {
	mux := http.NewServeMux()
	var configMu sync.RWMutex
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
