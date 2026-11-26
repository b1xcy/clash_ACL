package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestConversionOptionsPreserveSourcesAndPrivateNodeFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	config := defaultConfig()
	config.Sources = []Source{{ID: "source", URL: "https://example.invalid/sub?token=private-source", Enabled: true}}
	config.SelfHostedNodes = []SelfHostedNode{{ID: "fixed", Proxy: map[string]any{"name": "Fixed", "password": "private-node-secret", "extension": json.Number("9007199254740993")}}}
	config.AI = AIPolicy{NodeID: "fixed", NodeIDs: []string{"fixed"}, TestURL: "https://private-health.example.invalid/"}
	if err := writeConfig(path, config); err != nil {
		t.Fatal(err)
	}
	handler := newHandler(path)
	request := httptest.NewRequest(http.MethodGet, "/api/options", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var initial struct {
		Options ConversionOptions `json:"options"`
	}
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &initial) != nil || strings.Contains(response.Body.String(), "private-health.example.invalid") || initial.Options.AI.NodeID != "" || len(initial.Options.AI.NodeIDs) != 0 || initial.Options.AI.TestURL != "" {
		t.Fatal("GET /api/options leaked node_ids or test_url from server config")
	}

	body := `{"drop_plaintext":true,"source_drop_plaintext":{"public":true,"private":false},"rename_rules":[{"match":"HK","replacement":"香港","regex":false}],"ai":{"node_id":"fixed","node_ids":["fixed"],"test_url":"https://private-health.example.invalid/","chain":true,"domains":["openai.com"]},"groups_json":"[{\"name\":\"节点选择\",\"type\":\"select\",\"extension\":9007199254740993}]"}`
	request = httptest.NewRequest(http.MethodPut, "/api/options", strings.NewReader(body))

	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("cannot save options: %s", response.Body.String())
	}
	saved, err := readConfig(path)
	if err != nil || !saved.DropPlaintext || !saved.SourceDropPlaintext["public"] || len(saved.SourceDropPlaintext) != 2 {
		t.Fatal("plaintext filter setting was not persisted")
	}
	if err != nil || saved.Sources[0].URL != config.Sources[0].URL || saved.SelfHostedNodes[0].Proxy["extension"] != json.Number("9007199254740993") || saved.Groups[0]["extension"] != json.Number("9007199254740993") || !saved.AI.Chain || len(saved.AI.Domains) != 1 {
		t.Fatal("options update changed sources or lost protocol/group/AI fields")
	}
	if saved.AI.NodeID != "" || len(saved.AI.NodeIDs) != 0 || saved.AI.TestURL != "" {
		t.Fatal("PUT /api/options persisted node_id, node_ids, or test_url on the server")
	}
	request = httptest.NewRequest(http.MethodGet, "/api/options", nil)

	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "private-node-secret") || strings.Contains(response.Body.String(), "private-source") || strings.Contains(response.Body.String(), "private-health.example.invalid") || !strings.Contains(response.Body.String(), "9007199254740993") {
		t.Fatal("settings response leaked credentials/private AI fields or changed the group JSON text")
	}
}

func TestInvalidConversionOptionsDoNotChangeConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	config := defaultConfig()
	config.Rules = []string{"MATCH,DIRECT"}
	if err := writeConfig(path, config); err != nil {
		t.Fatal(err)
	}
	handler := newHandler(path)
	for _, body := range []string{
		`null`, `{} {}`, `{"rename_rules":[{"match":"[","regex":true}]}`,
		`{"groups_json":"null"}`, `{"groups_json":"[{}]"}`, `{"groups_json":"[] []"}`,
		`{"unknown_field":true}`,
	} {
		request := httptest.NewRequest(http.MethodPut, "/api/options", strings.NewReader(body))

		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("invalid options accepted: %s", body)
		}
		saved, err := readConfig(path)
		if err != nil || len(saved.Rules) != 1 || len(saved.Groups) != 0 || saved.AI.NodeID != "" || len(saved.RenameRules) != 0 {
			t.Fatal("invalid settings changed existing configuration")
		}
	}
}
