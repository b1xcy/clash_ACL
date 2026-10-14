package app

import (
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	FetchConcurrency int              `json:"fetch_concurrency"`
	Sources          []Source         `json:"sources"`
	SelfHostedNodes  []SelfHostedNode `json:"self_hosted_nodes"`
	RenameRules      []RenameRule     `json:"rename_rules"`
	RatePattern      string           `json:"rate_pattern"`
	Groups           []map[string]any `json:"groups"`
	Rules            []string         `json:"rules"`
	AI               AIPolicy         `json:"ai"`
	Base             map[string]any   `json:"base"`
}

type Source struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	URL         string   `json:"url"`
	Enabled     bool     `json:"enabled"`
	DefaultRate *float64 `json:"default_rate,omitempty"`
	RatePattern string   `json:"rate_pattern,omitempty"`
}

type SelfHostedNode struct {
	ID    string         `json:"id"`
	Proxy map[string]any `json:"proxy"`
}

type RenameRule struct {
	SourceID    string `json:"source_id,omitempty"`
	Match       string `json:"match"`
	Replacement string `json:"replacement"`
	Regex       bool   `json:"regex"`
}

type AIPolicy struct {
	NodeID  string   `json:"node_id"`
	NodeIDs []string `json:"node_ids"`
	Domains []string `json:"domains"`
	Chain   bool     `json:"chain,omitempty"`
	TestURL string   `json:"test_url,omitempty"`
}

func (ai AIPolicy) selectedIDs() []string {
	if ai.NodeIDs != nil {
		return ai.NodeIDs
	}
	if ai.NodeID != "" {
		return []string{ai.NodeID}
	}
	return nil
}

func defaultConfig() Config {
	return Config{
		FetchConcurrency: 2,
		Sources:          []Source{},
		SelfHostedNodes:  []SelfHostedNode{},
		RenameRules:      []RenameRule{},
		Groups:           []map[string]any{},
		Rules:            []string{},
		AI:               AIPolicy{Domains: []string{}},
		Base:             map[string]any{},
	}
}

func decodeConfig(reader io.Reader) (Config, error) {
	initial := defaultConfig()
	config := &initial
	decoder := json.NewDecoder(reader)
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return Config{}, errors.New("invalid configuration JSON")
	}
	if config == nil {
		return Config{}, errors.New("expected a JSON configuration object")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return Config{}, errors.New("expected one JSON configuration object")
	}
	if err := validateConfig(*config); err != nil {
		return Config{}, err
	}
	return *config, nil
}

func validateConfig(config Config) error {
	if config.RatePattern != "" {
		if _, err := ratePattern(config.RatePattern); err != nil {
			return err
		}
	}
	if config.FetchConcurrency < 1 || config.FetchConcurrency > 16 {
		return errors.New("fetch_concurrency must be between 1 and 16")
	}
	ids := make(map[string]bool)
	for _, source := range config.Sources {
		if source.ID == "" || ids[source.ID] {
			return errors.New("sources require unique, non-empty IDs")
		}
		ids[source.ID] = true
		parsed, err := url.Parse(source.URL)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
			return errors.New("source URL must use HTTP or HTTPS")
		}
		if source.DefaultRate != nil && *source.DefaultRate < 0 {
			return errors.New("default_rate cannot be negative")
		}
	}
	nodeIDs := make(map[string]bool)
	for _, node := range config.SelfHostedNodes {
		if node.ID == "" || nodeIDs[node.ID] || len(node.Proxy) == 0 {
			return errors.New("self-hosted nodes require unique IDs and proxy fields")
		}
		nodeIDs[node.ID] = true
	}
	for _, id := range config.AI.selectedIDs() {
		if !nodeIDs[id] && !(strings.HasPrefix(id, "node-") && validSnapshotID(strings.TrimPrefix(id, "node-"))) {
			return errors.New("AI node_ids must reference saved nodes or valid imported node IDs")
		}
	}
	if config.AI.Chain && len(config.AI.selectedIDs()) == 0 {
		return errors.New("AI chain requires a selected exit node")
	}
	if config.AI.TestURL != "" {
		parsed, err := url.Parse(config.AI.TestURL)
		if err != nil || parsed.Hostname() == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
			return errors.New("AI test_url must be an HTTP or HTTPS URL")
		}
	}
	return nil
}

func readConfig(path string) (Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return Config{}, err
	}
	defer file.Close()
	return decodeConfig(file)
}

func writeConfig(path string, config Config) error {
	if err := validateConfig(config); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".config-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(config); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
