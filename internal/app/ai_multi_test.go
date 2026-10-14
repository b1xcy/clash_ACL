package app

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"
)

func TestMultipleAIExitsAutoEntryAndOverrides(t *testing.T) {
	second := "trojan://synthetic-second@second.example.invalid:8443#JP%20Exit"
	links := importedEntry + "\n" + importedAnyTLS + "\n" + second
	firstID, secondID := parseURILines(importedAnyTLS)[0].NodeID, parseURILines(second)[0].NodeID
	for _, item := range []struct {
		ids      []string
		url      string
		expected string
	}{
		{[]string{firstID, secondID, firstID}, "", "https://exit.example.invalid/"},
		{[]string{secondID, firstID}, "", "https://second.example.invalid/"},
		{[]string{firstID, secondID}, "http://health.example.invalid:8080/ready", "http://health.example.invalid:8080/ready"},
	} {
		request := GenerationRequest{DirectLinks: links, AI: &AIPolicy{NodeIDs: item.ids, Chain: true, TestURL: item.url}, Rules: RuleRequest{Preset: "none"}}
		output, preview, err := generateConfig(context.Background(), newSourceManager(t.TempDir()), defaultConfig(), request, false)
		if err != nil {
			t.Fatal(err)
		}
		expected := []string{"Private Exit", "🇯🇵 JP Exit"}
		if item.ids[0] == secondID {
			expected[0], expected[1] = expected[1], expected[0]
		}
		for _, group := range preview.Groups {
			members, _ := stringList(group["proxies"])
			switch group["name"] {
			case aiGroup:
				if !reflect.DeepEqual(members, expected) {
					t.Fatal("exit order or deduplication changed", members)
				}
			case aiAutoGroup:
				if group["type"] != "url-test" || group["url"] != item.expected || !reflect.DeepEqual(members, []string{"Entry 0.5x"}) {
					t.Fatal("auto entry includes an exit or tests incorrect target", group)
				}
			case aiChainGroup:
				if !reflect.DeepEqual(members, []string{aiAutoGroup, "Entry 0.5x", "DIRECT"}) {
					t.Fatal("manual override or automatic default missing")
				}
			}
		}
		for _, node := range preview.Nodes {
			if node.ID == firstID || node.ID == secondID {
				if node.Proxy["dialer-proxy"] != aiChainGroup {
					t.Fatal("chain not applied to all exits")
				}
			}
		}
		validateImportedAIWithMihomo(t, output)
	}
	for _, ai := range []AIPolicy{
		{NodeIDs: []string{firstID, "node-missing"}},
		{NodeIDs: []string{firstID, secondID, parseURILines(importedEntry)[0].NodeID}, Chain: true},
		{NodeIDs: []string{firstID, secondID}, Chain: true, TestURL: "anytls://bad.invalid"},
	} {
		if _, _, err := generateConfig(context.Background(), newSourceManager(t.TempDir()), defaultConfig(), GenerationRequest{DirectLinks: links, AI: &ai, Rules: RuleRequest{Preset: "none"}}, false); err == nil {
			t.Fatal("invalid multi-exit policy accepted", ai)
		}
	}
}

func TestMultipleAIExitsPersistenceLegacyClearAndIPv6(t *testing.T) {
	firstID := parseURILines(importedAnyTLS)[0].NodeID
	config := defaultConfig()
	config.AI = AIPolicy{NodeIDs: []string{firstID}, Chain: true, TestURL: "https://health.example.invalid/"}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := writeConfig(path, config); err != nil {
		t.Fatal(err)
	}
	saved, err := readConfig(path)
	if err != nil || !reflect.DeepEqual(saved.AI, config.AI) {
		t.Fatal("multi-exit defaults lost", err)
	}
	config.AI = AIPolicy{NodeID: firstID, NodeIDs: []string{}}
	if err := writeConfig(path, config); err != nil {
		t.Fatal(err)
	}
	saved, err = readConfig(path)
	if err != nil || len(saved.AI.selectedIDs()) != 0 {
		t.Fatal("explicit clear restored legacy binding")
	}
	config.AI = AIPolicy{NodeID: firstID}
	if !reflect.DeepEqual(config.AI.selectedIDs(), []string{firstID}) {
		t.Fatal("legacy binding not migrated")
	}
	link := "anytls://synthetic-ipv6@[2001:db8::1]:9443#IPv6%20Exit"
	id := parseURILines(link)[0].NodeID
	_, preview, err := generateConfig(context.Background(), newSourceManager(t.TempDir()), defaultConfig(), GenerationRequest{DirectLinks: importedEntry + "\n" + link, AI: &AIPolicy{NodeIDs: []string{id}, Chain: true}, Rules: RuleRequest{Preset: "none"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, group := range preview.Groups {
		if group["name"] == aiAutoGroup && group["url"] != "https://[2001:db8::1]/" {
			t.Fatal("IPv6 URL malformed")
		}
	}
	dir := t.TempDir()
	request := GenerationRequest{DirectLinks: importedEntry + "\n" + importedAnyTLS, AI: &AIPolicy{NodeIDs: []string{firstID}, Chain: true}, Rules: RuleRequest{Preset: "none"}}
	plan, key, _ := snapshotPlan(defaultConfig(), request)
	store := newSnapshotStore(dir, newSourceManager(dir))
	before, err := store.get(context.Background(), key, plan, false)
	if err != nil {
		t.Fatal(err)
	}
	before.Preview.RulesVersion = "old-version"
	if err := store.write(key, before); err != nil {
		t.Fatal(err)
	}
	after, err := store.get(context.Background(), key, plan, false)
	if err != nil || after.YAML != before.YAML {
		t.Fatal("rules-only rebuild changed multi-exit chain", err)
	}
	encoded, _ := json.Marshal(after.Plan.Request.AI)
	if string(encoded) == "null" || !reflect.DeepEqual(after.Plan.Request.AI.NodeIDs, []string{firstID}) {
		t.Fatal("snapshot lost selected IDs")
	}
}
