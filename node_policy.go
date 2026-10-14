package main

import (
	"errors"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type SourceRate struct {
	Pattern string   `json:"pattern"`
	Default *float64 `json:"default,omitempty"`
}

type NodePolicyOptions struct {
	RenameRules   []RenameRule          `json:"rename_rules"`
	RatePattern   string                `json:"rate_pattern"`
	SourceRates   map[string]SourceRate `json:"source_rates"`
	RateOverrides map[string]float64    `json:"rate_overrides"`
}

var defaultRatePatterns = []*regexp.Regexp{
	regexp.MustCompile(`倍率\s*[:：=]?\s*([0-9]+(?:\.[0-9]+)?)`),
	regexp.MustCompile(`(?i)\brate\s*[:=]?\s*([0-9]+(?:\.[0-9]+)?)`),
	regexp.MustCompile(`(?i)([0-9]+(?:\.[0-9]+)?)\s*(?:倍|x|×)`),
}

func ratePattern(value string) (*regexp.Regexp, error) {
	pattern, err := regexp.Compile(value)
	if err != nil || pattern.NumSubexp() < 1 {
		return nil, errors.New("rate pattern requires a valid regular expression with a numeric capture group")
	}
	return pattern, nil
}

func validRate(value float64) bool { return value >= 0 && !math.IsInf(value, 0) && !math.IsNaN(value) }

func recognizeRate(name string, patterns []*regexp.Regexp) (*float64, error) {
	for _, pattern := range patterns {
		match := pattern.FindStringSubmatch(name)
		if len(match) < 2 {
			continue
		}
		value, err := strconv.ParseFloat(match[1], 64)
		if err != nil || !validRate(value) {
			return nil, errors.New("rate capture must be a finite non-negative number")
		}
		return &value, nil
	}
	return nil, nil
}

func applyNodePolicy(input []AggregatedNode, options NodePolicyOptions) ([]AggregatedNode, error) {
	patterns := defaultRatePatterns
	if options.RatePattern != "" {
		pattern, err := ratePattern(options.RatePattern)
		if err != nil {
			return nil, err
		}
		patterns = []*regexp.Regexp{pattern}
	}
	sourcePatterns := map[string]*regexp.Regexp{}
	for id, source := range options.SourceRates {
		if source.Default != nil && !validRate(*source.Default) {
			return nil, errors.New("source default rate must be finite and non-negative")
		}
		if source.Pattern != "" {
			pattern, err := ratePattern(source.Pattern)
			if err != nil {
				return nil, err
			}
			sourcePatterns[id] = pattern
		}
	}
	renames := make([]*regexp.Regexp, len(options.RenameRules))
	for index, rule := range options.RenameRules {
		if rule.Match == "" {
			return nil, errors.New("rename match cannot be empty")
		}
		if rule.Regex {
			pattern, err := regexp.Compile(rule.Match)
			if err != nil {
				return nil, errors.New("invalid rename regular expression")
			}
			renames[index] = pattern
		}
	}
	ids := map[string]bool{}
	for _, node := range input {
		ids[node.ID] = true
	}
	for id, value := range options.RateOverrides {
		if !ids[id] || !validRate(value) {
			return nil, errors.New("rate override requires an existing node ID and a finite non-negative rate")
		}
	}
	nodes := append([]AggregatedNode(nil), input...)
	for index := range nodes {
		node := &nodes[index]
		proxy := make(map[string]any, len(node.Proxy))
		for key, value := range node.Proxy {
			proxy[key] = value
		}
		node.Proxy = proxy
		node.Rate = nil
		node.RateConflict = false
		node.RateValues = nil
		node.RateSource = "unknown"
		node.RateGroup = "standard"
		if value, exists := options.RateOverrides[node.ID]; exists {
			node.Rate = &value
			node.RateSource = "override"
		} else {
			recognized := map[float64]bool{}
			defaults := map[float64]bool{}
			for _, origin := range node.Origins {
				selected := patterns
				if pattern := sourcePatterns[origin.SourceID]; pattern != nil {
					selected = []*regexp.Regexp{pattern}
				}
				rate, err := recognizeRate(origin.OriginalName, selected)
				if err != nil {
					return nil, err
				}
				if rate != nil {
					recognized[*rate] = true
				}
				if source := options.SourceRates[origin.SourceID]; source.Default != nil {
					defaults[*source.Default] = true
				}
			}
			values := recognized
			if len(values) > 0 {
				node.RateSource = "name"
			} else {
				values = defaults
				if len(values) > 0 {
					node.RateSource = "source_default"
				}
			}
			if len(values) > 0 {
				for value := range values {
					node.RateValues = append(node.RateValues, value)
				}
				sort.Float64s(node.RateValues)
				value := node.RateValues[len(node.RateValues)-1]
				node.Rate = &value
				node.RateConflict = len(node.RateValues) > 1
			}
		}
		if node.Rate != nil {
			if *node.Rate < 1 {
				node.RateGroup = "low"
			} else if *node.Rate > 1 {
				node.RateGroup = "high"
			}
		}
		for ruleIndex, rule := range options.RenameRules {
			if rule.SourceID != "" {
				matches := false
				for _, origin := range node.Origins {
					if origin.SourceID == rule.SourceID {
						matches = true
						break
					}
				}
				if !matches {
					continue
				}
			}
			if rule.Regex {
				node.Name = renames[ruleIndex].ReplaceAllString(node.Name, rule.Replacement)
			} else {
				node.Name = strings.ReplaceAll(node.Name, rule.Match, rule.Replacement)
			}
		}
		if strings.TrimSpace(node.Name) == "" {
			return nil, errors.New("renaming produced an empty node name")
		}
		node.Proxy["name"] = node.Name
	}
	resolveNodeNames(nodes)
	return nodes, nil
}
