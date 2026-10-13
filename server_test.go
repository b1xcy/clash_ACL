package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestCredentialSeparation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := writeConfig(path, defaultConfig()); err != nil {
		t.Fatal(err)
	}
	handler := newHandler(path, "admin-test", "download-test")
	for _, test := range []struct {
		path   string
		token  string
		status int
	}{
		{"/healthz", "", http.StatusOK},
		{"/api/config", "", http.StatusUnauthorized},
		{"/api/config", "wrong", http.StatusUnauthorized},
		{"/api/config", "download-test", http.StatusUnauthorized},
		{"/api/config", "admin-test", http.StatusOK},
		{"/download", "admin-test", http.StatusUnauthorized},
		{"/download", "download-test", http.StatusNotImplemented},
	} {
		request := httptest.NewRequest(http.MethodGet, test.path, nil)
		if test.token != "" {
			request.Header.Set("Authorization", "Bearer "+test.token)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != test.status {
			t.Errorf("%s with %q: got %d, want %d", test.path, test.token, response.Code, test.status)
		}
		if test.path != "/healthz" && response.Header().Get("Cache-Control") != "no-store" {
			t.Error("configuration and authorization responses must not be cached")
		}
	}
}

func TestConfigUpdatePreservesProtocolFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := writeConfig(path, defaultConfig()); err != nil {
		t.Fatal(err)
	}
	body := `{"sources":[{"id":"example","name":"Example","url":"https://example.invalid/sub","enabled":true}],"self_hosted_nodes":[{"id":"fixed","proxy":{"name":"Fixed","type":"vless","server":"example.invalid","uuid":"synthetic-test-id","reality-opts":{"public-key":"synthetic-public-key","short-id":"01"},"extension":{"large_number":9007199254740993}}}],"ai":{"node_id":"fixed","domains":["example.invalid"]}}`
	handler := newHandler(path, "admin-test", "download-test")
	request := httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer admin-test")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("save: %d %s", response.Code, response.Body.String())
	}
	config, err := readConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	proxy := config.SelfHostedNodes[0].Proxy
	if proxy["extension"].(map[string]any)["large_number"] != json.Number("9007199254740993") {
		t.Error("unknown protocol fields or exact numbers lost on disk round-trip")
	}
	if proxy["reality-opts"].(map[string]any)["short-id"] != "01" {
		t.Error("nested protocol parameters changed")
	}
	if config.FetchConcurrency != 2 || config.AI.NodeID != "fixed" {
		t.Error("defaults or AI reference not persisted")
	}
	restarted := newHandler(path, "admin-test", "download-test")
	request = httptest.NewRequest(http.MethodGet, "/api/config", nil)
	request.Header.Set("Authorization", "Bearer admin-test")
	response = httptest.NewRecorder()
	restarted.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "9007199254740993") {
		t.Error("saved configuration not readable by a new handler")
	}
}

func TestRejectedUpdatesLeaveConfigurationUnchanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := writeConfig(path, defaultConfig()); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	handler := newHandler(path, "admin-test", "download-test")
	for _, body := range []string{
		`null`, `[]`, `{"fetch_concurrency":0}`, `{} {}`, `{"typo":true}`,
		`{"sources":[{"id":"a","url":"file:///private-secret"}]}`,
		`{"sources":[{"id":"a","url":"https://example.invalid"},{"id":"a","url":"https://example.invalid"}]}`,
		`{"ai":{"node_id":"missing"}}`,
	} {
		request := httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(body))
		request.Header.Set("Authorization", "Bearer admin-test")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d for %s", response.Code, body)
		}
		if strings.Contains(response.Body.String(), "private-secret") {
			t.Error("error response leaked source details")
		}
		after, err := os.ReadFile(path)
		if err != nil || string(before) != string(after) {
			t.Fatal("rejected update changed stored configuration")
		}
	}
}

func TestConcurrentReadsAndWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := writeConfig(path, defaultConfig()); err != nil {
		t.Fatal(err)
	}
	handler := newHandler(path, "admin-test", "download-test")
	var workers sync.WaitGroup
	for i := 0; i < 20; i++ {
		workers.Add(1)
		go func(i int) {
			defer workers.Done()
			method := http.MethodGet
			if i%2 == 0 {
				method = http.MethodPut
			}
			request := httptest.NewRequest(method, "/api/config", strings.NewReader(`{}`))
			request.Header.Set("Authorization", "Bearer admin-test")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Errorf("concurrent %s: %d", method, response.Code)
			}
		}(i)
	}
	workers.Wait()
	if _, err := readConfig(path); err != nil {
		t.Fatalf("configuration damaged by concurrent updates: %v", err)
	}
}
