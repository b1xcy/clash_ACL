package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestAntigravityRulesUseAIExitBeforeBroadGoogleRule(t *testing.T) {
	config := generationFixture()
	request := GenerationRequest{
		AI:    &AIPolicy{NodeID: "unknown"},
		Rules: RuleRequest{Preset: "none", CustomRules: []string{"DOMAIN-SUFFIX,googleapis.com,DIRECT"}},
	}
	output, preview, err := generateConfig(context.Background(), newSourceManager(t.TempDir()), config, request, false)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Rules []string `yaml:"rules"`
	}
	if err := yaml.Unmarshal(output, &document); err != nil {
		t.Fatal(err)
	}
	firstMatch := func(host string) (string, int) {
		for index, rule := range document.Rules {
			parts := strings.Split(rule, ",")
			if len(parts) < 3 {
				continue
			}
			kind, matcher := parts[0], parts[1]
			if kind == "DOMAIN" && host == matcher ||
				kind == "DOMAIN-SUFFIX" && (host == matcher || strings.HasSuffix(host, "."+matcher)) ||
				kind == "DOMAIN-KEYWORD" && strings.Contains(host, matcher) {
				return parts[2], index
			}
		}
		return "", -1
	}
	broad, broadIndex := firstMatch("googleapis.com")
	if broad != "DIRECT" || broadIndex < 0 {
		t.Fatal("broad Google rule was not preserved")
	}
	for _, host := range []string{
		"antigravity.google",
		"antigravity.google.com",
		"antigravity.googleapis.com",
		"antigravity-pa.googleapis.com",
		"cloudaicompanion.googleapis.com",
		"cloudcode-pa.googleapis.com",
		"daily-cloudcode-pa.googleapis.com",
		"daily-cloudcode-pa.sandbox.googleapis.com",
		"antigravity-unleash.goog",
	} {
		target, index := firstMatch(host)
		if target != aiGroup || index < 0 || index > broadIndex {
			t.Fatalf("%s matched %q at %d; want %s before broad Google rule at %d", host, target, index, aiGroup, broadIndex)
		}
	}
	if len(preview.Groups) == 0 {
		t.Fatal("generation preview lost proxy groups")
	}
}

func TestAIRulesEndpointListsAntigravityEndpoints(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := writeConfig(path, defaultConfig()); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	newHandler(path).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/rules/ai", nil))
	if response.Code != http.StatusOK {
		t.Fatal("AI rules endpoint failed", response.Code)
	}
	var body struct {
		Rules []string `json:"rules"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(body.Rules, "\n")
	for _, rule := range []string{
		"DOMAIN-SUFFIX,antigravity.google",
		"DOMAIN,antigravity.googleapis.com",
		"DOMAIN,cloudcode-pa.googleapis.com",
		"DOMAIN,daily-cloudcode-pa.sandbox.googleapis.com",
	} {
		if !strings.Contains(joined, rule) {
			t.Fatalf("AI rules endpoint omitted %s", rule)
		}
	}
}
