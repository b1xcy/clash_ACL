package main

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func policyFixture(t *testing.T, names ...string) AggregatedNode {
	t.Helper()
	inputs := []AggregatedNode{}
	for _, name := range names {
		inputs = append(inputs, AggregatedNode{Proxy: aggregateFixture(name, "synthetic-password"), Origins: []NodeOrigin{{SourceID: "source", OriginalName: name}}})
	}
	nodes, _, err := aggregateNodes(inputs)
	if err != nil {
		t.Fatal(err)
	}
	return nodes[0]
}

func TestRateClassificationBeforeRenameAndStableIdentity(t *testing.T) {
	for _, test := range []struct {
		name, group string
		rate        *float64
	}{
		{"HK [倍率:0.5]", "low", ratePointer(0.5)},
		{"HK 1倍", "standard", ratePointer(1)},
		{"HK 2X", "high", ratePointer(2)},
		{"HK RATE=0.25", "low", ratePointer(0.25)},
		{"HK 0×", "low", ratePointer(0)},
		{"HK 443", "standard", nil},
	} {
		input := policyFixture(t, test.name)
		options := NodePolicyOptions{RenameRules: []RenameRule{{Match: ".*", Replacement: "Final", Regex: true}}}
		nodes, err := applyNodePolicy([]AggregatedNode{input}, options)
		if err != nil {
			t.Fatal(err)
		}
		node := nodes[0]
		if node.RateGroup != test.group || node.Name != "Final" || node.ID != input.ID || (node.Rate == nil) != (test.rate == nil) || (node.Rate != nil && *node.Rate != *test.rate) {
			t.Fatalf("wrong rate or identity: %#v", node)
		}
		if input.Name != test.name || input.Proxy["name"] != test.name || node.Origins[0].OriginalName != test.name {
			t.Fatal("policy changed source names")
		}
	}
}

func ratePointer(value float64) *float64 { return &value }

func TestRateConflictsOverridesAndSourceDefaults(t *testing.T) {
	input := policyFixture(t, "HK 0.5x", "HK 2x", "HK unknown")
	nodes, err := applyNodePolicy([]AggregatedNode{input}, NodePolicyOptions{SourceRates: map[string]SourceRate{"source": {Default: ratePointer(9)}}})
	if err != nil || *nodes[0].Rate != 2 || !nodes[0].RateConflict || nodes[0].RateSource != "name" {
		t.Fatal("conflicting named rates were hidden or source default overrode names")
	}
	nodes, err = applyNodePolicy([]AggregatedNode{input}, NodePolicyOptions{RateOverrides: map[string]float64{input.ID: 0.1}})
	if err != nil || *nodes[0].Rate != 0.1 || nodes[0].RateSource != "override" || nodes[0].RateGroup != "low" {
		t.Fatal("manual node override did not take priority")
	}
	unknown := policyFixture(t, "HK unknown")
	nodes, err = applyNodePolicy([]AggregatedNode{unknown}, NodePolicyOptions{SourceRates: map[string]SourceRate{"source": {Default: ratePointer(0.75)}}})
	if err != nil || *nodes[0].Rate != 0.75 || nodes[0].RateSource != "source_default" {
		t.Fatal("source default not applied")
	}
	custom := policyFixture(t, "HK cost=0.25")
	nodes, err = applyNodePolicy([]AggregatedNode{custom}, NodePolicyOptions{RatePattern: `cost=([0-9.]+)`})
	if err != nil || *nodes[0].Rate != 0.25 {
		t.Fatal("custom rate pattern not applied")
	}
}

