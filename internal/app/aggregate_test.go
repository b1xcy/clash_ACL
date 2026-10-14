package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func aggregateFixture(name, password string) map[string]any {
	return map[string]any{"name": name, "type": "trojan", "server": "example.invalid", "port": 443, "password": password}
}

func TestAggregationIdentityAndNameConflicts(t *testing.T) {
	first := aggregateFixture("Same", "password-one")
	duplicate := aggregateFixture("Other", "password-one")
	duplicate["port"] = "443"
	differentCredential := aggregateFixture("Same", "password-two")
	differentProtocol := aggregateFixture("Same", "password-one")
	differentProtocol["type"] = "anytls"
	differentTLS := aggregateFixture("Same", "password-one")
	differentTLS["sni"] = "tls.invalid"
	largeExtension := aggregateFixture("Same", "password-one")
	largeExtension["extension"] = map[string]any{"number": json.Number("9007199254740993")}
	reserved := aggregateFixture("DIRECT", "reserved-password")
	inputs := []AggregatedNode{}
	for index, proxy := range []map[string]any{first, duplicate, differentCredential, differentProtocol, differentTLS, largeExtension, reserved} {
		inputs = append(inputs, AggregatedNode{Proxy: proxy, Origins: []NodeOrigin{{SourceID: string(rune('a' + index)), OriginalName: proxy["name"].(string)}}})
	}
	nodes, duplicates, err := aggregateNodes(inputs)
	if err != nil || len(nodes) != 6 || duplicates != 1 || len(nodes[0].Origins) != 2 || nodes[0].Name != "Same" {
		t.Fatal("connection identity did not preserve distinct nodes or merge duplicate origins")
	}
	names := make(map[string]bool)
	for _, node := range nodes {
		if names[node.Name] || node.Name == "DIRECT" || node.Proxy["name"] != node.Name {
			t.Fatal("final names conflict or were not written to node copies")
		}
		names[node.Name] = true
	}
	if first["name"] != "Same" || differentCredential["name"] != "Same" || reserved["name"] != "DIRECT" || !reflect.DeepEqual(nodes[4].Proxy["extension"], largeExtension["extension"]) {
		t.Fatal("aggregation modified inputs or discarded extensions")
	}
	id, err := nodeIdentity(first)
	if err != nil {
		t.Fatal(err)
	}
	first["name"] = "Renamed"
	renamedID, err := nodeIdentity(first)
	if err != nil || renamedID != id {
		t.Fatal("node name changed stable identity")
	}
	reordered, _, err := aggregateNodes([]AggregatedNode{inputs[3], inputs[0]})
	if err != nil || reordered[1].ID != id {
		t.Fatal("input order changed node identity")
	}
}

func TestAggregationAvoidsGeneratedNameCollision(t *testing.T) {
	one := aggregateFixture("Same", "one")
	two := aggregateFixture("Same", "two")
	id, _ := nodeIdentity(two)
	three := aggregateFixture("Same ["+strings.TrimPrefix(id, "node-")[:8]+"]", "three")
	nodes, _, err := aggregateNodes([]AggregatedNode{{Proxy: one}, {Proxy: two}, {Proxy: three}})
	if err != nil || nodes[1].Name == nodes[2].Name || nodes[2].Name != three["name"] {
		t.Fatal("generated suffix collided with an existing original name")
	}
}

func TestAggregationAPIUsesOnlyRequestedSourcesAndConfirmedLinks(t *testing.T) {
	dataDir := t.TempDir()
	path := filepath.Join(dataDir, "config.json")
	if err := writeConfig(path, defaultConfig()); err != nil {
		t.Fatal(err)
	}
	query := url.Values{"url": {"https://example.invalid/first?token=secret", "https://example.invalid/second"}}
	sources, err := requestSources(query)
	if err != nil {
		t.Fatal(err)
	}
	manager := newSourceManager(dataDir)
	now := time.Now()
	for index, source := range sources {
		password := "synthetic-secret"
		if index == 1 {
			password = "different-secret"
		}
		if err := manager.writeCache(source, SourceCache{LastSuccess: &now, LastAttempt: &now, Error: "upstream temporarily failed", Proxies: []map[string]any{aggregateFixture("Airport", password)}}); err != nil {
			t.Fatal(err)
		}
	}
	handler := newHandler(path)
	request := map[string]any{"urls": query["url"], "aggregate": false, "direct_links": "trojan://synthetic-secret@example.invalid:443#Direct\nanytls://new-secret@example.invalid:443#Airport"}
	body, _ := json.Marshal(request)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/aggregate/preview", strings.NewReader(string(body))))
	var result struct {
		Nodes      []AggregatedNode `json:"nodes"`
		Inputs     int              `json:"input_count"`
		Count      int              `json:"node_count"`
		Duplicates int              `json:"duplicate_count"`
		Sources    []SourceView     `json:"sources"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || result.Inputs != 3 || result.Count != 2 || result.Duplicates != 1 || len(result.Nodes[0].Origins) != 2 || len(result.Sources) != 2 || !result.Sources[0].UsingCache || result.Sources[1].Enabled {
		t.Fatalf("incorrect cached/URI aggregation: %s", response.Body.String())
	}
	for _, secret := range []string{"synthetic-secret", "different-secret", "new-secret", "example.invalid", "trojan://", "password"} {
		if strings.Contains(response.Body.String(), secret) {
			t.Fatal("aggregation preview exposed source or node credentials")
		}
	}
	saved, err := readConfig(path)
	if err != nil || len(saved.Sources) != 0 || len(saved.SelfHostedNodes) != 0 {
		t.Fatal("request aggregation wrote shared configuration")
	}
	for _, body := range []string{
		`null`, `{}`, `{} {}`, `{"unknown":true}`,
		`{"direct_links":"ssr://secret"}`, `{"urls":["file:///invalid"]}`,
	} {
		response = httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/aggregate/preview", strings.NewReader(body)))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("invalid aggregate input accepted: %s", body)
		}
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/aggregate/preview", strings.NewReader(`{"urls":["https://example.invalid/not-read"]}`)))
	if response.Code != http.StatusConflict {
		t.Fatal("missing enabled source was silently skipped")
	}
}
