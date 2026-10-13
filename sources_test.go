package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

const testSubscription = `proxies:
  - name: "Example [倍率:0.5]"
    type: vless
    server: example.invalid
    port: 443
    uuid: synthetic-test-secret
    reality-opts:
      short-id: "01"
      public-key: synthetic-public-key
    extension:
      large: 9007199254740993
rules:
  - MATCH,DIRECT
`

func TestSubscriptionParsingPreservesFields(t *testing.T) {
	proxies, err := parseSubscription([]byte(testSubscription))
	if err != nil || len(proxies) != 1 {
		t.Fatalf("parse failed: %v", err)
	}
	if proxies[0]["reality-opts"].(map[string]any)["short-id"] != "01" {
		t.Fatal("protocol field changed during parsing")
	}
	for _, input := range []string{"invalid: [", "proxies: []", "proxies: [{name: missing-type}]", testSubscription + "\n---\nproxies: []"} {
		if _, err := parseSubscription([]byte(input)); err == nil {
			t.Fatal("invalid or multiple YAML documents accepted")
		}
	}
}

func TestFailedRefreshPreservesCacheAndRestartState(t *testing.T) {
	var failing atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failing.Load() {
			http.Error(w, "private-upstream-secret", http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte(testSubscription))
	}))
	defer upstream.Close()
	dir := t.TempDir()
	manager := newSourceManager(dir)
	source := Source{ID: "example", Name: "Example", URL: upstream.URL, Enabled: true}
	flight := manager.startRefresh(source, 2)
	<-flight.done
	if flight.result.State != "ready" || flight.result.NodeCount != 1 {
		t.Fatalf("initial refresh failed: %+v", flight.result)
	}
	cache, err := manager.readCache(source)
	if err != nil || cache.Proxies[0]["extension"].(map[string]any)["large"] != json.Number("9007199254740993") {
		t.Fatal("cache lost unknown fields or exact numbers")
	}
	failing.Store(true)
	flight = manager.startRefresh(source, 2)
	<-flight.done
	if !flight.result.UsingCache || flight.result.State != "cached" || flight.result.NodeCount != 1 || strings.Contains(flight.result.Error, "private-upstream-secret") || strings.Contains(flight.result.Error, upstream.URL) {
		t.Fatal("failed refresh lost old nodes or exposed upstream details")
	}
	restarted := newSourceManager(dir)
	if restarted.view(source).State != "cached" || restarted.view(source).NodeCount != 1 {
		t.Fatal("cached state was not retained across restart")
	}
	source.URL += "/different"
	if restarted.view(source).NodeCount != 0 {
		t.Fatal("changed URL reused another subscription's cache")
	}
}

func TestRefreshCoalescesOverlappingRequests(t *testing.T) {
	var requests atomic.Int32
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		entered <- struct{}{}
		<-release
		w.Write([]byte(testSubscription))
	}))
	defer upstream.Close()
	manager := newSourceManager(t.TempDir())
	source := Source{ID: "example", URL: upstream.URL, Enabled: true}
	first := manager.startRefresh(source, 2)
	<-entered
	for i := 0; i < 10; i++ {
		if manager.startRefresh(source, 2) != first {
			close(release)
			t.Fatal("overlapping refresh did not share the same task")
		}
	}
	close(release)
	<-first.done
	if requests.Load() != 1 {
		t.Fatal("upstream was fetched repeatedly")
	}
}

func TestRefreshConcurrencyAndDisabledSources(t *testing.T) {
	var active, maximum, requests atomic.Int32
	entered := make(chan struct{}, 4)
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		current := active.Add(1)
		for {
			old := maximum.Load()
			if current <= old || maximum.CompareAndSwap(old, current) {
				break
			}
		}
		entered <- struct{}{}
		<-release
		w.Write([]byte(testSubscription))
		active.Add(-1)
	}))
	defer upstream.Close()
	manager := newSourceManager(t.TempDir())
	flights := []*sourceFlight{}
	for _, id := range []string{"a", "b", "c", "d"} {
		flights = append(flights, manager.startRefresh(Source{ID: id, URL: upstream.URL, Enabled: true}, 2))
	}
	if manager.startRefresh(Source{ID: "disabled", URL: upstream.URL, Enabled: false}, 2) != nil {
		close(release)
		t.Fatal("disabled source started a fetch")
	}
	<-entered
	<-entered
	close(release)
	for _, flight := range flights {
		<-flight.done
	}
	if maximum.Load() > 2 || requests.Load() != 4 {
		t.Fatal("refresh exceeded concurrency limit or fetched a disabled source")
	}
}

func TestYAMLPreviewRequiresAdminAndDoesNotExposeParameters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := writeConfig(path, defaultConfig()); err != nil {
		t.Fatal(err)
	}
	handler := newHandler(path, "admin-test", "download-test")
	for _, token := range []string{"download-test", "admin-test"} {
		request := httptest.NewRequest(http.MethodPost, "/api/yaml/preview", strings.NewReader(testSubscription))
		request.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if token == "download-test" {
			if response.Code != http.StatusUnauthorized {
				t.Fatal("download credential can parse management input")
			}
			continue
		}
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Example") || strings.Contains(response.Body.String(), "synthetic-test-secret") || strings.Contains(response.Body.String(), "example.invalid") || strings.Contains(response.Body.String(), "reality-opts") {
			t.Fatal("preview missing nodes or exposing private protocol fields")
		}
	}
}

func TestSourceOnlyUpdatePreservesOtherConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	config := defaultConfig()
	config.Base["extension"] = json.Number("9007199254740993")
	config.SelfHostedNodes = []SelfHostedNode{{ID: "fixed", Proxy: map[string]any{"password": "private-config-secret", "extension": json.Number("9007199254740993")}}}
	if err := writeConfig(path, config); err != nil {
		t.Fatal(err)
	}
	handler := newHandler(path, "admin-test", "download-test")
	request := httptest.NewRequest(http.MethodPut, "/api/sources", strings.NewReader(`[{"id":"new","name":"New source","url":"https://example.invalid/sub","enabled":true}]`))
	request.Header.Set("Authorization", "Bearer admin-test")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatal("source update failed")
	}
	saved, err := readConfig(path)
	if err != nil || len(saved.Sources) != 1 || saved.Base["extension"] != json.Number("9007199254740993") || saved.SelfHostedNodes[0].Proxy["extension"] != json.Number("9007199254740993") {
		t.Fatal("source update changed unrelated configuration or numeric precision")
	}
	request = httptest.NewRequest(http.MethodGet, "/api/sources", nil)
	request.Header.Set("Authorization", "Bearer admin-test")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "private-config-secret") {
		t.Fatal("source editor should not receive self-hosted credentials")
	}
}
