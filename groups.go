package main

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

const (
	mainGroup     = "🚀 节点选择"
	lowGroup      = "💰 低倍率"
	standardGroup = "⚖️ 标准倍率"
	highGroup     = "💎 高倍率"
	aiGroup       = "🤖 AI 固定出口"
)

func templateGroups(preset string, nodes []AggregatedNode) ([]map[string]any, error) {
	if preset == "" || preset == "none" {
		return nil, nil
	}
	var data []byte
	for _, entry := range rulePresets {
		if entry.ID == preset {
			data, _ = bundledRules.ReadFile(entry.file)
			break
		}
	}
	if data == nil {
		return nil, errors.New("unknown bundled rule preset")
	}
	groups := []map[string]any{}
	for _, raw := range strings.Split(string(data), "\n") {
		value, found := strings.CutPrefix(strings.TrimSpace(raw), "custom_proxy_group=")
		if !found {
			continue
		}
		parts := strings.Split(value, "`")
		if len(parts) < 3 {
			return nil, errors.New("invalid bundled proxy group")
		}
		group := map[string]any{"name": parts[0], "type": parts[1]}
		selectors := parts[2:]
		if parts[1] != "select" {
			if len(parts) < 5 {
				return nil, errors.New("invalid bundled automatic group")
			}
			selectors = parts[2 : len(parts)-2]
			group["url"] = parts[len(parts)-2]
			timing := strings.Split(parts[len(parts)-1], ",")
			interval, err := strconv.Atoi(timing[0])
			if err != nil || interval < 1 {
				return nil, errors.New("invalid bundled group interval")
			}
			group["interval"] = interval
			if len(timing) > 1 && timing[1] != "" {
				timeout, err := strconv.Atoi(timing[1])
				if err != nil || timeout < 1 {
					return nil, errors.New("invalid bundled group timeout")
				}
				group["timeout"] = timeout * 1000
			}
			if len(timing) > 2 && timing[2] != "" && parts[1] == "url-test" {
				tolerance, err := strconv.Atoi(timing[2])
				if err != nil || tolerance < 0 {
					return nil, errors.New("invalid bundled group tolerance")
				}
				group["tolerance"] = tolerance
			}
			if parts[1] == "load-balance" {
				group["strategy"] = "consistent-hashing"
			}
		}
		members := []string{}
		for _, selector := range selectors {
			if name, direct := strings.CutPrefix(selector, "[]"); direct {
				members = appendUnique(members, name)
				continue
			}
			pattern, err := regexp.Compile(selector)
			if err != nil {
				return nil, errors.New("unsupported bundled node selector")
			}
			for _, node := range nodes {
				if parts[1] != "select" && node.RateGroup == "high" {
					continue
				}
				if pattern.MatchString(node.Name) {
					members = appendUnique(members, node.Name)
				}
			}
		}
		group["proxies"] = members
		groups = append(groups, group)
	}
	return groups, nil
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func stringList(value any) ([]string, error) {
	if value == nil {
		return nil, nil
	}
	if values, ok := value.([]string); ok {
		return values, nil
	}
	values, ok := value.([]any)
	if !ok {
		return nil, errors.New("group members and selectors must be string arrays")
	}
	result := []string{}
	for _, item := range values {
		text, ok := item.(string)
		if !ok || text == "" {
			return nil, errors.New("group members and selectors must be non-empty strings")
		}
		result = append(result, text)
	}
	return result, nil
}

func userGroups(input []map[string]any, nodes []AggregatedNode) ([]map[string]any, error) {
	byID := map[string]string{}
	for _, node := range nodes {
		byID[node.ID] = node.Name
	}
	groups := []map[string]any{}
	seen := map[string]bool{}
	for _, original := range input {
		group := map[string]any{}
		for key, value := range original {
			group[key] = value
		}
		name, _ := group["name"].(string)
		if !validPolicyName(name) || seen[name] || name == lowGroup || name == standardGroup || name == highGroup || name == aiGroup {
			return nil, errors.New("custom group names must be unique and cannot override rate or AI groups")
		}
		seen[name] = true
		for _, key := range []string{"use", "include-all", "include-all-proxies", "include-all-providers"} {
			if _, exists := group[key]; exists {
				return nil, errors.New("custom groups must use explicit members or local node selectors")
			}
		}
		members, err := stringList(group["proxies"])
		if err != nil {
			return nil, err
		}
		members = append([]string(nil), members...)
		for index, member := range members {
			if resolved, exists := byID[member]; exists {
				members[index] = resolved
			} else if strings.HasPrefix(member, "node-") {
				return nil, errors.New("custom group references an unknown node ID")
			}
		}
		ids, err := stringList(group["node_ids"])
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			name, exists := byID[id]
			if !exists {
				return nil, errors.New("custom group references an unknown node ID")
			}
			members = appendUnique(members, name)
		}
		rates, err := stringList(group["rate_groups"])
		if err != nil {
			return nil, err
		}
		for _, rate := range rates {
			if rate != "low" && rate != "standard" && rate != "high" {
				return nil, errors.New("rate_groups must use low, standard or high")
			}
		}
		sources, err := stringList(group["source_ids"])
		if err != nil {
			return nil, err
		}
		filter, filterExists := group["filter"].(string)
		if _, exists := group["filter"]; exists && !filterExists {
			return nil, errors.New("group filter must be a regular expression string")
		}
		pattern, err := regexp.Compile(filter)
		if err != nil {
			return nil, errors.New("invalid custom group filter")
		}
		if filterExists || len(rates) > 0 || len(sources) > 0 {
			for _, node := range nodes {
				if !pattern.MatchString(node.Name) || (len(rates) > 0 && !containsString(rates, node.RateGroup)) {
					continue
				}
				matchesSource := len(sources) == 0
				for _, origin := range node.Origins {
					matchesSource = matchesSource || containsString(sources, origin.SourceID)
				}
				if matchesSource {
					members = appendUnique(members, node.Name)
				}
			}
		}
		for _, key := range []string{"node_ids", "rate_groups", "source_ids", "filter"} {
			delete(group, key)
		}
		group["proxies"] = members
		groups = append(groups, group)
	}
	return groups, nil
}

