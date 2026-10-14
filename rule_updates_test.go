package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func upstreamFixture(t *testing.T, mutate func(map[string][]byte)) []byte {
	t.Helper()
	files := map[string][]byte{}
	if err := fs.WalkDir(bundledRules, "rules/ACL4SSR/Clash", func(file string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := bundledRules.ReadFile(file)
		if err != nil {
			return err
		}
		files["ACL4SSR-master/"+strings.TrimPrefix(file, "rules/ACL4SSR/")] = data
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if mutate != nil {
		mutate(files)
	}
	data, err := packRuleFiles(files)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestRuleUpdateAtomicValidationPersistenceAndConditionalRequest(t *testing.T) {
	archive := upstreamFixture(t, func(files map[string][]byte) {
		files["ACL4SSR-master/Clash/UnBan.list"] = append(files["ACL4SSR-master/Clash/UnBan.list"], []byte("\nDOMAIN,autoupdate.example.invalid\n")...)
	})
	var calls atomic.Int32
	var fail atomic.Bool
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if fail.Load() {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		if r.Header.Get("If-None-Match") == `"fixture"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"fixture"`)
		w.Write(archive)
	}))
	defer source.Close()
	dir := t.TempDir()
	manager := newRuleUpdateManager(dir, true)
	manager.url = source.URL
	<-manager.startUpdate(context.Background())
	status := manager.view()
	if status.Error != "" || status.Origin != "upstream" || !validSnapshotID(status.Version) || status.LastUpdated == nil || status.NextCheck.Sub(*status.LastChecked) != 24*time.Hour {
		t.Fatal("valid update failed or schedule differs", status)
	}
	library, version := manager.current()
	result, err := compileRules(RuleRequest{Preset: "acl4ssr-online", SkipUnsupported: true}, library)
	if err != nil || !containsString(result.Rules, "DOMAIN,autoupdate.example.invalid,🎯 全球直连") {
		t.Fatal("updated rules not used", err)
	}
	restarted := newRuleUpdateManager(dir, true)
	if restarted.view().Version != version || restarted.view().Error != "" {
		t.Fatal("validated rule update did not survive restart", restarted.view())
	}
	before := *status.LastUpdated
	<-manager.startUpdate(context.Background())
	if calls.Load() != 2 || !manager.view().LastUpdated.Equal(before) || manager.view().Error != "" {
		t.Fatal("conditional unchanged update changed active rules")
	}
	fail.Store(true)
	<-manager.startUpdate(context.Background())
	if manager.view().Version != version || manager.view().Error == "" {
		t.Fatal("failed upstream request replaced active version")
	}
	if newRuleUpdateManager(dir, true).view().Version != version {
		t.Fatal("failed check damaged persisted active rules")
	}
	if err := os.WriteFile(filepath.Join(dir, "rule-updates", version+".zip"), []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if fallback := newRuleUpdateManager(dir, true).view(); fallback.Origin != "bundled" || fallback.Error == "" {
		t.Fatal("corrupt disk rules did not fall back to bundled rules")
	}
}

func TestInvalidRuleUpdatesRetainBundledLibrary(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(map[string][]byte)
	}{
		{"missing", func(files map[string][]byte) { delete(files, "ACL4SSR-master/Clash/UnBan.list") }},
		{"remote", func(files map[string][]byte) {
			key := "ACL4SSR-master/Clash/config/ACL4SSR_Online.ini"
			files[key] = bytes.ReplaceAll(files[key], []byte("../UnBan.list"), []byte("https://unsupported.invalid/rules"))
		}},
		{"syntax", func(files map[string][]byte) {
			files["ACL4SSR-master/Clash/UnBan.list"] = []byte("UNSUPPORTED,example.invalid")
		}},
		{"target", func(files map[string][]byte) {
			key := "ACL4SSR-master/Clash/config/ACL4SSR_Online.ini"
			files[key] = bytes.Replace(files[key], []byte("ruleset=🎯 全球直连"), []byte("ruleset=Missing"), 1)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			archive := upstreamFixture(t, test.mutate)
			source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(archive) }))
			defer source.Close()
			manager := newRuleUpdateManager(t.TempDir(), false)
			manager.url = source.URL
			<-manager.startUpdate(context.Background())
			if manager.view().Version != bundledRuleVersion || manager.view().Error == "" {
				t.Fatal("invalid rule update replaced bundled rules")
			}
		})
	}
}

