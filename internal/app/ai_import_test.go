package app

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

const importedAnyTLS = "anytls://synthetic-exit-password@exit.example.invalid:443?sni=exit.example.invalid#Private%20Exit"
const importedEntry = "ss://YWVzLTEyOC1nY206c3ludGhldGljLWVudHJ5LXBhc3N3b3Jk@entry.example.invalid:443#Entry%200.5x"

func TestMixedProtocolImportsCanEachBeSelectedAsAIExit(t *testing.T) {
	links := []string{
		"ss://aes-128-gcm:synthetic-ss-password@ss.example.invalid:443#SS%20Exit",
		"tuic://11111111-1111-4111-8111-111111111111:synthetic-tuic-password@tuic.example.invalid:443?congestion_control=bbr&alpn=h3#TUIC%20Exit",
		"vless://11111111-1111-4111-8111-111111111111@vless.example.invalid:443?security=tls&type=ws&host=vless.example.invalid&path=%2Fsocket&encryption=none#VLESS%20Exit",
		"vmess://11111111-1111-4111-8111-111111111111@vmess.example.invalid:443?security=tls&type=ws&host=vmess.example.invalid&path=%2Fsocket#VMess%20Exit",
		"trojan://synthetic-trojan-password@trojan.example.invalid:443?sni=trojan.example.invalid#Trojan%20Exit",
		"hysteria2://synthetic-hysteria-password@hy2.example.invalid:443?sni=hy2.example.invalid#Hysteria2%20Exit",
		importedAnyTLS,
	}
	text := importedEntry + "\n" + strings.Join(links, "\n")
	for _, line := range parseURILines(strings.Join(links, "\n")) {
		if line.Error != "" {
			t.Fatal("mixed protocol import failed", line.Type, line.Error)
		}
		t.Run(line.Type, func(t *testing.T) {
			for _, chain := range []bool{false, true} {
				request := GenerationRequest{DirectLinks: text, AI: &AIPolicy{NodeID: line.NodeID, Chain: chain}, Rules: RuleRequest{Preset: "none"}}
				output, preview, err := generateConfig(context.Background(), newSourceManager(t.TempDir()), defaultConfig(), request, false)
				if err != nil {
					t.Fatal(err)
				}
				if len(preview.Nodes) != 8 {
					t.Fatal("mixed protocol nodes were lost")
				}
				for _, node := range preview.Nodes {
					if node.ID != line.NodeID {
						continue
					}
					actual := map[string]any{}
					for key, value := range node.Proxy {
						if key != "dialer-proxy" {
							actual[key] = value
						}
					}
					expected, _ := json.Marshal(line.Proxy)
					got, _ := json.Marshal(actual)
					if string(got) != string(expected) {
						t.Fatal("AI selection changed protocol parameters", line.Type)
					}
					if chain && node.Proxy["dialer-proxy"] != aiChainGroup {
						t.Fatal("chain was not applied to selected protocol")
					}
				}
				for _, group := range preview.Groups {
					if group["name"] == aiGroup {
						members, _ := stringList(group["proxies"])
						if !reflect.DeepEqual(members, []string{line.Name}) {
							t.Fatal("AI group chose a different protocol or node")
						}
					}
				}
				validateImportedAIWithMihomo(t, output)
			}
		})
	}
}