func containsString(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func validPolicyName(name string) bool {
	return strings.TrimSpace(name) != "" && !strings.ContainsAny(name, ",\r\n") && !containsString([]string{"DIRECT", "REJECT", "REJECT-DROP", "PASS", "COMPATIBLE", "GLOBAL"}, name)
}

func buildGroups(preset string, custom []map[string]any, nodes []AggregatedNode, aiNode string) ([]map[string]any, error) {
	groups := []map[string]any{}
	rateMembers := map[string][]string{"low": {}, "standard": {}, "high": {}}
	for _, node := range nodes {
		rateMembers[node.RateGroup] = append(rateMembers[node.RateGroup], node.Name)
	}
	mainMembers := []string{}
	for _, entry := range []struct{ rate, name string }{{"low", lowGroup}, {"standard", standardGroup}, {"high", highGroup}} {
		members := rateMembers[entry.rate]
		if len(members) == 0 {
			continue
		}
		group := map[string]any{"name": entry.name, "type": "select", "proxies": members}
		if entry.rate != "high" && !strings.Contains(preset, "noauto") {
			group["type"], group["url"], group["interval"], group["tolerance"] = "url-test", "https://www.gstatic.com/generate_204", 300, 50
		}
		groups = append(groups, group)
		mainMembers = append(mainMembers, entry.name)
	}
	groups = append(groups, map[string]any{"name": mainGroup, "type": "select", "proxies": mainMembers})
	if aiNode != "" {
		groups = append(groups, map[string]any{"name": aiGroup, "type": "select", "proxies": []string{aiNode}})
	}
	users, err := userGroups(custom, nodes)
	if err != nil {
		return nil, err
	}
	positions := map[string]int{}
	for index, group := range groups {
		positions[group["name"].(string)] = index
	}
	for _, group := range users {
		name := group["name"].(string)
		if aiNode != "" && name == "💬 Ai平台" {
			return nil, errors.New("custom groups cannot override the bound AI template group")
		}
		if index, exists := positions[name]; exists {
			groups[index] = group
		} else {
			positions[name] = len(groups)
			groups = append(groups, group)
		}
	}
	templates, err := templateGroups(preset, nodes)
	if err != nil {
		return nil, err
	}
	templateNames := map[string]bool{}
	for _, group := range templates {
		name := group["name"].(string)
		if _, exists := positions[name]; exists {
			continue
		}
		if aiNode != "" && name == "💬 Ai平台" {
			group = map[string]any{"name": name, "type": "select", "proxies": []string{aiGroup}}
		}
		templateNames[name] = true
		positions[name] = len(groups)
		groups = append(groups, group)
	}
	removed := map[string]bool{}
	for {
		changed := false
		for _, group := range groups {
			name := group["name"].(string)
			if !templateNames[name] || removed[name] {
				continue
			}
			members, _ := stringList(group["proxies"])
			kept := []string{}
			for _, member := range members {
				if !removed[member] {
					kept = append(kept, member)
				}
			}
			group["proxies"] = kept
			if len(kept) == 0 {
				removed[name], changed = true, true
			}
		}
		if !changed {
			break
		}
	}
	kept := []map[string]any{}
	for _, group := range groups {
		if !removed[group["name"].(string)] {
			kept = append(kept, group)
		}
	}
	if err := validateGroups(kept, nodes); err != nil {
		return nil, err
	}
	return kept, nil
}

func validateGroups(groups []map[string]any, nodes []AggregatedNode) error {
	targets := map[string]bool{"DIRECT": true, "REJECT": true, "REJECT-DROP": true, "PASS": true, "COMPATIBLE": true}
	graph := map[string][]string{}
	for _, node := range nodes {
		if !validPolicyName(node.Name) || targets[node.Name] {
			return errors.New("invalid or duplicate generated node name")
		}
		targets[node.Name] = true
		if dialer, exists := node.Proxy["dialer-proxy"]; exists {
			name, ok := dialer.(string)
			if !ok || name == "" {
				return errors.New("invalid node dialer-proxy")
			}
			graph[node.Name] = []string{name}
		}
	}
	for _, group := range groups {
		name, _ := group["name"].(string)
		if !validPolicyName(name) || targets[name] {
			return errors.New("invalid or duplicate generated group name")
		}
		targets[name] = true
		kind, _ := group["type"].(string)
		if !containsString([]string{"select", "url-test", "fallback", "load-balance"}, kind) {
			return errors.New("unsupported group type")
		}
		members, err := stringList(group["proxies"])
		if err != nil || len(members) == 0 {
			return fmt.Errorf("group %s has no matching nodes or members", name)
		}
		if kind != "select" {
			endpoint, _ := group["url"].(string)
			parsed, err := url.Parse(endpoint)
			if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
				return errors.New("automatic groups require an HTTP / HTTPS test URL")
			}
			interval, exists := group["interval"]
			if !exists {
				group["interval"] = 300
			} else if value, err := strconv.Atoi(fmt.Sprint(interval)); err != nil || value < 1 {
				return errors.New("automatic group interval must be a positive integer")
			}
		}
		graph[name] = members
	}
	states := map[string]int{}
	var visit func(string) error
	visit = func(name string) error {
		if !targets[name] {
			return errors.New("group or dialer-proxy references an unknown target")
		}
		if states[name] == 1 {
			return errors.New("proxy group or dialer-proxy dependency cycle")
		}
		if states[name] == 2 {
			return nil
		}
		states[name] = 1
		for _, member := range graph[name] {
			if err := visit(member); err != nil {
				return err
			}
		}
		states[name] = 2
		return nil
	}
	for name := range graph {
		if err := visit(name); err != nil {
			return err
		}
	}
	return nil
}
