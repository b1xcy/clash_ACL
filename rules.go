package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

type RuleRequest struct {
	Preset string `json:"preset"`

	CustomRules []string          `json:"custom_rules"`
	PolicyMap   map[string]string `json:"policy_map"`
	Targets     []string          `json:"targets"`
	Fallback    string            `json:"fallback"`

	SkipUnsupported bool `json:"skip_unsupported"`
}

type RuleEntry struct {
	Rule   string `json:"rule"`
	Origin string `json:"origin"`
}

type RuleResult struct {
	TemplateGroups []string    `json:"template_groups"`
	Rules          []string    `json:"rules"`
	Entries        []RuleEntry `json:"entries"`
	CustomCount    int         `json:"custom_count"`
	ExternalCount  int         `json:"external_count"`
	DuplicateCount int         `json:"duplicate_count"`

	SkippedRules []SkippedRule `json:"skipped_rules"`
}

type SkippedRule struct {
	Ruleset int    `json:"ruleset"`
	Item    int    `json:"item"`
	Type    string `json:"type"`
}

type externalRuleSet struct {
	target string
	url    string
	kind   string
	inline string
}

type unmappedPolicies struct{ targets []string }

func (err unmappedPolicies) Error() string {
	return "rule targets require mappings to existing nodes or groups"
}

func splitRule(value string) ([]string, error) {
	parts := []string{}
	depth, start := 0, 0
	for index, char := range value {
		switch char {
		case '(':
			depth++
		case ')':
			depth--
			if depth < 0 {
				return nil, errors.New("unbalanced rule expression")
			}
		case ',':
			if depth == 0 {
				parts = append(parts, strings.TrimSpace(value[start:index]))
				start = index + 1
			}
		}
	}
	if depth != 0 {
		return nil, errors.New("unbalanced rule expression")
	}
	parts = append(parts, strings.TrimSpace(value[start:]))
	return parts, nil
}

func normalizedRule(value, override string) (string, string, bool, error) {
	parts, err := splitRule(strings.TrimSpace(value))
	if err != nil || len(parts) < 1 {
		return "", "", false, errors.New("invalid routing rule")
	}
	parts[0] = strings.ToUpper(parts[0])
	if parts[0] == "FINAL" {
		parts[0] = "MATCH"
	}
	if parts[0] == "MATCH" {
		if override != "" && len(parts) == 1 {
			parts = append(parts, override)
		}
		if len(parts) != 2 || parts[1] == "" {
			return "", "", false, errors.New("invalid fallback rule")
		}
		if override != "" {
			parts[1] = override
		}
		return strings.Join(parts, ","), parts[1], true, nil
	}
	if !strings.Contains("|DOMAIN|DOMAIN-SUFFIX|DOMAIN-KEYWORD|DOMAIN-REGEX|IP-CIDR|IP-CIDR6|IP-SUFFIX|IP-ASN|GEOIP|GEOSITE|SRC-IP-CIDR|SRC-IP-SUFFIX|SRC-PORT|DST-PORT|NETWORK|DSCP|IN-PORT|IN-TYPE|IN-USER|IN-NAME|UID|PROCESS-NAME|PROCESS-PATH|PROCESS-NAME-WILDCARD|PROCESS-PATH-WILDCARD|PROCESS-NAME-REGEX|PROCESS-PATH-REGEX|AND|OR|NOT|", "|"+parts[0]+"|") {
		return "", "", false, errors.New("unsupported rule type; RULE-SET and SUB-RULE need provider support")
	}
	flag := ""
	if len(parts) > 2 && parts[len(parts)-1] == "no-resolve" {
		flag = "no-resolve"
		parts = parts[:len(parts)-1]
	}
	if override != "" && len(parts) == 2 {
		parts = append(parts, override)
	}
	if len(parts) != 3 || parts[1] == "" || parts[2] == "" {
		return "", "", false, errors.New("rule requires a matcher and target")
	}
	if override != "" {
		parts[2] = override
	}
	if strings.Contains(parts[0], "IP-CIDR") {
		if _, _, err := net.ParseCIDR(parts[1]); err != nil {
			return "", "", false, errors.New("invalid rule CIDR")
		}
	}
	target := parts[2]
	if flag != "" {
		parts = append(parts, flag)
	}
	return strings.Join(parts, ","), target, false, nil
}