func TestImportedAnyTLSAIExitAndChainKeepRulesAndIdentity(t *testing.T) {
	lines := parseURILines(importedAnyTLS)
	if len(lines) != 1 || lines[0].Error != "" || !strings.HasPrefix(lines[0].NodeID, "node-") {
		t.Fatal("AnyTLS import does not expose a stable node ID", lines)
	}
	for _, chain := range []bool{false, true} {
		t.Run(map[bool]string{false: "direct", true: "chain"}[chain], func(t *testing.T) {
			request := GenerationRequest{DirectLinks: importedEntry + "\n" + importedAnyTLS + "\n" + strings.Replace(importedAnyTLS, "Private%20Exit", "Duplicate%20Exit", 1), AI: &AIPolicy{NodeID: lines[0].NodeID, Chain: chain, Domains: []string{"relay.example.invalid"}}, Policy: &NodePolicyOptions{RenameRules: []RenameRule{{Match: "Private", Replacement: "Renamed"}}}, Rules: RuleRequest{Preset: "none", CustomRules: []string{"DOMAIN-SUFFIX,openai.com,DIRECT"}}}
			output, preview, err := generateConfig(context.Background(), newSourceManager(t.TempDir()), defaultConfig(), request, false)
			if err != nil {
				t.Fatal(err)
			}
			if len(preview.Nodes) != 2 || preview.DuplicateCount != 1 {
				t.Fatal("AI exit did not deduplicate")
			}
			var document struct {
				Proxies []map[string]any `yaml:"proxies"`
				Groups  []map[string]any `yaml:"proxy-groups"`
				Rules   []string         `yaml:"rules"`
			}
			if err := yaml.Unmarshal(output, &document); err != nil {
				t.Fatal(err)
			}
			for _, proxy := range document.Proxies {
				if proxy["type"] == "anytls" {
					if proxy["name"] != "Renamed Exit" || proxy["password"] != "synthetic-exit-password" {
						t.Fatal("AnyTLS parameters or final name changed")
					}
					if chain && proxy["dialer-proxy"] != aiChainGroup {
						t.Fatal("AnyTLS exit not chained through entry group")
					}
					if !chain && proxy["dialer-proxy"] != nil {
						t.Fatal("disabled chain modified proxy")
					}
				}
			}
			fixed, entry := false, false
			for _, group := range preview.Groups {
				members, _ := stringList(group["proxies"])
				if group["name"] == aiGroup {
					fixed = true
					if !reflect.DeepEqual(members, []string{"Renamed Exit"}) {
						t.Fatal("AI group changed final exit")
					}
				}
				if group["name"] == aiChainGroup {
					entry = true
					if group["type"] != "select" || !reflect.DeepEqual(members, []string{aiAutoGroup, "DIRECT", "Entry 0.5x"}) {
						t.Fatal("entry group has incorrect candidates")
					}
				}
			}
			if !fixed || entry != chain || len(document.Rules) < 10 || document.Rules[len(document.Rules)-1] != "MATCH,"+mainGroup {
				t.Fatal("chain lost AI groups, routing rules or fallback")
			}
			guard := "DOMAIN-SUFFIX,openai.com," + aiGroup
			custom := "DOMAIN-SUFFIX,openai.com,DIRECT"
			rules := strings.Join(document.Rules, "\n")
			if strings.Index(rules, guard) < 0 || strings.Index(rules, guard) > strings.Index(rules, custom) {
				t.Fatal("AI rules shadowed by DIRECT")
			}
			validateImportedAIWithMihomo(t, output)
		})
	}
}

