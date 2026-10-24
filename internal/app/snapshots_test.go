package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestOldSnapshotRepairsVLESSSNIWithoutFetching(t *testing.T) {
	for _, savedProxies := range []bool{false, true} {
		t.Run(fmt.Sprint(savedProxies), func(t *testing.T) {
			dir := t.TempDir()
			store := newSnapshotStore(dir, newSourceManager(dir))
			proxy := map[string]any{"name": "Legacy", "type": "vless", "server": "192.0.2.1", "port": 443, "uuid": "11111111-1111-4111-8111-111111111111", "tls": true, "sni": "tls.example.invalid"}
			nodeID, _ := nodeIdentity(proxy)
			config := defaultConfig()
			request := GenerationRequest{URLs: []string{"https://unreachable.invalid/sub"}, AI: &AIPolicy{NodeID: nodeID}, Rules: RuleRequest{Preset: "none"}}
			plan, id, err := snapshotPlan(config, request)
			if err != nil {
				t.Fatal(err)
			}
			old := ConfigSnapshot{Plan: plan, YAML: "proxies: [{name: Legacy, type: vless, server: 192.0.2.1, port: 443, uuid: 11111111-1111-4111-8111-111111111111, tls: true, sni: tls.example.invalid}]", GeneratedAt: time.Now().UTC(), Preview: GenerationPreview{RulesVersion: bundledRuleVersion + "/dns-v1-domestic-v1", Nodes: []AggregatedNode{{ID: nodeID, Name: "Legacy", Type: "vless", RateGroup: "self"}}}}
			if savedProxies {
				old.Proxies = []map[string]any{proxy}
			}
			if err := store.write(id, old); err != nil {
				t.Fatal(err)
			}
			repaired, err := store.get(context.Background(), id, plan, false)
			if err != nil {
				t.Fatal(err)
			}
			proxies, err := parseSubscription([]byte(repaired.YAML))
			if err != nil || proxies[0]["servername"] != "tls.example.invalid" || proxies[0]["sni"] != nil || repaired.Preview.Nodes[0].ID != nodeID || repaired.Preview.Nodes[0].RateGroup != "self" {
				t.Fatal("snapshot repair lost SNI, identity or self-hosted policy", err)
			}
			validateImportedAIWithMihomo(t, []byte(repaired.YAML))
			again, err := store.get(context.Background(), id, plan, false)
			if err != nil || !again.GeneratedAt.Equal(repaired.GeneratedAt) || again.YAML != repaired.YAML {
				t.Fatal("repaired snapshot was not reused", err)
			}
		})
	}
}

func TestSnapshotDownloadsRefreshCacheAndRestart(t *testing.T) {
	var calls atomic.Int32
	var version atomic.Int32
	version.Store(1)
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if version.Load() < 0 {
			w.WriteHeader(502)
			return
		}
		fmt.Fprintf(w, "proxies:\n  - {name: Node-%d, type: ss, server: example.invalid, port: 443, cipher: aes-128-gcm, password: synthetic-secret}\n", version.Load())
	}))
	defer source.Close()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := writeConfig(path, defaultConfig()); err != nil {
		t.Fatal(err)
	}
	handler := newHandler(path)
	get := func(endpoint string) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, endpoint, nil))
		return response
	}
	endpoint := "/download?url=" + url.QueryEscape(source.URL) + "&preset=none"
	first := get(endpoint)
	if first.Code != 200 || calls.Load() != 1 || !strings.Contains(first.Body.String(), "Node-1") {
		t.Fatal("first download did not generate", first.Body.String())
	}
	id := first.Header().Get("X-Clash-ACL-Snapshot")
	version.Store(2)
	second := get(endpoint)
	if calls.Load() != 1 || second.Body.String() != first.Body.String() {
		t.Fatal("default download refreshed instead of serving the snapshot")
	}
	handler = newHandler(path)
	if restarted := get("/download?id=" + id); restarted.Code != 200 || restarted.Body.String() != first.Body.String() || calls.Load() != 1 {
		t.Fatal("snapshot did not persist across restart")
	}
	refreshed := get("/download?id=" + id + "&refresh=true")
	if refreshed.Code != 200 || calls.Load() != 2 || !strings.Contains(refreshed.Body.String(), "Node-2") || refreshed.Header().Get("X-Clash-ACL-Refresh-Succeeded") != "true" {
		t.Fatal("forced refresh did not wait for updated nodes", refreshed.Body.String())
	}
	version.Store(-1)
	failed := get("/download?id=" + id + "&refresh=true")
	if failed.Code != 200 || calls.Load() != 3 || failed.Header().Get("X-Clash-ACL-Using-Cache") != "true" || failed.Header().Get("X-Clash-ACL-Refresh-Succeeded") != "false" || failed.Body.String() != refreshed.Body.String() {
		t.Fatal("failed refresh concealed old cache or lost valid nodes")
	}
	config, _ := readConfig(path)
	if len(config.Sources) != 0 {
		t.Fatal("download changed global sources")
	}
	for _, bad := range []string{"/download?id=../config", "/download?id=" + id + "&url=" + source.URL, "/download?id=" + id + "&refresh=invalid", "/download?id=" + strings.Repeat("0", 64)} {
		if response := get(bad); response.Code != 400 && response.Code != 404 {
			t.Fatal("invalid snapshot download accepted")
		}
	}
}

