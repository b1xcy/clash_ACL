package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

type GenerationRequest struct {
	URLs        []string           `json:"urls"`
	Aggregate   *bool              `json:"aggregate"`
	DirectLinks string             `json:"direct_links"`
	Policy      *NodePolicyOptions `json:"policy,omitempty"`
	AI          *AIPolicy          `json:"ai,omitempty"`
	Groups      *[]map[string]any  `json:"groups,omitempty"`
	GroupsJSON  *string            `json:"groups_json,omitempty"`
	Rules       RuleRequest        `json:"rules"`
}

type GenerationPreview struct {
	Nodes          []AggregatedNode `json:"nodes"`
	Groups         []map[string]any `json:"groups"`
	Sources        []SourceView     `json:"sources"`
	InputCount     int              `json:"input_count"`
	DuplicateCount int              `json:"duplicate_count"`
	RuleCount      int              `json:"rule_count"`
	SkippedRules   []SkippedRule    `json:"skipped_rules"`
	RulesVersion   string           `json:"rules_version,omitempty"`
}

func decodeGenerationRequest(reader io.Reader) (GenerationRequest, error) {
	var request *GenerationRequest
	decoder := json.NewDecoder(reader)
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil || request == nil {
		return GenerationRequest{}, errors.New("expected a generation request object")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return GenerationRequest{}, errors.New("expected one generation request object")
	}
	if request.GroupsJSON != nil {
		if request.Groups != nil {
			return GenerationRequest{}, errors.New("use either groups or groups_json")
		}
		data, _ := json.Marshal(ConversionOptions{GroupsJSON: *request.GroupsJSON})
		_, groups, err := decodeOptions(bytes.NewReader(data))
		if err != nil {
			return GenerationRequest{}, err
		}
		request.Groups, request.GroupsJSON = &groups, nil
	}
	return *request, nil
}

func generationSources(request GenerationRequest) ([]Source, error) {
	query := url.Values{"url": request.URLs}
	if request.Aggregate != nil {
		query.Set("aggregate", strconv.FormatBool(*request.Aggregate))
	}
	if len(request.URLs) == 0 {
		return nil, nil
	}
	return requestSources(query)
}

func generateConfig(ctx context.Context, manager *SourceManager, config Config, request GenerationRequest, refresh bool, libraries ...fs.FS) ([]byte, GenerationPreview, error) {
	preview := GenerationPreview{Sources: []SourceView{}, SkippedRules: []SkippedRule{}}
	sources, err := generationSources(request)
	if err != nil {
		return nil, preview, err
	}
	flights := []*sourceFlight{}
	for _, source := range sources {
		if !source.Enabled {
			continue
		}
		cache, err := manager.readCache(source)
		if refresh || err != nil || cache.LastSuccess == nil {
			flights = append(flights, manager.startRefresh(source, config.FetchConcurrency))
		}
	}
	attempted := map[string]SourceView{}
	for _, flight := range flights {
		select {
		case <-ctx.Done():
			return nil, preview, errors.New("subscription generation request cancelled")
		case <-flight.done:
			attempted[flight.result.ID] = flight.result
		}
	}
	input := []AggregatedNode{}
	policy := NodePolicyOptions{RenameRules: config.RenameRules, RatePattern: config.RatePattern, SourceRates: map[string]SourceRate{}}
	if request.Policy != nil {
		policy = *request.Policy
		policy.SourceRates = map[string]SourceRate{}
		for key, value := range request.Policy.SourceRates {
			policy.SourceRates[key] = value
		}
	}
	for _, source := range sources {
		view := manager.view(source)
		if attempt, exists := attempted[source.ID]; exists {
			view = attempt
		}
		preview.Sources = append(preview.Sources, view)
		if !source.Enabled {
			continue
		}
		for _, saved := range config.Sources {
			if saved.URL == source.URL {
				if _, exists := policy.SourceRates[source.ID]; !exists {
					policy.SourceRates[source.ID] = SourceRate{Pattern: saved.RatePattern, Default: saved.DefaultRate}
				}
				break
			}
		}
		cache, err := manager.readCache(source)
		if err != nil || cache.LastSuccess == nil || len(cache.Proxies) == 0 {
			return nil, preview, errors.New("an enabled subscription has no successful node cache")
		}
		for _, proxy := range cache.Proxies {
			name, _ := proxy["name"].(string)
			input = append(input, AggregatedNode{Proxy: proxy, Origins: []NodeOrigin{{SourceID: source.ID, SourceName: source.Name, OriginalName: name}}})
		}
	}
	for _, line := range parseURILines(request.DirectLinks) {
		if line.Error != "" {
			return nil, preview, errors.New("direct links contain invalid lines; confirm valid nodes first")
		}
		input = append(input, AggregatedNode{Proxy: line.Proxy, Origins: []NodeOrigin{{SourceID: "direct-links", SourceName: "直接节点", OriginalName: line.Name, Line: line.Line}}})
	}
	for _, node := range config.SelfHostedNodes {
		name, _ := node.Proxy["name"].(string)
		input = append(input, AggregatedNode{Proxy: node.Proxy, Origins: []NodeOrigin{{SourceID: "self:" + node.ID, SourceName: "自建节点", OriginalName: name}}})
	}
	if len(input) == 0 {
		return nil, preview, errors.New("generation requires at least one node")
	}
	preview.InputCount = len(input)
	nodes, duplicates, err := aggregateNodes(input)
	if err != nil {
		return nil, preview, err
	}
	preview.DuplicateCount = duplicates
	nodes, err = applyNodePolicy(nodes, policy)
	if err != nil {
		return nil, preview, err
	}
	return renderConfig(config, request, nodes, preview, libraries...)
}

