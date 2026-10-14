package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSelfHostedEditingPreservesOtherSettingsAndNumbers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	config := generationFixture()
	config.Sources = []Source{{ID: "saved", URL: "https://example.invalid/sub", Enabled: true}}
	config.AI = AIPolicy{NodeID: "unknown"}
	config.Base = map[string]any{"mixed-port": 8999}
	if err := writeConfig(path, config); err != nil {
		t.Fatal(err)
	}
	handler := newHandler(path)
	body := `[{"id":"unknown","proxy":{"name":"Fixed","type":"ss","server":"fixed.invalid","port":443,"cipher":"aes-128-gcm","password":"synthetic-secret","extension":{"large":9007199254740993}}}]`
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/api/self-hosted", strings.NewReader(body)))
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	saved, err := readConfig(path)
	if err != nil || len(saved.Sources) != 1 || saved.AI.NodeID != "unknown" || saved.Base["mixed-port"] != json.Number("8999") || saved.SelfHostedNodes[0].Proxy["extension"].(map[string]any)["large"] != json.Number("9007199254740993") {
		t.Fatal("self-hosted update changed unrelated fields or numeric precision")
	}
	read := httptest.NewRecorder()
	handler.ServeHTTP(read, httptest.NewRequest(http.MethodGet, "/api/self-hosted", nil))
	if read.Code != 200 || !strings.Contains(read.Body.String(), "9007199254740993") || read.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("editable JSON did not retain precise parameters")
	}
	before, _ := os.ReadFile(path)
	for _, invalid := range []string{`null`, `[]`, `[] []`, `[{}]`, `[{"id":"x","proxy":{"name":"Fixed"}}]`} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/api/self-hosted", strings.NewReader(invalid)))
		if response.Code != 400 {
			t.Fatal("invalid nodes or removal of AI-bound node accepted")
		}
		after, _ := os.ReadFile(path)
		if string(before) != string(after) {
			t.Fatal("rejected node edit changed configuration")
		}
	}
}

func TestGenerationGroupsJSONPreservesLargeIntegers(t *testing.T) {
	request, err := decodeGenerationRequest(strings.NewReader(`{"groups_json":"[{\"name\":\"Custom\",\"type\":\"select\",\"proxies\":[\"DIRECT\"],\"extension\":9007199254740993}]"}`))
	if err != nil || request.Groups == nil || (*request.Groups)[0]["extension"] != json.Number("9007199254740993") {
		t.Fatal("generation groups_json lost exact integers", err)
	}
	for _, invalid := range []string{`{"groups":[],"groups_json":"[]"}`, `{"groups_json":"null"}`, `{"groups_json":"[] []"}`} {
		if _, err := decodeGenerationRequest(strings.NewReader(invalid)); err == nil {
			t.Fatal("invalid or ambiguous group input accepted")
		}
	}
}