func TestGeneratedAddressPrivacyAndPinnedOptions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	config := generationFixture()
	if err := writeConfig(path, config); err != nil {
		t.Fatal(err)
	}
	handler := newHandler(path)
	body := `{"rules":{"preset":"none"}}`
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/generate", strings.NewReader(body)))
	if response.Code != 200 || strings.Contains(response.Body.String(), "synthetic-low-secret") || strings.Contains(response.Body.String(), "low.invalid") {
		t.Fatal("generation response failed or exposed private node data", response.Body.String())
	}
	var result struct {
		ID           string `json:"id"`
		DownloadPath string `json:"download_path"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || !validSnapshotID(result.ID) {
		t.Fatal("invalid generated download address")
	}
	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodGet, result.DownloadPath, nil))
	if first.Code != 200 || !strings.Contains(first.Body.String(), "synthetic-low-secret") {
		t.Fatal("download lost proxy credentials")
	}
	config.RenameRules = []RenameRule{{Match: "香港", Replacement: "Changed"}}
	if err := writeConfig(path, config); err != nil {
		t.Fatal(err)
	}
	repeated := httptest.NewRecorder()
	handler.ServeHTTP(repeated, httptest.NewRequest(http.MethodGet, result.DownloadPath+"&refresh=true", nil))
	if repeated.Body.String() != first.Body.String() {
		t.Fatal("existing address unexpectedly changed its pinned options")
	}
	changed := httptest.NewRecorder()
	handler.ServeHTTP(changed, httptest.NewRequest(http.MethodPost, "/api/generate", strings.NewReader(body)))
	var changedResult struct {
		ID string `json:"id"`
	}
	json.Unmarshal(changed.Body.Bytes(), &changedResult)
	if changed.Code != 200 || changedResult.ID == result.ID {
		t.Fatal("changed parameters reused the old snapshot ID")
	}
}

func TestFailedGenerationPreservesSnapshotAndFilesArePrivate(t *testing.T) {
	dir := t.TempDir()
	store := newSnapshotStore(dir, newSourceManager(dir))
	plan, id, err := snapshotPlan(generationFixture(), GenerationRequest{Rules: RuleRequest{Preset: "none"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.get(context.Background(), id, plan, false); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "snapshots", id+".json")
	before, _ := os.ReadFile(file)
	plan.Request.AI = &AIPolicy{NodeID: "missing"}
	if _, err := store.get(context.Background(), id, plan, true); err == nil {
		t.Fatal("invalid AI generation succeeded")
	}
	after, _ := os.ReadFile(file)
	if string(before) != string(after) {
		t.Fatal("failed generation replaced a valid snapshot")
	}
	if stat, err := os.Stat(file); err != nil {
		t.Fatal(err)
	} else if runtime.GOOS != "windows" && stat.Mode().Perm()&0077 != 0 {
		t.Fatal("snapshot permissions expose proxy credentials")
	}
	files, _ := filepath.Glob(filepath.Join(dir, "snapshots", ".snapshot-*"))
	if len(files) != 0 {
		t.Fatal("temporary snapshot file left behind")
	}
}

func TestConcurrentRefreshesShareWorkAndRequestsStayIsolated(t *testing.T) {
	var calls atomic.Int32
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		select {
		case started <- struct{}{}:
		default:
		}
		<-release
		w.Write([]byte("proxies:\n  - {name: Airport, type: ss, server: example.invalid, port: 443, cipher: aes-128-gcm, password: synthetic-secret}\n"))
	}))
	defer source.Close()
	dir := t.TempDir()
	store := newSnapshotStore(dir, newSourceManager(dir))
	plan, id, _ := snapshotPlan(defaultConfig(), GenerationRequest{URLs: []string{source.URL}, Rules: RuleRequest{Preset: "none"}})
	var workers sync.WaitGroup
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			_, err := store.get(context.Background(), id, plan, true)
			results <- err
		}()
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("refresh did not start")
	}
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		store.mu.Lock()
		active := len(store.flights)
		store.mu.Unlock()
		if active == 1 {
			break
		}
	}
	time.Sleep(30 * time.Millisecond)
	close(release)
	workers.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 1 {
		t.Fatal("simultaneous refresh requests did not share source work")
	}
	otherPlan, otherID, _ := snapshotPlan(generationFixture(), GenerationRequest{Rules: RuleRequest{Preset: "none"}})
	other, err := store.get(context.Background(), otherID, otherPlan, false)
	if err != nil || otherID == id || strings.Contains(other.YAML, "Airport") {
		t.Fatal("different conversion requests shared snapshot data")
	}
}