func TestRuleUpdatesRebuildExistingSnapshotsWithoutFetchingSubscriptions(t *testing.T) {
	var fetches atomic.Int32
	subscription := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetches.Add(1)
		w.Write([]byte("proxies:\n  - {name: Airport 0.5x, type: ss, server: airport.invalid, port: 443, cipher: aes-128-gcm, password: synthetic-secret}\n"))
	}))
	defer subscription.Close()
	dir := t.TempDir()
	updates := newRuleUpdateManager(dir, false)
	store := newSnapshotStore(dir, newSourceManager(dir), updates)
	config := generationFixture()
	config.SelfHostedNodes[0].Proxy["extension"] = map[string]any{"decimal": json.Number("0.12345678901234567890123456789"), "large": json.Number("184467440737095516171267719")}
	config.RenameRules = []RenameRule{{Match: "Airport", Replacement: "Airport-renamed"}}
	plan, id, _ := snapshotPlan(config, GenerationRequest{URLs: []string{subscription.URL}, Rules: RuleRequest{Preset: "acl4ssr-online", SkipUnsupported: true}})
	first, err := store.get(context.Background(), id, plan, false)
	if err != nil {
		t.Fatal(err)
	}
	archive := upstreamFixture(t, func(files map[string][]byte) {
		files["ACL4SSR-master/Clash/UnBan.list"] = append(files["ACL4SSR-master/Clash/UnBan.list"], []byte("\nDOMAIN,latest.example.invalid\n")...)
	})
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(archive) }))
	defer source.Close()
	updates.url = source.URL
	<-updates.startUpdate(context.Background())
	if updates.view().Error != "" {
		t.Fatal(updates.view().Error)
	}
	second, err := store.get(context.Background(), id, plan, false)
	if err != nil || fetches.Load() != 1 || !strings.Contains(second.YAML, "DOMAIN,latest.example.invalid") || second.Preview.RulesVersion == first.Preview.RulesVersion {
		t.Fatal("rule update failed to regenerate snapshot without fetching sources", err)
	}
	if second.Preview.Nodes[0].ID != first.Preview.Nodes[0].ID || second.Preview.Nodes[0].Name != first.Preview.Nodes[0].Name {
		t.Fatal("rule-only rebuild changed node identity or renamed nodes twice")
	}
	if !strings.Contains(second.YAML, "0.12345678901234567890123456789") || !strings.Contains(second.YAML, "184467440737095516171267719") || strings.Contains(second.YAML, "Airport-renamed-renamed") {
		t.Fatal("rule rebuild lost proxy precision or reapplied renaming")
	}
	if _, err := store.get(context.Background(), id, plan, false); err != nil || fetches.Load() != 1 {
		t.Fatal("up-to-date snapshot unexpectedly fetched subscription")
	}
}

func TestRuleUpdateScheduleAndManualChecksShareWork(t *testing.T) {
	archive := upstreamFixture(t, nil)
	var calls atomic.Int32
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		started <- struct{}{}
		<-release
		w.Write(archive)
	}))
	defer source.Close()
	manager := newRuleUpdateManager(t.TempDir(), true)
	manager.url = source.URL
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan struct{})
	go func() { manager.run(ctx); close(finished) }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("startup check did not run")
	}
	flight := manager.startUpdate(ctx)
	if flight != manager.startUpdate(ctx) {
		t.Fatal("manual checks started duplicate updates")
	}
	close(release)
	<-flight
	cancel()
	<-finished
	if calls.Load() != 1 || manager.view().Error != "" {
		t.Fatal("scheduled and manual updates did not share work", manager.view())
	}
	ctx, cancel = context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { manager.run(ctx); close(done) }()
	cancel()
	<-done
	if calls.Load() != 1 {
		t.Fatal("restart repeated a check before 24 hours elapsed")
	}
}

func TestUpstreamRemoteReferencesAreLocalAndVersionPackagingIsStable(t *testing.T) {
	archive := upstreamFixture(t, func(files map[string][]byte) {
		for key, data := range files {
			if strings.HasSuffix(key, ".ini") {
				files[key] = bytes.ReplaceAll(data, []byte(",../"), []byte(",https://raw.githubusercontent.com/ACL4SSR/ACL4SSR/master/Clash/"))
			}
		}
	})
	reader, _ := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	files, err := importUpstreamRules(reader)
	if err != nil {
		t.Fatal(err)
	}
	packed, err := packRuleFiles(files)
	if err != nil {
		t.Fatal(err)
	}
	again, _ := packRuleFiles(files)
	if !bytes.Equal(packed, again) {
		t.Fatal("identical rules produced changing version fingerprints")
	}
	library, _ := zip.NewReader(bytes.NewReader(packed), int64(len(packed)))
	for _, preset := range rulePresets {
		sets, _, _, err := loadRulePreset(preset.ID, library)
		if err != nil {
			t.Fatal(err)
		}
		for _, set := range sets {
			if strings.Contains(set.url, ":") {
				t.Fatal("upstream remote rule address was retained")
			}
		}
	}
}