func validateImportedAIWithMihomo(t *testing.T, output []byte) {
	t.Helper()
	kernel := os.Getenv("CLASH_ACL_TEST_MIHOMO")
	if kernel == "" {
		return
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "imported-ai.yaml")
	if dataDir := os.Getenv("CLASH_ACL_TEST_MIHOMO_DATA"); dataDir != "" {
		for _, name := range []string{"Country.mmdb", "ASN.mmdb"} {
			data, err := os.ReadFile(filepath.Join(dataDir, name))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := os.WriteFile(file, output, 0600); err != nil {
		t.Fatal(err)
	}
	if logs, err := exec.Command(kernel, "-t", "-d", dir, "-f", file).CombinedOutput(); err != nil {
		t.Fatalf("Mihomo rejected imported AI config: %v\n%s", err, logs)
	}
}

func TestImportedAIOptionsPersistAndMissingExitDoesNotFallback(t *testing.T) {
	id := parseURILines(importedAnyTLS)[0].NodeID
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := writeConfig(path, defaultConfig()); err != nil {
		t.Fatal(err)
	}
	handler := newHandler(path)
	body := `{"ai":{"node_id":"` + id + `","chain":true,"domains":["relay.example.invalid"]},"groups_json":"[]"}`
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/api/options", strings.NewReader(body)))
	if response.Code != 200 {
		t.Fatal("imported AI selection could not be saved", response.Body.String())
	}
	config, err := readConfig(path)
	if err != nil || config.AI.NodeID != id || !config.AI.Chain {
		t.Fatal("imported AI binding not persisted")
	}
	if _, _, err := generateConfig(context.Background(), newSourceManager(dir), config, GenerationRequest{DirectLinks: importedEntry, Rules: RuleRequest{Preset: "none"}}, false); err == nil {
		t.Fatal("missing exit silently fell back")
	}
	if _, _, err := generateConfig(context.Background(), newSourceManager(dir), config, GenerationRequest{DirectLinks: importedAnyTLS, Rules: RuleRequest{Preset: "none"}}, false); err == nil {
		t.Fatal("chain without entry fell back to direct")
	}
	if _, _, err := generateConfig(context.Background(), newSourceManager(dir), config, GenerationRequest{DirectLinks: importedEntry + "\n" + importedAnyTLS, Rules: RuleRequest{Preset: "none"}}, false); err != nil {
		t.Fatal(err)
	}
}

func TestSubscriptionNodesCanBeSelectedAsAIExitAndRemovedSourcesFail(t *testing.T) {
	proxy := map[string]any{"name": "From YAML", "type": "anytls", "server": "yaml.example.invalid", "port": 443, "password": "synthetic-password"}
	id, _ := nodeIdentity(proxy)
	data, _ := marshalMihomo(map[string]any{"proxies": []map[string]any{proxy}})
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(data) }))
	defer source.Close()
	request := GenerationRequest{URLs: []string{source.URL}, AI: &AIPolicy{NodeID: id}, Rules: RuleRequest{Preset: "none"}}
	if _, preview, err := generateConfig(context.Background(), newSourceManager(t.TempDir()), defaultConfig(), request, false); err != nil || len(preview.Nodes) != 1 {
		t.Fatal("YAML AI exit unavailable", err)
	}
	if nodes := previewNodes([]map[string]any{proxy}); len(nodes) != 1 || nodes[0].ID != id {
		t.Fatal("subscription preview lacks AI binding identity")
	}
	request.URLs = nil
	request.DirectLinks = importedEntry
	if _, _, err := generateConfig(context.Background(), newSourceManager(t.TempDir()), defaultConfig(), request, false); err == nil {
		t.Fatal("disabled source AI exit was replaced")
	}
}

func TestAIChainSnapshotRebuildPreservesParametersAndRejectsCycles(t *testing.T) {
	dir := t.TempDir()
	id := parseURILines(importedAnyTLS)[0].NodeID
	request := GenerationRequest{DirectLinks: importedEntry + "\n" + importedAnyTLS, AI: &AIPolicy{NodeID: id, Chain: true}, Rules: RuleRequest{Preset: "none"}}
	plan, key, err := snapshotPlan(defaultConfig(), request)
	if err != nil {
		t.Fatal(err)
	}
	store := newSnapshotStore(dir, newSourceManager(dir))
	first, err := store.get(context.Background(), key, plan, false)
	if err != nil {
		t.Fatal(err)
	}
	first.Preview.RulesVersion = "old-version"
	if err := store.write(key, first); err != nil {
		t.Fatal(err)
	}
	restarted := newSnapshotStore(dir, newSourceManager(dir))
	second, err := restarted.get(context.Background(), key, plan, false)
	if err != nil || first.YAML != second.YAML {
		t.Fatal("rule rebuild changed AI chain", err)
	}
	before, _ := json.Marshal(plan)
	if _, _, err := generateConfig(context.Background(), newSourceManager(dir), plan.Config, plan.Request, false); err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(plan)
	if string(before) != string(after) {
		t.Fatal("generation modified imported link or defaults")
	}
	config := generationFixture()
	config.AI = AIPolicy{NodeID: "unknown", Chain: true}
	config.SelfHostedNodes[0].Proxy["dialer-proxy"] = "美国"
	if _, _, err := generateConfig(context.Background(), newSourceManager(dir), config, GenerationRequest{Rules: RuleRequest{Preset: "none"}}, false); err == nil {
		t.Fatal("AI chain accepted a dependency cycle")
	}
}
