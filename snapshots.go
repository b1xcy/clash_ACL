package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type GenerationPlan struct {
	Request GenerationRequest `json:"request"`
	Config  Config            `json:"config"`
}

type ConfigSnapshot struct {
	Plan        GenerationPlan    `json:"plan"`
	YAML        string            `json:"yaml"`
	Preview     GenerationPreview `json:"preview"`
	GeneratedAt time.Time         `json:"generated_at"`
}

type snapshotFlight struct {
	done     chan struct{}
	snapshot ConfigSnapshot
	err      error
}

type SnapshotStore struct {
	dir     string
	manager *SourceManager
	mu      sync.Mutex
	flights map[string]*snapshotFlight
}

func newSnapshotStore(dataDir string, manager *SourceManager) *SnapshotStore {
	return &SnapshotStore{dir: filepath.Join(dataDir, "snapshots"), manager: manager, flights: map[string]*snapshotFlight{}}
}

func snapshotPlan(config Config, request GenerationRequest) (GenerationPlan, string, error) {
	sources, err := generationSources(request)
	if err != nil {
		return GenerationPlan{}, "", err
	}
	request.URLs = []string{}
	for _, source := range sources {
		if source.Enabled {
			request.URLs = append(request.URLs, source.URL)
		}
	}
	aggregate := true
	request.Aggregate = &aggregate
	if request.Rules.Preset == "" {
		request.Rules.Preset = "acl4ssr-online"
	}
	request.Rules.Targets = nil
	if request.Policy == nil {
		request.Policy = &NodePolicyOptions{RatePattern: config.RatePattern, RenameRules: config.RenameRules}
	}
	if request.AI == nil {
		ai := config.AI
		request.AI = &ai
	}
	if request.Groups == nil {
		groups := config.Groups
		request.Groups = &groups
	}
	selectedSources := []Source{}
	for _, saved := range config.Sources {
		if containsString(request.URLs, saved.URL) {
			selectedSources = append(selectedSources, saved)
		}
	}
	config.Sources = selectedSources
	config.Groups, config.AI, config.RenameRules, config.RatePattern = nil, AIPolicy{}, nil, ""
	plan := GenerationPlan{Request: request, Config: config}
	data, err := json.Marshal(plan)
	if err != nil {
		return GenerationPlan{}, "", errors.New("cannot encode conversion parameters")
	}
	sum := sha256.Sum256(data)
	return plan, hex.EncodeToString(sum[:]), nil
}

func validSnapshotID(id string) bool {
	if len(id) != 64 || id != strings.ToLower(id) {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}

func (store *SnapshotStore) read(id string) (ConfigSnapshot, error) {
	if !validSnapshotID(id) {
		return ConfigSnapshot{}, errors.New("invalid subscription snapshot ID")
	}
	file, err := os.Open(filepath.Join(store.dir, id+".json"))
	if err != nil {
		return ConfigSnapshot{}, err
	}
	defer file.Close()
	var snapshot ConfigSnapshot
	decoder := json.NewDecoder(file)
	decoder.UseNumber()
	if err := decoder.Decode(&snapshot); err != nil || snapshot.YAML == "" || snapshot.GeneratedAt.IsZero() {
		return ConfigSnapshot{}, errors.New("cannot read subscription snapshot")
	}
	return snapshot, nil
}

func (store *SnapshotStore) write(id string, snapshot ConfigSnapshot) error {
	if err := os.MkdirAll(store.dir, 0700); err != nil {
		return errors.New("cannot create snapshot directory")
	}
	file, err := os.CreateTemp(store.dir, ".snapshot-*.json")
	if err != nil {
		return errors.New("cannot create subscription snapshot")
	}
	defer os.Remove(file.Name())
	if err := json.NewEncoder(file).Encode(snapshot); err != nil {
		file.Close()
		return errors.New("cannot encode subscription snapshot")
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return errors.New("cannot sync subscription snapshot")
	}
	if err := file.Close(); err != nil {
		return errors.New("cannot close subscription snapshot")
	}
	if err := os.Rename(file.Name(), filepath.Join(store.dir, id+".json")); err != nil {
		return errors.New("cannot save subscription snapshot")
	}
	return nil
}

func (store *SnapshotStore) get(ctx context.Context, id string, plan GenerationPlan, refresh bool) (ConfigSnapshot, error) {
	if !refresh {
		if snapshot, err := store.read(id); err == nil {
			return snapshot, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return ConfigSnapshot{}, errors.New("cannot read subscription snapshot")
		}
	}
	store.mu.Lock()
	flight := store.flights[id]
	if flight != nil {
		store.mu.Unlock()
		select {
		case <-ctx.Done():
			return ConfigSnapshot{}, errors.New("subscription generation request cancelled")
		case <-flight.done:
			return flight.snapshot, flight.err
		}
	}
	flight = &snapshotFlight{done: make(chan struct{})}
	store.flights[id] = flight
	store.mu.Unlock()
	defer func() {
		store.mu.Lock()
		delete(store.flights, id)
		close(flight.done)
		store.mu.Unlock()
	}()
	output, preview, err := generateConfig(ctx, store.manager, plan.Config, plan.Request, refresh)
	if err != nil {
		flight.err = err
		return ConfigSnapshot{}, err
	}
	snapshot := ConfigSnapshot{Plan: plan, YAML: string(output), Preview: preview, GeneratedAt: time.Now().UTC()}
	if err := store.write(id, snapshot); err != nil {
		flight.err = err
		return ConfigSnapshot{}, err
	}
	flight.snapshot = snapshot
	return snapshot, nil
}

func snapshotUsingCache(snapshot ConfigSnapshot) bool {
	for _, source := range snapshot.Preview.Sources {
		if source.Enabled && source.UsingCache {
			return true
		}
	}
	return false
}
