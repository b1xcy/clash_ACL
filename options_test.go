package main

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
	if err := writeConfig(path, config); err != nil {
		t.Fatal(err)
	}
	handler := newHandler(path)
	body := `{"rename_rules":[{"match":"HK","replacement":"香港","regex":false}],"ai":{"node_id":"fixed","domains":["openai.com"]},"groups_json":"[{\"name\":\"节点选择\",\"type\":\"select\",\"extension\":9007199254740993}]"}`
	request := httptest.NewRequest(http.MethodPut, "/api/options", strings.NewReader(body))

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("cannot save options: %s", response.Body.String())
	}
	saved, err := readConfig(path)
	if err != nil || saved.Sources[0].URL != config.Sources[0].URL || saved.SelfHostedNodes[0].Proxy["extension"] != json.Number("9007199254740993") || saved.Groups[0]["extension"] != json.Number("9007199254740993") || saved.AI.NodeID != "fixed" {
		t.Fatal("options update changed sources or lost protocol/group fields")
	}
	request = httptest.NewRequest(http.MethodGet, "/api/options", nil)

	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "private-node-secret") || strings.Contains(response.Body.String(), "private-source") || !strings.Contains(response.Body.String(), "9007199254740993") {
		t.Fatal("settings response leaked credentials or changed the group JSON text")
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
		`{"ai":{"node_id":"airport"}}`,
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
