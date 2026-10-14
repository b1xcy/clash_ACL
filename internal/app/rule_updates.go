package app

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const bundledRuleVersion = "802e3abf065f8609e7d9073ba506158dbe2ba6e7"
const ruleUpdateInterval = 24 * time.Hour
const maxRuleArchiveSize = 64 << 20

type RuleUpdateStatus struct {
	Enabled     bool       `json:"enabled"`
	Interval    int        `json:"interval_hours"`
	State       string     `json:"state"`
	Origin      string     `json:"origin"`
	Version     string     `json:"version"`
	LastChecked *time.Time `json:"last_checked"`
	LastUpdated *time.Time `json:"last_updated"`
	NextCheck   *time.Time `json:"next_check"`
	Error       string     `json:"error"`
	ETag        string     `json:"-"`
}

type ruleUpdateState struct {
	Version     string     `json:"version"`
	ETag        string     `json:"etag"`
	LastChecked *time.Time `json:"last_checked"`
	LastUpdated *time.Time `json:"last_updated"`
	Error       string     `json:"error"`
}

type RuleUpdateManager struct {
	dir     string
	url     string
	client  *http.Client
	enabled bool
	mu      sync.RWMutex
	files   fs.FS
	status  RuleUpdateStatus
	flight  chan struct{}
}

func newRuleUpdateManager(dataDir string, enabled bool) *RuleUpdateManager {
	manager := &RuleUpdateManager{
		dir: filepath.Join(dataDir, "rule-updates"), url: "https://codeload.github.com/ACL4SSR/ACL4SSR/zip/refs/heads/master",
		client: &http.Client{Timeout: 90 * time.Second}, enabled: enabled, files: bundledRules,
		status: RuleUpdateStatus{Enabled: enabled, Interval: 24, Origin: "bundled", Version: bundledRuleVersion, State: "ready"},
	}
	data, err := os.ReadFile(filepath.Join(manager.dir, "state.json"))
	if errors.Is(err, os.ErrNotExist) {
		return manager
	}
	var saved ruleUpdateState
	if err != nil || json.Unmarshal(data, &saved) != nil {
		manager.status.Error = "cannot read saved rule update state; using bundled rules"
		return manager
	}
	manager.status.LastChecked, manager.status.LastUpdated, manager.status.Error = saved.LastChecked, saved.LastUpdated, saved.Error
	if saved.Version == bundledRuleVersion {
		return manager
	}
	if !validSnapshotID(saved.Version) {
		manager.status.Error = "invalid saved rule version; using bundled rules"
		return manager
	}
	archive, err := os.ReadFile(filepath.Join(manager.dir, saved.Version+".zip"))
	if err != nil {
		manager.status.Error = "cannot read saved rule archive; using bundled rules"
		return manager
	}
	sum := sha256.Sum256(archive)
	library, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil || hex.EncodeToString(sum[:]) != saved.Version || validateRuleLibrary(context.Background(), library) != nil {
		manager.status.Error = "saved rule archive failed validation; using bundled rules"
		return manager
	}
	manager.files = library
	manager.status.Origin, manager.status.Version, manager.status.ETag = "upstream", saved.Version, saved.ETag
	return manager
}

func (manager *RuleUpdateManager) current() (fs.FS, string) {
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	return manager.files, manager.status.Version
}

func (manager *RuleUpdateManager) view() RuleUpdateStatus {
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	status := manager.status
	if manager.flight != nil {
		status.State = "updating"
	} else if status.Error != "" {
		status.State = "error"
	} else {
		status.State = "ready"
	}
	if manager.enabled {
		next := time.Now().UTC()
		if status.LastChecked != nil {
			next = status.LastChecked.Add(ruleUpdateInterval)
		}
		status.NextCheck = &next
	}
	return status
}

func (manager *RuleUpdateManager) startUpdate(ctx context.Context) <-chan struct{} {
	manager.mu.Lock()
	if manager.flight != nil {
		flight := manager.flight
		manager.mu.Unlock()
		return flight
	}
	flight := make(chan struct{})
	manager.flight = flight
	manager.mu.Unlock()
	go func() {
		manager.check(ctx)
		manager.mu.Lock()
		manager.flight = nil
		close(flight)
		manager.mu.Unlock()
	}()
	return flight
}

