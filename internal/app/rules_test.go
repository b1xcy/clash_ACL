package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestBundledPresetsLoadOfflineAndKeepCustomPriority(t *testing.T) {
	for _, preset := range rulePresets {
		result, err := compileRules(RuleRequest{Preset: preset.ID, CustomRules: []string{"DOMAIN-SUFFIX,openai.com,Fixed", "MATCH,REJECT"}, Targets: []string{"Fixed"}, SkipUnsupported: true})
		if err != nil {
			t.Fatalf("%s: %v", preset.ID, err)
		}
		if result.ExternalCount < 1000 || result.Rules[0] != "DOMAIN-SUFFIX,openai.com,Fixed" || result.Rules[len(result.Rules)-1] != "MATCH,REJECT" || len(result.TemplateGroups) == 0 {
			t.Fatal("local rule priority or template groups missing")
		}
		finals := 0
		for _, rule := range result.Rules {
			if strings.HasPrefix(rule, "MATCH,") {
				finals++
			}
		}
		if finals != 1 {
			t.Fatal("imported FINAL stayed in the middle of rules")
		}
		sets, _, base, err := loadRulePreset(preset.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, set := range sets {
			if set.url != "" {
				if strings.Contains(set.url, "http") {
					t.Fatal("bundled rules reference a remote address")
				}
				if _, err := readBundledRule(base, set.url); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
}

func TestByteDanceDomesticSupplementAcrossPresets(t *testing.T) {
	for _, preset := range rulePresets {
		t.Run(preset.ID, func(t *testing.T) {
			result, err := compileRules(RuleRequest{Preset: preset.ID, Targets: []string{"Fixed"}, CustomRules: []string{"DOMAIN,forced.bytegecko.com,Fixed"}, SkipUnsupported: true})
			if err != nil {
				t.Fatal(err)
			}
			if result.Rules[0] != "DOMAIN,forced.bytegecko.com,Fixed" {
				t.Fatal("custom rule priority lost")
			}
			for _, host := range []string{"lf-normal-gr-sourcecdn.bytegecko.com", "lf-source.bytegeckoext.com", "perfops.byte-test.com", "www.douyin.com", "v.idouyinvod.com", "www.fqnovel.com", "www.feishu.cn", "api.volcengine.com"} {
				target := ""
				for _, rule := range result.Rules {
					parts := strings.Split(rule, ",")
					if len(parts) < 3 {
						continue
					}
					if (parts[0] == "DOMAIN" && host == parts[1]) || (parts[0] == "DOMAIN-SUFFIX" && (host == parts[1] || strings.HasSuffix(host, "."+parts[1]))) || (parts[0] == "DOMAIN-KEYWORD" && strings.Contains(host, parts[1])) {
						target = parts[2]
						break
					}
				}
				if target != "🎯 全球直连" {
					t.Fatalf("%s matched %q instead of domestic routing", host, target)
				}
			}
			firstSupplement, geoIP, ad := -1, -1, -1
			for index, entry := range result.Entries {
				if entry.Origin == "domestic-supplement" {
					if firstSupplement < 0 {
						firstSupplement = index
					}
					for _, overseas := range []string{"tiktok.com", "tiktokcdn.com", "ibytedtos.com", "byteoversea.com", "capcut.com", "larksuite.com", "coze.com", "trae.ai", "byteperf.com"} {
						if strings.Split(entry.Rule, ",")[1] == overseas {
							t.Fatalf("overseas service added to domestic supplement: %s", overseas)
						}
					}
				}
				if strings.HasPrefix(entry.Rule, "GEOIP,CN,") {
					geoIP = index
				}
				if strings.HasPrefix(entry.Rule, "DOMAIN-SUFFIX,ad.toutiao.com,") {
					ad = index
				}
			}
			if firstSupplement < 0 || geoIP < firstSupplement || (ad >= 0 && ad > firstSupplement) {
				t.Fatal("supplement must follow existing ad rules and precede China IP routing")
			}
		})
	}
}

func TestCustomRulesOrderAndFallback(t *testing.T) {
	result, err := compileRules(RuleRequest{Preset: "none", CustomRules: []string{"DOMAIN,a,Fixed", "MATCH,REJECT", "DOMAIN,b,DIRECT", "DOMAIN,a,Fixed"}, Targets: []string{"Fixed"}})
	if err != nil || !reflect.DeepEqual(result.Rules, []string{"DOMAIN,a,Fixed", "DOMAIN,b,DIRECT", "MATCH,REJECT"}) || result.DuplicateCount != 1 || result.Entries[0].Origin != "custom" || result.Entries[2].Origin != "fallback" {
		t.Fatal("custom priority or fallback extraction failed")
	}
	result, err = compileRules(RuleRequest{CustomRules: []string{"MATCH,REJECT"}, Fallback: "DIRECT"})
	if err != nil || result.Rules[0] != "MATCH,DIRECT" {
		t.Fatal("explicit fallback ignored")
	}
}

func TestRulePayloadFormats(t *testing.T) {
	for _, test := range []struct {
		text, kind string
		want       []string
	}{
		{"# header\npayload:\n - '+.example.com'\n - exact.invalid\n", "domain", []string{"DOMAIN-SUFFIX,example.com,Fixed", "DOMAIN,exact.invalid,Fixed"}},
		{"payload:\n - 192.0.2.0/24\n - '2001:db8::/32'\n", "ipcidr", []string{"IP-CIDR,192.0.2.0/24,Fixed", "IP-CIDR6,2001:db8::/32,Fixed"}},
		{"AND,((DOMAIN,example.com),(NETWORK,UDP))\nIP-CIDR,192.0.2.0/24,no-resolve", "classical", []string{"AND,((DOMAIN,example.com),(NETWORK,UDP)),Fixed", "IP-CIDR,192.0.2.0/24,Fixed,no-resolve"}},
	} {
		got, err := parseRulePayload([]byte(test.text), test.kind, "Fixed")
		if err != nil || !reflect.DeepEqual(got, test.want) {
			t.Fatalf("payload conversion failed: %#v %v", got, err)
		}
	}
	for _, text := range []string{"<html>failed</html>", "IP-CIDR,invalid", "RULE-SET,unbound", "AND,((DOMAIN,a),(NETWORK,UDP)", "payload: []", "payload: []\n---\npayload: []"} {
		if _, err := parseRulePayload([]byte(text), "classical", "DIRECT"); err == nil {
			t.Fatal("invalid rule payload accepted")
		}
	}
}

func TestForeignCompatibilityIsExplicitAndDoesNotSkipCustomRules(t *testing.T) {
	data := []byte("DOMAIN,example.com\nURL-REGEX,^https://example.com/\nUSER-AGENT,ExampleApp\n")
	if _, err := parseRulePayload(data, "classical", "DIRECT"); err == nil {
		t.Fatal("unsupported external rules silently skipped")
	}
	rules, skipped, err := parseRulePayloadCompatible(data, "classical", "DIRECT", true)
	if err != nil || len(rules) != 1 || len(skipped) != 2 || skipped[0].Item != 2 || skipped[0].Type != "URL-REGEX" {
		t.Fatal("compatibility diagnostics lost")
	}
	if _, _, err := parseRulePayloadCompatible([]byte("UNKNOWN,example.com"), "classical", "DIRECT", true); err == nil {
		t.Fatal("unknown types accepted")
	}
	if _, err := compileRules(RuleRequest{CustomRules: []string{"USER-AGENT,ExampleApp,DIRECT"}, SkipUnsupported: true}); err == nil {
		t.Fatal("custom unsupported rule silently skipped")
	}
}

func TestPresetMappingAndPathValidation(t *testing.T) {
	_, err := compileRules(RuleRequest{Preset: "acl4ssr-online", PolicyMap: map[string]string{"🚀 节点选择": "Missing"}, SkipUnsupported: true})
	if missing, ok := err.(unmappedPolicies); !ok || !reflect.DeepEqual(missing.targets, []string{"Missing"}) {
		t.Fatal("invalid policy mapping accepted")
	}
	for _, ref := range []string{"../../../../main.go", "/main.go", "missing.list"} {
		if _, err := readBundledRule("rules/ACL4SSR/Clash/config", ref); err == nil {
			t.Fatal("invalid local rule path accepted")
		}
	}
	if _, err := compileRules(RuleRequest{Preset: "unknown"}); err == nil {
		t.Fatal("unknown preset accepted")
	}
	if _, err := parseRuleConfig([]byte("[custom]\nruleset=DIRECT,https://example.invalid/list")); err == nil {
		t.Fatal("remote reference accepted in local template")
	}
}

func TestRulesAPILeavesConfigurationUntouched(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := writeConfig(path, defaultConfig()); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	handler := newHandler(path)
	for _, body := range []string{`null`, `{} {}`, `{"config_url":"https://example.invalid"}`, `{"custom_rules":["DOMAIN,a,Missing"]}`, `{"custom_rules":["MATCH,DIRECT","MATCH,REJECT"]}`} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/rules/preview", strings.NewReader(body)))
		if response.Code != http.StatusBadRequest {
			t.Fatal("invalid request accepted")
		}
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/rules/preview", strings.NewReader(`{"preset":"acl4ssr-online","custom_rules":["DOMAIN,example.com,DIRECT"],"skip_unsupported":true}`)))
	var result RuleResult
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || response.Code != http.StatusOK || result.ExternalCount < 1000 || result.Rules[0] != "DOMAIN,example.com,DIRECT" {
		t.Fatal("local rule API failed")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("preview changed saved configuration")
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/rules/presets", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "acl4ssr-online") || strings.Contains(response.Body.String(), "rules/ACL4SSR") {
		t.Fatal("preset endpoint missing data or exposing implementation paths")
	}
}
