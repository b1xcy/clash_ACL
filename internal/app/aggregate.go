package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

type NodeOrigin struct {
	SourceID     string `json:"source_id"`
	SourceName   string `json:"source_name"`
	OriginalName string `json:"original_name"`
	Line         int    `json:"line,omitempty"`
}

type AggregatedNode struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	Type         string         `json:"type"`
	Origins      []NodeOrigin   `json:"origins"`
	Proxy        map[string]any `json:"-"`
	Rate         *float64       `json:"rate,omitempty"`
	RateGroup    string         `json:"rate_group,omitempty"`
	RateSource   string         `json:"rate_source,omitempty"`
	RateConflict bool           `json:"rate_conflict,omitempty"`
	RateValues   []float64      `json:"rate_values,omitempty"`
}

func nodeIdentity(proxy map[string]any) (string, error) {
	fields := make(map[string]any, len(proxy))
	for key, value := range proxy {
		if key != "name" {
			fields[key] = value
		}
	}
	if port, ok := fields["port"].(string); ok {
		if value, err := strconv.Atoi(port); err == nil {
			fields["port"] = value
		}
	}
	data, err := json.Marshal(fields)
	if err != nil {
		return "", errors.New("node parameters cannot be encoded")
	}
	sum := sha256.Sum256(data)
	return "node-" + hex.EncodeToString(sum[:]), nil
}

func aggregateNodes(input []AggregatedNode) ([]AggregatedNode, int, error) {
	nodes := make([]AggregatedNode, 0, len(input))
	positions := make(map[string]int)
	for _, node := range input {
		id, err := nodeIdentity(node.Proxy)
		if err != nil {
			return nil, 0, err
		}
		if index, found := positions[id]; found {
			nodes[index].Origins = append(nodes[index].Origins, node.Origins...)
			continue
		}
		node.ID = id
		node.Name, _ = node.Proxy["name"].(string)
		node.Type, _ = node.Proxy["type"].(string)
		if node.Name == "" || node.Type == "" {
			return nil, 0, errors.New("node requires a name and type")
		}
		proxy := make(map[string]any, len(node.Proxy))
		for key, value := range node.Proxy {
			proxy[key] = value
		}
		node.Proxy = proxy
		positions[id] = len(nodes)
		nodes = append(nodes, node)
	}
	resolveNodeNames(nodes)
	return nodes, len(input) - len(nodes), nil
}

func resolveNodeNames(nodes []AggregatedNode, groupNames ...string) {
	reserved := map[string]bool{"DIRECT": true, "REJECT": true, "REJECT-DROP": true, "PASS": true, "COMPATIBLE": true, "GLOBAL": true}
	used := map[string]bool{"DIRECT": true, "REJECT": true, "REJECT-DROP": true, "PASS": true, "COMPATIBLE": true, "GLOBAL": true}
	for _, name := range groupNames {
		reserved[name], used[name] = true, true
	}
	for _, node := range nodes {
		reserved[node.Name] = true
	}
	for index := range nodes {
		node := &nodes[index]
		name := node.Name
		if used[name] {
			base := name + " [" + strings.TrimPrefix(node.ID, "node-")[:8] + "]"
			name = base
			for suffix := 2; used[name] || reserved[name]; suffix++ {
				name = fmt.Sprintf("%s-%d", base, suffix)
			}
		}
		node.Name = name
		node.Proxy["name"] = name
		used[name] = true
	}
}