func (manager *RuleUpdateManager) run(ctx context.Context) {
	if !manager.enabled {
		return
	}
	for {
		status := manager.view()
		delay := time.Duration(0)
		if status.LastChecked != nil {
			delay = time.Until(status.LastChecked.Add(ruleUpdateInterval))
		}
		if delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
		if ctx.Err() != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-manager.startUpdate(ctx):
		}
	}
}

func (manager *RuleUpdateManager) check(ctx context.Context) {
	status := manager.view()
	now := time.Now().UTC()
	status.LastChecked = &now
	library, archive, etag, err := manager.download(ctx, status.ETag)
	if err == nil && library != nil {
		err = validateRuleLibrary(ctx, library)
	}
	if err == nil && library != nil {
		sum := sha256.Sum256(archive)
		version := hex.EncodeToString(sum[:])
		if err = os.MkdirAll(manager.dir, 0700); err == nil {
			err = writeRuleFile(filepath.Join(manager.dir, version+".zip"), archive)
		}
		if err == nil {
			if version != status.Version {
				status.LastUpdated = &now
			}
			status.Version, status.Origin, status.ETag = version, "upstream", etag
		}
	}
	if err != nil {
		status.Error = err.Error()
	} else {
		status.Error = ""
	}
	state := ruleUpdateState{Version: status.Version, ETag: status.ETag, LastChecked: status.LastChecked, LastUpdated: status.LastUpdated, Error: status.Error}
	data, _ := json.Marshal(state)
	if saveErr := os.MkdirAll(manager.dir, 0700); saveErr != nil {
		err, status.Error = saveErr, "cannot save rule update state"
	} else if saveErr := writeRuleFile(filepath.Join(manager.dir, "state.json"), data); saveErr != nil {
		err, status.Error = saveErr, "cannot save rule update state"
	}
	manager.mu.Lock()
	previous := manager.status.Version
	if err == nil {
		if library != nil {
			manager.files = library
		}
		manager.status = status
	} else {
		manager.status.LastChecked, manager.status.Error = status.LastChecked, status.Error
	}
	manager.mu.Unlock()
	if err == nil && previous != status.Version && validSnapshotID(previous) {
		os.Remove(filepath.Join(manager.dir, previous+".zip"))
	}
}