func renderConfig(config Config, request GenerationRequest, nodes []AggregatedNode, preview GenerationPreview, libraries ...fs.FS) ([]byte, GenerationPreview, error) {
	for index := range nodes {
		nodes[index].Name = flagNodeName(nodes[index].Name, nodes[index].Origins)
	}
	customGroups := config.Groups
	if request.Groups != nil {
		customGroups = *request.Groups
	}
	ruleRequest := request.Rules
	if ruleRequest.Preset == "" {
		ruleRequest.Preset = "acl4ssr-online"
	}
	_, templateNames, _, err := loadRulePreset(ruleRequest.Preset, libraries...)
	if ruleRequest.Preset == "none" {
		err = nil
	}
	if err != nil {
		return nil, preview, err
	}
	names := append([]string{mainGroup, lowGroup, standardGroup, highGroup, aiGroup, aiChainGroup, selfGroup, telegramGroup}, templateNames...)
	for _, group := range customGroups {
		name, _ := group["name"].(string)
		names = append(names, name)
	}
	resolveNodeNames(nodes, names...)
	ai := config.AI
	if request.AI != nil {
		ai = *request.AI
	}
	aiNode := ""
	if ai.NodeID != "" {
		id := ai.NodeID
		for _, self := range config.SelfHostedNodes {
			if self.ID != ai.NodeID {
				continue
			}
			var err error
			id, err = nodeIdentity(self.Proxy)
			if err != nil {
				return nil, preview, err
			}
			break
		}
		for _, node := range nodes {
			if node.ID != id {
				continue
			}
			if dialer, exists := node.Proxy["dialer-proxy"]; exists && (!ai.Chain || dialer != aiChainGroup) {
				return nil, preview, errors.New("AI node has an existing dialer-proxy; configure its chain using the AI chain option")
			}
			server, _ := node.Proxy["server"].(string)
			port, portErr := strconv.Atoi(fmt.Sprint(node.Proxy["port"]))
			kind := node.Type
			if server == "" || portErr != nil || port < 1 || port > 65535 || containsString([]string{"direct", "reject", "dns", "compatible"}, strings.ToLower(kind)) {
				return nil, preview, errors.New("AI fixed node requires a proxy server and valid port")
			}
			aiNode = node.Name
			if ai.Chain {
				node.Proxy["dialer-proxy"] = aiChainGroup
			}
			break
		}
		if aiNode == "" {
			return nil, preview, errors.New("selected AI node is not available; import it again or select another node")
		}
	} else if len(ai.Domains) != 0 {
		return nil, preview, errors.New("AI domains require a selected fixed node")
	}
	if ai.Chain && aiNode == "" {
		return nil, preview, errors.New("AI chain requires a selected exit node")
	}
	groups, err := buildGroups(ruleRequest.Preset, customGroups, nodes, aiNode, ai.Chain, libraries...)
	if err != nil {
		return nil, preview, err
	}
	ruleRequest.Targets = nil
	actualTargets := map[string]bool{"DIRECT": true, "REJECT": true, "REJECT-DROP": true, "PASS": true, "COMPATIBLE": true}
	for _, node := range nodes {
		ruleRequest.Targets = append(ruleRequest.Targets, node.Name)
		actualTargets[node.Name] = true
	}
	for _, group := range groups {
		name := group["name"].(string)
		ruleRequest.Targets = append(ruleRequest.Targets, name)
		actualTargets[name] = true
	}
	custom := append([]string(nil), ruleRequest.CustomRules...)
	custom = append(custom, config.Rules...)
	if actualTargets[selfGroup] {
		target := telegramGroup
		if actualTargets["📲 电报信息"] {
			target = "📲 电报信息"
		}
		data, err := fs.ReadFile(ruleLibrary(libraries), "rules/ACL4SSR/Clash/Telegram.list")
		if err != nil {
			return nil, preview, err
		}
		values, err := parseRulePayload(data, "classical", target)
		if err != nil {
			return nil, preview, err
		}
		custom = append(custom, values...)
	}
	if aiNode != "" || ruleRequest.Preset != "none" {
		target := mainGroup
		if actualTargets["💬 Ai平台"] {
			target = "💬 Ai平台"
		}
		if aiNode != "" {
			target = aiGroup
		}
		data, err := bundledRules.ReadFile("rules/AI/NetCoffee.list")
		if err != nil {
			return nil, preview, err
		}
		values, err := parseRulePayload(data, "classical", target)
		if err != nil {
			return nil, preview, err
		}
		if aiNode != "" {
			custom = append(values, custom...)
		} else {
			custom = append(custom, values...)
		}
	}
	if aiNode != "" {
		if target, exists := ruleRequest.PolicyMap["💬 Ai平台"]; exists && target != "💬 Ai平台" && target != aiGroup {
			return nil, preview, errors.New("bound AI template policy cannot be mapped to another exit")
		}
		aiRules := []string{}
		for _, file := range []string{"AI.list", "OpenAi.list"} {
			data, err := fs.ReadFile(ruleLibrary(libraries), "rules/ACL4SSR/Clash/Ruleset/"+file)
			if err != nil {
				return nil, preview, errors.New("cannot read bundled AI rules")
			}
			values, err := parseRulePayload(data, "classical", aiGroup)
			if err != nil {
				return nil, preview, err
			}
			for _, value := range values {
				if !strings.HasPrefix(value, "DOMAIN,") && !strings.HasPrefix(value, "DOMAIN-SUFFIX,") && !strings.HasPrefix(value, "DOMAIN-KEYWORD,") {
					return nil, preview, errors.New("bundled AI rules must only match domains")
				}
				aiRules = append(aiRules, value)
			}
		}
		for _, domain := range ai.Domains {
			domain = strings.ToLower(strings.TrimSpace(domain))
			if !validDomain(domain) {
				return nil, preview, errors.New("AI domains must be plain domain names")
			}
			aiRules = append(aiRules, "DOMAIN-SUFFIX,"+domain+","+aiGroup)
		}
		custom = append(aiRules, custom...)
	}
	ruleRequest.CustomRules = custom
	if ruleRequest.Fallback == "" && ruleRequest.Preset == "none" {
		customFallback := false
		for _, rule := range custom {
			_, _, final, err := normalizedRule(rule, "")
			customFallback = customFallback || (err == nil && final)
		}
		if !customFallback {
			ruleRequest.Fallback = mainGroup
		}
	}
	rules, err := compileRules(ruleRequest, libraries...)
	if err != nil {
		return nil, preview, err
	}
	for _, rule := range rules.Rules {
		_, target, _, err := normalizedRule(rule, "")
		if err != nil || !actualTargets[target] {
			return nil, preview, errors.New("generated rule references an unavailable group or node")
		}
	}
	document := map[string]any{"mixed-port": 7890, "allow-lan": false, "log-level": "info"}
	for key, value := range config.Base {
		if key == "proxy-providers" || key == "rule-providers" || key == "script" || key == "sub-rules" {
			return nil, preview, errors.New("base configuration cannot supply external providers or scripts")
		}
		document[key] = value
	}
	proxies := make([]map[string]any, len(nodes))
	for index, node := range nodes {
		proxies[index] = node.Proxy
	}
	document["mode"], document["proxies"], document["proxy-groups"], document["rules"] = "rule", proxies, groups, rules.Rules
	output, err := marshalMihomo(document)
	if err != nil {
		return nil, preview, err
	}
	preview.Nodes, preview.Groups, preview.RuleCount, preview.SkippedRules = nodes, groups, len(rules.Rules), rules.SkippedRules
	return output, preview, nil
}

