package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func generationFixture() Config {
	config := defaultConfig()
	config.SelfHostedNodes = []SelfHostedNode{
		{ID: "low", Proxy: map[string]any{"name": "香港 0.5x", "type": "ss", "server": "low.invalid", "port": json.Number("443"), "cipher": "aes-128-gcm", "password": "synthetic-low-secret"}},
		{ID: "unknown", Proxy: map[string]any{"name": "美国", "type": "ss", "server": "standard.invalid", "port": 443, "cipher": "aes-128-gcm", "password": "synthetic-standard-secret"}},
		{ID: "high", Proxy: map[string]any{"name": "日本 2x", "type": "ss", "server": "high.invalid", "port": 443, "cipher": "aes-128-gcm", "password": "synthetic-high-secret"}},
	}
	return config
}

func TestGenerateAllPresetsWithResolvedGroups(t *testing.T) {
	config := generationFixture()
	before, _ := json.Marshal(config)
	manager := newSourceManager(t.TempDir())
	for _, preset := range rulePresets {
		t.Run(preset.ID, func(t *testing.T) {
			output, preview, err := generateConfig(context.Background(), manager, config, GenerationRequest{Rules: RuleRequest{Preset: preset.ID, SkipUnsupported: true, CustomRules: []string{"DOMAIN,example.invalid,DIRECT", "MATCH,REJECT"}}}, false)
			if err != nil {
				t.Fatal(err)
			}
			var document struct {
				Proxies []map[string]any `yaml:"proxies"`
				Groups  []map[string]any `yaml:"proxy-groups"`
				Rules   []string         `yaml:"rules"`
				Mode    string           `yaml:"mode"`
			}
			if err := yaml.Unmarshal(output, &document); err != nil || len(document.Proxies) != 3 || document.Mode != "rule" || document.Rules[0] != "DOMAIN,example.invalid,DIRECT" || document.Rules[len(document.Rules)-1] != "MATCH,REJECT" {
				t.Fatal("generation lost nodes, mode or rule priority", err)
			}
			for _, group := range preview.Groups {
				members, _ := stringList(group["proxies"])
				if group["name"] == mainGroup && members[0] != lowGroup {
					t.Fatal("ordinary default does not prefer low rates")
				}
				if group["type"] != "select" && containsString(members, "日本 2x") {
					t.Fatal("high-rate node included in automatic backup")
				}
				if strings.Contains(preset.ID, "noauto") && group["type"] != "select" {
					t.Fatal("NoAuto preset contains an automatic group")
				}
				if group["name"] == "🇰🇷 韩国节点" || group["name"] == "🎥 奈飞节点" {
					t.Fatal("empty template node groups were retained")
				}
			}
		})
	}
	after, _ := json.Marshal(config)
	if string(before) != string(after) {
		t.Fatal("generation mutated saved configuration")
	}
}