func (manager *RuleUpdateManager) download(ctx context.Context, etag string) (*zip.Reader, []byte, string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, manager.url, nil)
	if err != nil {
		return nil, nil, "", errors.New("cannot create upstream rule request")
	}
	request.Header.Set("User-Agent", "clash_ACL-rule-update")
	if etag != "" {
		request.Header.Set("If-None-Match", etag)
	}
	response, err := manager.client.Do(request)
	if err != nil {
		return nil, nil, "", errors.New("upstream rule request failed")
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotModified && etag != "" {
		return nil, nil, etag, nil
	}
	if response.StatusCode != http.StatusOK {
		return nil, nil, "", fmt.Errorf("upstream rule request returned HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxRuleArchiveSize+1))
	if err != nil || len(data) > maxRuleArchiveSize {
		return nil, nil, "", errors.New("cannot read upstream archive or archive exceeds 64 MiB")
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, nil, "", errors.New("invalid upstream rule archive")
	}
	files, err := importUpstreamRules(reader)
	if err != nil {
		return nil, nil, "", err
	}
	archive, err := packRuleFiles(files)
	if err != nil {
		return nil, nil, "", err
	}
	library, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, nil, "", errors.New("cannot prepare updated rule library")
	}
	return library, archive, response.Header.Get("ETag"), nil
}

func importUpstreamRules(archive *zip.Reader) (map[string][]byte, error) {
	entries := map[string]*zip.File{}
	root := ""
	for _, file := range archive.File {
		prefix, relative, found := strings.Cut(file.Name, "/")
		if !found || !strings.HasPrefix(prefix, "ACL4SSR-") {
			continue
		}
		if root == "" {
			root = prefix
		}
		if prefix != root || !fs.ValidPath(relative) {
			continue
		}
		if _, duplicate := entries[relative]; duplicate {
			return nil, errors.New("duplicate upstream archive entry")
		}
		entries[relative] = file
	}
	files := map[string][]byte{}
	total := 0
	read := func(relative string) ([]byte, error) {
		file := entries[relative]
		if file == nil || file.UncompressedSize64 > maxSubscriptionSize {
			return nil, errors.New("upstream archive has a missing or oversized required file")
		}
		reader, err := file.Open()
		if err != nil {
			return nil, errors.New("cannot open upstream rule file")
		}
		defer reader.Close()
		data, err := io.ReadAll(io.LimitReader(reader, maxSubscriptionSize+1))
		total += len(data)
		if err != nil || len(data) > maxSubscriptionSize || total > maxRuleArchiveSize {
			return nil, errors.New("cannot read upstream rules or rules exceed size limit")
		}
		return []byte(strings.ReplaceAll(string(data), "\r\n", "\n")), nil
	}
	required := map[string]bool{"rules/ACL4SSR/Clash/Ruleset/AI.list": true, "rules/ACL4SSR/Clash/Ruleset/OpenAi.list": true}
	for _, preset := range rulePresets {
		data, err := read(strings.TrimPrefix(preset.file, "rules/ACL4SSR/"))
		if err != nil {
			return nil, fmt.Errorf("upstream template %s: %w", preset.ID, err)
		}
		text := string(data)
		for _, prefix := range []string{"https://raw.githubusercontent.com/ACL4SSR/ACL4SSR/master/Clash/", "https://raw.githubusercontent.com/ACL4SSR/ACL4SSR/refs/heads/master/Clash/"} {
			text = strings.ReplaceAll(text, prefix, "../")
		}
		data = []byte(strings.TrimRight(text, "\r\n") + "\n")
		sets, err := parseRuleConfig(data)
		if err != nil {
			return nil, fmt.Errorf("upstream template %s: %w", preset.ID, err)
		}
		files[preset.file] = data
		for _, set := range sets {
			if set.url == "" {
				continue
			}
			file := path.Clean(path.Join(path.Dir(preset.file), set.url))
			if !strings.HasPrefix(file, "rules/ACL4SSR/Clash/") {
				return nil, errors.New("upstream rule references must remain inside Clash")
			}
			required[file] = true
		}
	}
	for file := range required {
		data, err := read(strings.TrimPrefix(file, "rules/ACL4SSR/"))
		if err != nil {
			return nil, err
		}
		files[file] = data
	}
	return files, nil
}

func packRuleFiles(files map[string][]byte) ([]byte, error) {
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	keys := make([]string, 0, len(files))
	for key := range files {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		entry, err := writer.Create(key)
		if err != nil {
			return nil, errors.New("cannot package updated rules")
		}
		if _, err := entry.Write(files[key]); err != nil {
			return nil, errors.New("cannot package updated rules")
		}
	}
	if err := writer.Close(); err != nil {
		return nil, errors.New("cannot finish updated rule package")
	}
	return output.Bytes(), nil
}

func validateRuleLibrary(ctx context.Context, library fs.FS) error {
	config := defaultConfig()
	for index, name := range []string{"香港 0.5x", "美国 1x", "日本 2x"} {
		config.SelfHostedNodes = append(config.SelfHostedNodes, SelfHostedNode{ID: fmt.Sprint(index), Proxy: map[string]any{"name": name, "type": "ss", "server": "validation.invalid", "port": 443, "cipher": "aes-128-gcm", "password": fmt.Sprint(index)}})
	}
	config.AI = AIPolicy{NodeID: "1"}
	for _, preset := range rulePresets {
		if ctx.Err() != nil {
			return errors.New("rule update validation cancelled")
		}
		if _, _, err := generateConfig(ctx, newSourceManager(""), config, GenerationRequest{Rules: RuleRequest{Preset: preset.ID, SkipUnsupported: true, CustomRules: []string{"DOMAIN,validation.invalid,DIRECT", "MATCH,REJECT"}}}, false, library); err != nil {
			return fmt.Errorf("updated rules failed validation for %s: %w", preset.ID, err)
		}
	}
	return nil
}

func writeRuleFile(destination string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(destination), ".rules-*")
	if err != nil {
		return errors.New("cannot create rule update file")
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(data); err != nil {
		file.Close()
		return errors.New("cannot write rule update file")
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return errors.New("cannot sync rule update file")
	}
	if err := file.Close(); err != nil {
		return errors.New("cannot close rule update file")
	}
	if err := os.Rename(file.Name(), destination); err != nil {
		return errors.New("cannot save rule update file")
	}
	return nil
}