func parseRuleConfig(data []byte) ([]externalRuleSet, error) {
	sets := []externalRuleSet{}
	section := ""
	for _, raw := range strings.Split(strings.TrimPrefix(string(data), "\ufeff"), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.ToLower(line[1 : len(line)-1])
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found || section != "custom" || strings.TrimSpace(key) != "ruleset" {
			continue
		}
		target, source, found := strings.Cut(strings.TrimSpace(value), ",")
		if !found || strings.TrimSpace(target) == "" || strings.TrimSpace(source) == "" {
			return nil, errors.New("invalid external ruleset declaration")
		}
		set := externalRuleSet{target: strings.TrimSpace(target), kind: "classical"}
		source = strings.TrimSpace(source)
		if strings.HasPrefix(source, "[]") {
			set.inline = strings.TrimPrefix(source, "[]")
			if _, _, _, err := normalizedRule(set.inline, "DIRECT"); err != nil {
				return nil, err
			}
		} else {
			for _, prefix := range []string{"clash-classic:", "clash-domain:", "clash-ipcidr:", "surge:"} {
				if strings.HasPrefix(source, prefix) {
					set.kind = strings.TrimSuffix(strings.TrimPrefix(prefix, "clash-"), ":")
					if set.kind == "classic" || set.kind == "surge" {
						set.kind = "classical"
					}
					source = strings.TrimPrefix(source, prefix)
					break
				}
			}
			if strings.Contains(source, ":") || strings.Contains(source, "\\") || strings.HasPrefix(source, "/") {
				return nil, errors.New("bundled templates must use relative local rule files")
			}
			set.url = source
		}
		sets = append(sets, set)
	}
	if len(sets) == 0 {
		return nil, errors.New("external config must contain [custom] ruleset declarations")
	}
	return sets, nil
}

func parseRulePayload(data []byte, kind, target string) ([]string, error) {
	rules, _, err := parseRulePayloadCompatible(data, kind, target, false)
	return rules, err
}

func parseRulePayloadCompatible(data []byte, kind, target string, skip bool) ([]string, []SkippedRule, error) {
	text := strings.TrimPrefix(string(data), "\ufeff")
	var values []string
	first := ""
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			first = line
			break
		}
	}
	if strings.HasPrefix(first, "payload:") {
		var document struct {
			Payload []string `yaml:"payload"`
		}
		decoder := yaml.NewDecoder(strings.NewReader(text))
		if err := decoder.Decode(&document); err != nil {
			return nil, nil, errors.New("invalid rule payload YAML")
		}
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			return nil, nil, errors.New("expected one rule payload YAML document")
		}
		values = document.Payload
	} else {
		values = strings.Split(text, "\n")
	}
	rules := []string{}
	skipped := []SkippedRule{}
	for index, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || strings.HasPrefix(value, "#") || strings.HasPrefix(value, ";") || strings.HasPrefix(value, "//") {
			continue
		}
		if kind == "classical" {
			ruleType, matcher, found := strings.Cut(value, ",")
			ruleType = strings.ToUpper(strings.TrimSpace(ruleType))
			if ruleType == "URL-REGEX" || ruleType == "USER-AGENT" {
				if !skip || !found || strings.TrimSpace(matcher) == "" {
					return nil, nil, errors.New("external URL-REGEX / USER-AGENT rules require skip_unsupported=true for Mihomo")
				}
				skipped = append(skipped, SkippedRule{Item: index + 1, Type: ruleType})
				continue
			}
		}
		switch kind {
		case "domain":
			if strings.HasPrefix(value, "+.") {
				value = "DOMAIN-SUFFIX," + strings.TrimPrefix(value, "+.")
			} else if strings.HasPrefix(value, ".") || strings.ContainsAny(value, "*,()") || strings.ContainsAny(value, " \t") {
				return nil, nil, errors.New("unsupported domain wildcard in external rules")
			} else {
				value = "DOMAIN," + value
			}
		case "ipcidr":
			value = "IP-CIDR," + value
			if strings.Contains(strings.TrimPrefix(value, "IP-CIDR,"), ":") {
				value = strings.Replace(value, "IP-CIDR,", "IP-CIDR6,", 1)
			}
		}
		rule, _, _, err := normalizedRule(value, target)
		if err != nil {
			return nil, nil, err
		}
		rules = append(rules, rule)
	}
	if len(rules) == 0 && len(skipped) == 0 {
		return nil, nil, errors.New("external ruleset contains no rules")
	}
	return rules, skipped, nil
}

