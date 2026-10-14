package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"go.yaml.in/yaml/v3"
)

const maxSubscriptionSize = 8 << 20

type SourceCache struct {
	LastAttempt *time.Time       `json:"last_attempt"`
	LastSuccess *time.Time       `json:"last_success"`
	Error       string           `json:"error"`
	Proxies     []map[string]any `json:"proxies"`
}

type NodePreview struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type SourceView struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Enabled     bool       `json:"enabled"`
	State       string     `json:"state"`
	NodeCount   int        `json:"node_count"`
	LastAttempt *time.Time `json:"last_attempt"`
	LastSuccess *time.Time `json:"last_success"`
	Error       string     `json:"error"`
	UsingCache  bool       `json:"using_cache"`
}

type sourceFlight struct {
	done   chan struct{}
	result SourceView
}

type SourceManager struct {
	dir     string
	client  *http.Client
	mu      sync.Mutex
	flights map[string]*sourceFlight
	active  int
	changed chan struct{}
}

func newSourceManager(dataDir string) *SourceManager {
	return &SourceManager{
		dir: filepath.Join(dataDir, "cache"), client: &http.Client{Timeout: 20 * time.Second},
		flights: make(map[string]*sourceFlight), changed: make(chan struct{}),
	}
}

func sourceKey(source Source) string {
	sum := sha256.Sum256([]byte(source.ID + "\x00" + source.URL))
	return hex.EncodeToString(sum[:])
}

func parseSubscription(data []byte) ([]map[string]any, error) {
	var document struct {
		Proxies []map[string]any `yaml:"proxies"`
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&document); err != nil {
		return nil, errors.New("invalid subscription YAML")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, errors.New("expected one YAML document")
	}
	if len(document.Proxies) == 0 {
		return nil, errors.New("subscription contains no proxies")
	}
	for index, proxy := range document.Proxies {
		name, nameOK := proxy["name"].(string)
		kind, typeOK := proxy["type"].(string)
		if !nameOK || !typeOK || name == "" || kind == "" {
			return nil, fmt.Errorf("node %d requires a name and type", index+1)
		}
	}
	if _, err := json.Marshal(document.Proxies); err != nil {
		return nil, errors.New("node fields must use JSON-compatible string keys and values")
	}
	return document.Proxies, nil
}

func previewNodes(proxies []map[string]any) []NodePreview {
	preview := make([]NodePreview, 0, len(proxies))
	for _, proxy := range proxies {
		name, _ := proxy["name"].(string)
		kind, _ := proxy["type"].(string)
		id, _ := nodeIdentity(proxy)
		preview = append(preview, NodePreview{ID: id, Name: name, Type: kind})
	}
	return preview
}

func (manager *SourceManager) readCache(source Source) (SourceCache, error) {
	file, err := os.Open(filepath.Join(manager.dir, sourceKey(source)+".json"))
	if err != nil {
		return SourceCache{}, err
	}
	defer file.Close()
	var cache SourceCache
	decoder := json.NewDecoder(file)
	decoder.UseNumber()
	err = decoder.Decode(&cache)
	return cache, err
}

func (manager *SourceManager) writeCache(source Source, cache SourceCache) error {
	if err := os.MkdirAll(manager.dir, 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(manager.dir, ".source-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err := json.NewEncoder(file).Encode(cache); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), filepath.Join(manager.dir, sourceKey(source)+".json"))
}

func sourceView(source Source, cache SourceCache) SourceView {
	view := SourceView{
		ID: source.ID, Name: source.Name, Enabled: source.Enabled, NodeCount: len(cache.Proxies),
		LastAttempt: cache.LastAttempt, LastSuccess: cache.LastSuccess, Error: cache.Error, State: "pending",
	}
	if cache.LastSuccess != nil {
		view.State = "ready"
	}
	if cache.Error != "" {
		view.State = "error"
		if cache.LastSuccess != nil {
			view.State = "cached"
			view.UsingCache = true
		}
	}
	if !source.Enabled {
		view.State = "disabled"
	}
	return view
}

func (manager *SourceManager) view(source Source) SourceView {
	cache, err := manager.readCache(source)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		cache = SourceCache{Error: "cannot read subscription cache"}
	}
	view := sourceView(source, cache)
	manager.mu.Lock()
	if manager.flights[sourceKey(source)] != nil && source.Enabled {
		view.State = "refreshing"
	}
	manager.mu.Unlock()
	return view
}

func (manager *SourceManager) acquire(limit int) {
	for {
		manager.mu.Lock()
		if manager.active < limit {
			manager.active++
			manager.mu.Unlock()
			return
		}
		changed := manager.changed
		manager.mu.Unlock()
		<-changed
	}
}

func (manager *SourceManager) startRefresh(source Source, limit int) *sourceFlight {
	if !source.Enabled {
		return nil
	}
	key := sourceKey(source)
	manager.mu.Lock()
	flight := manager.flights[key]
	if flight == nil {
		flight = &sourceFlight{done: make(chan struct{})}
		manager.flights[key] = flight
		go func() {
			flight.result = manager.fetch(source, limit)
			manager.mu.Lock()
			delete(manager.flights, key)
			close(flight.done)
			manager.mu.Unlock()
		}()
	}
	manager.mu.Unlock()
	return flight
}

func (manager *SourceManager) fetch(source Source, limit int) SourceView {
	manager.acquire(limit)
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	defer func() {
		manager.mu.Lock()
		manager.active--
		close(manager.changed)
		manager.changed = make(chan struct{})
		manager.mu.Unlock()
	}()
	previous, _ := manager.readCache(source)
	cache := previous
	now := time.Now().UTC()
	cache.LastAttempt = &now
	proxies, err := manager.fetchYAML(ctx, source.URL)
	if err != nil {
		cache.Error = err.Error()
	} else {
		cache.Proxies = proxies
		cache.LastSuccess = &now
		cache.Error = ""
	}
	if err := manager.writeCache(source, cache); err != nil {
		cache = previous
		cache.LastAttempt = &now
		cache.Error = "cannot save subscription cache"
	}
	return sourceView(source, cache)
}

func (manager *SourceManager) fetchYAML(ctx context.Context, url string) ([]map[string]any, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, errors.New("invalid subscription request")
	}
	request.Header.Set("User-Agent", "clash_ACL")
	response, err := manager.client.Do(request)
	if err != nil {
		return nil, errors.New("subscription request failed")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("subscription returned HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxSubscriptionSize+1))
	if err != nil {
		return nil, errors.New("cannot read subscription response")
	}
	if len(data) > maxSubscriptionSize {
		return nil, errors.New("subscription exceeds the 8 MiB limit")
	}
	return parseSubscription(data)
}