func TestAIFixedExitAndRulesPrecedeGenericPolicies(t *testing.T) {
	config := generationFixture()
	config.AI = AIPolicy{NodeID: "unknown", Domains: []string{"relay.example.invalid"}}
	config.Rules = []string{"DOMAIN-SUFFIX,bing.com,DIRECT"}
	output, preview, err := generateConfig(context.Background(), newSourceManager(t.TempDir()), config, GenerationRequest{Rules: RuleRequest{Preset: "acl4ssr-full", SkipUnsupported: true, CustomRules: []string{"DOMAIN-SUFFIX,openai.com,DIRECT"}}}, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, group := range preview.Groups {
		members, _ := stringList(group["proxies"])
		if group["name"] == aiGroup && !reflect.DeepEqual(members, []string{"美国"}) {
			t.Fatal("AI group contains another exit")
		}
		if group["name"] == "💬 Ai平台" && !reflect.DeepEqual(members, []string{aiGroup}) {
			t.Fatal("template AI group permits another exit")
		}
	}
	var document struct {
		Rules []string `yaml:"rules"`
	}
	if err := yaml.Unmarshal(output, &document); err != nil {
		t.Fatal(err)
	}
	text := strings.Join(document.Rules, "\n")
	for _, rule := range []string{"DOMAIN,api.githubcopilot.com," + aiGroup, "DOMAIN,sydney.bing.com," + aiGroup, "DOMAIN-SUFFIX,openai.com," + aiGroup, "DOMAIN-SUFFIX,relay.example.invalid," + aiGroup} {
		if strings.Index(text, rule) < 0 || strings.Index(text, rule) > strings.Index(text, "DOMAIN-SUFFIX,bing.com,DIRECT") {
			t.Fatalf("AI domain guard missing or shadowed: %s at %d, generic at %d", rule, strings.Index(text, rule), strings.Index(text, "DOMAIN-SUFFIX,bing.com,DIRECT"))
		}
	}
	for _, ai := range []AIPolicy{{NodeID: "missing"}, {Domains: []string{"example.invalid"}}, {NodeID: "unknown", Domains: []string{"https://example.invalid/path"}}, {NodeID: "unknown", Domains: []string{"example.invalid,DIRECT"}}} {
		if _, _, err := generateConfig(context.Background(), newSourceManager(t.TempDir()), config, GenerationRequest{AI: &ai, Rules: RuleRequest{Preset: "none"}}, false); err == nil {
			t.Fatal("invalid AI policy accepted")
		}
	}
	config.SelfHostedNodes[1].Proxy["dialer-proxy"] = mainGroup
	if _, _, err := generateConfig(context.Background(), newSourceManager(t.TempDir()), config, GenerationRequest{Rules: RuleRequest{Preset: "none"}}, false); err == nil {
		t.Fatal("AI node can be dialed through another exit")
	}
}

func TestCustomGroupsIDsSelectorsConflictsAndCycles(t *testing.T) {
	config := generationFixture()
	id, _ := nodeIdentity(config.SelfHostedNodes[0].Proxy)
	config.RenameRules = []RenameRule{{Match: "香港", Replacement: "Renamed"}}
	config.Groups = []map[string]any{
		{"name": "ByID", "type": "select", "node_ids": []string{id}},
		{"name": "BySource", "type": "select", "rate_groups": []string{"standard"}, "source_ids": []string{"self:unknown"}},
		{"name": "Ⓜ️ 微软服务", "type": "select", "proxies": []string{"REJECT"}},
	}
	_, preview, err := generateConfig(context.Background(), newSourceManager(t.TempDir()), config, GenerationRequest{Rules: RuleRequest{Preset: "acl4ssr-online", SkipUnsupported: true}}, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, group := range preview.Groups {
		members, _ := stringList(group["proxies"])
		if group["name"] == "ByID" && !reflect.DeepEqual(members, []string{"Renamed 0.5x"}) {
			t.Fatal("stable ID failed after rename")
		}
		if group["name"] == "BySource" && !reflect.DeepEqual(members, []string{"美国"}) {
			t.Fatal("source or rate selector failed")
		}
		if group["name"] == "Ⓜ️ 微软服务" && !reflect.DeepEqual(members, []string{"REJECT"}) {
			t.Fatal("template replaced user group")
		}
	}
	for _, groups := range [][]map[string]any{
		{{"name": lowGroup, "type": "select", "proxies": []string{"DIRECT"}}},
		{{"name": "A", "type": "select", "proxies": []string{"Missing"}}},
		{{"name": "A", "type": "select", "proxies": []string{"B"}}, {"name": "B", "type": "select", "proxies": []string{"A"}}},
		{{"name": "A", "type": "select", "filter": "NoMatches"}},
		{{"name": "A", "type": "select", "node_ids": []string{"node-missing"}}},
		{{"name": "A", "type": "select", "rate_groups": []string{"unknown"}}},
		{{"name": "A", "type": "select", "include-all": true}},
	} {
		config.Groups = groups
		if _, _, err := generateConfig(context.Background(), newSourceManager(t.TempDir()), config, GenerationRequest{Rules: RuleRequest{Preset: "none"}}, false); err == nil {
			t.Fatal("invalid group policy accepted")
		}
	}
}

func TestGenerationNameCollisionAndExactYAMLNumbers(t *testing.T) {
	config := generationFixture()
	config.SelfHostedNodes[0].Proxy["name"] = mainGroup
	config.SelfHostedNodes[0].Proxy["extension"] = map[string]any{"large": json.Number("9007199254740993"), "text": "001"}
	config.Base = map[string]any{"mode": "global", "dns": map[string]any{"enable": false}, "mixed-port": 8899}
	output, preview, err := generateConfig(context.Background(), newSourceManager(t.TempDir()), config, GenerationRequest{Rules: RuleRequest{Preset: "none"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Nodes[0].Name == mainGroup || !strings.Contains(string(output), "large: 9007199254740993") || !strings.Contains(string(output), `text: "001"`) || !strings.Contains(string(output), "mode: rule") || !strings.Contains(string(output), "mixed-port: 8899") {
		t.Fatal("group collision, exact numeric values or base settings lost")
	}
	if config.SelfHostedNodes[0].Proxy["name"] != mainGroup {
		t.Fatal("input node name mutated")
	}
}

func TestGenerationPreviewPrivacyAndRequestValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := writeConfig(path, generationFixture()); err != nil {
		t.Fatal(err)
	}
	handler := newHandler(path)
	for _, body := range []string{`null`, `{} {}`, `{"typo":true}`} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/generate/preview", strings.NewReader(body)))
		if response.Code != http.StatusBadRequest {
			t.Fatal("invalid generation request accepted")
		}
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/generate/preview", strings.NewReader(`{"rules":{"preset":"none"}}`)))
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "synthetic-low-secret") || strings.Contains(response.Body.String(), "low.invalid") {
		t.Fatal("generation preview leaked credentials or failed", response.Body.String())
	}
}

func TestGeneratedConfigurationsWithMihomo(t *testing.T) {
	kernel := os.Getenv("CLASH_ACL_TEST_MIHOMO")
	if kernel == "" {
		t.Skip("set CLASH_ACL_TEST_MIHOMO to validate with a local Mihomo kernel")
	}
	dir := t.TempDir()
	dataDir := os.Getenv("CLASH_ACL_TEST_MIHOMO_DATA")
	if dataDir != "" {
		data, err := os.ReadFile(filepath.Join(dataDir, "Country.mmdb"))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "Country.mmdb"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, preset := range rulePresets {
		t.Run(preset.ID, func(t *testing.T) {
			config := generationFixture()
			config.SelfHostedNodes[1].Proxy["type"] = "anytls"
			delete(config.SelfHostedNodes[1].Proxy, "cipher")
			config.AI = AIPolicy{NodeID: "unknown", Chain: true, Domains: []string{"relay.example.invalid"}}
			output, _, err := generateConfig(context.Background(), newSourceManager(t.TempDir()), config, GenerationRequest{Rules: RuleRequest{Preset: preset.ID, SkipUnsupported: true}}, false)
			if err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(dir, preset.ID+".yaml")
			if err := os.WriteFile(file, output, 0600); err != nil {
				t.Fatal(err)
			}
			command := exec.Command(kernel, "-t", "-d", dir, "-f", file)
			if logs, err := command.CombinedOutput(); err != nil {
				t.Fatalf("Mihomo rejected generated configuration: %v\n%s", err, logs)
			}
		})
	}
}