func validDomain(domain string) bool {
	if len(domain) == 0 || len(domain) > 253 || !strings.Contains(domain, ".") {
		return false
	}
	for _, label := range strings.Split(domain, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, char := range label {
			if !(char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '-') {
				return false
			}
		}
	}
	return true
}

func marshalMihomo(document map[string]any) ([]byte, error) {
	data, err := json.Marshal(document)
	if err != nil {
		return nil, errors.New("generated configuration contains invalid values")
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, errors.New("cannot encode generated configuration")
	}
	var encode func(any) (*yaml.Node, error)
	encode = func(value any) (*yaml.Node, error) {
		node := &yaml.Node{}
		switch typed := value.(type) {
		case map[string]any:
			node.Kind, node.Tag = yaml.MappingNode, "!!map"
			keys := make([]string, 0, len(typed))
			for key := range typed {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				child, err := encode(typed[key])
				if err != nil {
					return nil, err
				}
				node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, child)
			}
		case []any:
			node.Kind, node.Tag = yaml.SequenceNode, "!!seq"
			for _, item := range typed {
				child, err := encode(item)
				if err != nil {
					return nil, err
				}
				node.Content = append(node.Content, child)
			}
		case json.Number:
			node.Kind, node.Tag, node.Value = yaml.ScalarNode, "!!int", typed.String()
			if strings.ContainsAny(node.Value, ".eE") {
				node.Tag = "!!float"
			}
		default:
			if err := node.Encode(value); err != nil {
				return nil, fmt.Errorf("cannot encode generated YAML value")
			}
		}
		return node, nil
	}
	node, err := encode(value)
	if err != nil {
		return nil, err
	}
	var output bytes.Buffer
	encoder := yaml.NewEncoder(&output)
	encoder.SetIndent(2)
	if err := encoder.Encode(node); err != nil {
		return nil, errors.New("cannot encode generated YAML")
	}
	if err := encoder.Close(); err != nil {
		return nil, errors.New("cannot finish generated YAML")
	}
	return output.Bytes(), nil
}