func TestRenamingScopeOrderAndCollisionResolution(t *testing.T) {
	one := policyFixture(t, "HK_01")
	two := policyFixture(t, "HK_02")
	two.Proxy["password"] = "another-password"
	id, _ := nodeIdentity(two.Proxy)
	two.ID = id
	options := NodePolicyOptions{RenameRules: []RenameRule{{SourceID: "other", Match: "HK", Replacement: "Wrong"}, {Match: "HK", Replacement: "香港"}, {Match: `香港_(\d+)`, Replacement: "Same", Regex: true}}}
	nodes, err := applyNodePolicy([]AggregatedNode{one, two}, options)
	if err != nil || nodes[0].Name != "Same" || nodes[1].Name == "Same" || nodes[0].ID != one.ID || nodes[1].ID != two.ID {
		t.Fatal("rename order/scope or collision behavior failed")
	}
	if one.Name != "HK_01" || two.Name != "HK_02" {
		t.Fatal("rename modified input nodes")
	}
}

func TestInvalidNodePoliciesFailInsteadOfGuessing(t *testing.T) {
	input := policyFixture(t, "HK rate=no-number")
	for _, options := range []NodePolicyOptions{
		{RatePattern: "["}, {RatePattern: "no-capture"}, {RatePattern: `rate=(.*)`},
		{SourceRates: map[string]SourceRate{"source": {Default: ratePointer(-1)}}},
		{RateOverrides: map[string]float64{input.ID: math.Inf(1)}},
		{RateOverrides: map[string]float64{"missing": 1}},
		{RenameRules: []RenameRule{{Match: "[", Regex: true}}},
		{RenameRules: []RenameRule{{Match: ""}}},
		{RenameRules: []RenameRule{{Match: ".*", Replacement: "", Regex: true}}},
	} {
		if _, err := applyNodePolicy([]AggregatedNode{input}, options); err == nil {
			t.Fatal("invalid policy silently accepted")
		}
	}
}

func TestAggregationPolicyAPIClassifiesAndRenames(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := writeConfig(path, defaultConfig()); err != nil {
		t.Fatal(err)
	}
	body := `{"direct_links":"trojan://synthetic-secret@example.invalid:443#HK%200.5x\ntrojan://other-secret@example.invalid:443#Unknown","policy":{"rename_rules":[{"match":"HK","replacement":"香港"}]}}`
	response := httptest.NewRecorder()
	newHandler(path).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/aggregate/preview", strings.NewReader(body)))
	var result struct {
		Nodes  []AggregatedNode `json:"nodes"`
		Counts map[string]int   `json:"rate_counts"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || response.Code != http.StatusOK || result.Counts["low"] != 1 || result.Counts["standard"] != 1 || result.Counts["unknown"] != 1 || !strings.HasPrefix(result.Nodes[0].Name, "香港") {
		t.Fatalf("policy API failed: %s", response.Body.String())
	}
	if strings.Contains(response.Body.String(), "synthetic-secret") || strings.Contains(response.Body.String(), "example.invalid") {
		t.Fatal("policy preview exposed credentials")
	}
}

func TestRatePatternOptionsPersistAndRejectInvalidUpdates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := writeConfig(path, defaultConfig()); err != nil {
		t.Fatal(err)
	}
	handler := newHandler(path)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/api/options", strings.NewReader(`{"rate_pattern":"([0-9.]+)x","rename_rules":[{"match":"HK","replacement":"香港"}],"groups_json":"[]"}`)))
	if response.Code != http.StatusOK {
		t.Fatal("rate pattern options did not save")
	}
	for _, body := range []string{`{"rate_pattern":"["}`, `{"rate_pattern":"no-capture"}`, `{"rename_rules":[{"match":""}]}`} {
		response = httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/api/options", strings.NewReader(body)))
		if response.Code != http.StatusBadRequest {
			t.Fatal("invalid node policy settings saved")
		}
	}
	saved, err := readConfig(path)
	if err != nil || saved.RatePattern != "([0-9.]+)x" || len(saved.RenameRules) != 1 {
		t.Fatal("invalid update changed existing settings")
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/options", nil))
	var result struct {
		Options ConversionOptions `json:"options"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result.Options.RatePattern != saved.RatePattern {
		t.Fatal("saved rate pattern missing on reload")
	}
}