func compileRules(request RuleRequest) (RuleResult, error) {
	result := RuleResult{Rules: []string{}, Entries: []RuleEntry{}, SkippedRules: []SkippedRule{}, TemplateGroups: []string{}}
	targets := map[string]bool{"DIRECT": true, "REJECT": true, "REJECT-DROP": true, "PASS": true, "COMPATIBLE": true}
	for _, target := range request.Targets {
		if target != "" && !strings.ContainsAny(target, ",\r\n") {
			targets[target] = true
		}
	}
	sets := []externalRuleSet{}
	localBase := ""
	if request.Preset != "" && request.Preset != "none" {
		var err error
		sets, result.TemplateGroups, localBase, err = loadRulePreset(request.Preset)
		if err != nil {
			return result, err
		}
		for _, name := range result.TemplateGroups {
			targets[name] = true
		}
	}

	unmapped := map[string]bool{}
	for index := range sets {
		if mapped, exists := request.PolicyMap[sets[index].target]; exists {
			sets[index].target = mapped
		}
		if !targets[sets[index].target] {
			unmapped[sets[index].target] = true
		}
	}
	if len(unmapped) != 0 {
		names := []string{}
		for name := range unmapped {
			names = append(names, name)
		}
		sort.Strings(names)
		return result, unmappedPolicies{targets: names}
	}

	external := make([][]string, len(sets))
	for index, set := range sets {
		if set.inline != "" {
			external[index] = []string{set.inline}
			continue
		}
		data, err := readBundledRule(localBase, set.url)
		if err != nil {
			return result, err
		}
		var skipped []SkippedRule
		external[index], skipped, err = parseRulePayloadCompatible(data, set.kind, set.target, request.SkipUnsupported)
		if err != nil {
			return result, fmt.Errorf("local ruleset %d: %w", index+1, err)
		}
		for _, entry := range skipped {
			entry.Ruleset = index + 1
			result.SkippedRules = append(result.SkippedRules, entry)
		}
	}
	seen := make(map[string]bool)
	fallback, customFallback := request.Fallback, ""
	externalFallback := ""
	appendRules := func(values []string, override, origin string) error {
		for index, value := range values {
			value = strings.TrimSpace(value)
			if value == "" || strings.HasPrefix(value, "#") || strings.HasPrefix(value, ";") {
				continue
			}
			rule, target, final, err := normalizedRule(value, override)
			if err != nil {
				return fmt.Errorf("%s rule %d: %w", origin, index+1, err)
			}
			if !targets[target] {
				return fmt.Errorf("%s rule %d references an unknown target", origin, index+1)
			}
			if final {
				if origin == "custom" {
					if customFallback != "" && customFallback != target {
						return errors.New("conflicting custom fallback rules")
					}
					customFallback = target
				} else if externalFallback == "" {
					externalFallback = target
				}
				continue
			}
			if seen[rule] {
				result.DuplicateCount++
				continue
			}
			seen[rule] = true
			result.Rules = append(result.Rules, rule)
			result.Entries = append(result.Entries, RuleEntry{Rule: rule, Origin: origin})
			if origin == "custom" {
				result.CustomCount++
			} else {
				result.ExternalCount++
			}
		}
		return nil
	}
	if err := appendRules(request.CustomRules, "", "custom"); err != nil {
		return result, err
	}
	for index, values := range external {
		if err := appendRules(values, sets[index].target, "external"); err != nil {
			return result, err
		}
	}
	if fallback == "" {
		fallback = customFallback
	}
	if fallback == "" {
		fallback = externalFallback
	}
	if fallback == "" {
		fallback = "DIRECT"
	}
	if !targets[fallback] {
		return result, errors.New("fallback references an unknown target")
	}
	result.Rules = append(result.Rules, "MATCH,"+fallback)
	result.Entries = append(result.Entries, RuleEntry{Rule: "MATCH," + fallback, Origin: "fallback"})
	return result, nil
}
