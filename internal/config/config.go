package config

import (
	"fmt"
	"strings"

	"go.yaml.in/yaml/v3"
)

type Config struct {
	DataDir         string   `yaml:"data_dir"`
	Nameserver      string   `yaml:"nameserver"`
	HostmasterEmail string   `yaml:"hostmaster_email"`
	RPZs            []RPZ    `yaml:"rpzs"`
	DryRun          bool     `yaml:"dry_run"`
	AlsoNotify      HostList `yaml:"also_notify"`
}

// HostList is one or more host[:port] addresses, written as a YAML list or, for
// compatibility, a single string (comma-separated values are split).
type HostList []string

func (h *HostList) UnmarshalYAML(n *yaml.Node) error {
	switch n.Kind {
	case yaml.ScalarNode:
		var s string
		if err := n.Decode(&s); err != nil {
			return err
		}
		*h = nil
		for _, part := range strings.Split(s, ",") {
			if part = strings.TrimSpace(part); part != "" {
				*h = append(*h, part)
			}
		}
		return nil
	case yaml.SequenceNode:
		var list []string
		if err := n.Decode(&list); err != nil {
			return err
		}
		*h = list
		return nil
	default:
		return fmt.Errorf("line %d: expected a host or a list of hosts", n.Line)
	}
}

type RPZType string

const (
	RPZTypeRemote RPZType = "managed"
	RPZTypeStatic RPZType = "static"
)

type RPZ struct {
	Name string `yaml:"name"`

	Type string `yaml:"type"`

	ReloadSchedule string `yaml:"reload_schedule"`
	URL            string `yaml:"url"`
	FetchOnStart   bool   `yaml:"fetch_on_start"`

	// MinRules is the fewest rules a downloaded list may have; a smaller one (an
	// empty or truncated download) is rejected and the loaded zone is kept.
	MinRules int `yaml:"min_rules"`
	// MaxShrinkPercent rejects a download with this many percent fewer rules than
	// the last good one (default 50; 100 turns the check off).
	MaxShrinkPercent int `yaml:"max_shrink_percent"`

	Rules       []RPZRule `yaml:"rules"`
	TTL         int       `yaml:"ttl"`
	Refresh     int       `yaml:"refresh"`
	Retry       int       `yaml:"retry"`
	Expire      int       `yaml:"expire"`
	NegativeTTL int       `yaml:"negative_ttl"`
}

type RPZAction string

const (
	ActionNXDOMAIN RPZAction = "."
	ActionNODATA   RPZAction = "*."
	ActionPassthru RPZAction = "rpz-passthru."
	ActionDrop     RPZAction = "rpz-drop."
)

type RPZRule struct {
	Trigger           string    `yaml:"trigger"`
	Action            RPZAction `yaml:"action"`
	IncludeSubdomains bool      `yaml:"include_subdomains"`
}

func (c *Config) Validate() error {
	if c.DataDir == "" {
		return fmt.Errorf("data_dir is required")
	}

	if strings.TrimSpace(c.Nameserver) == "" {
		return fmt.Errorf("nameserver is required (the zones' SOA and NS name)")
	}

	if strings.TrimSpace(c.HostmasterEmail) == "" {
		return fmt.Errorf("hostmaster_email is required")
	}

	if len(c.RPZs) == 0 {
		return fmt.Errorf("rpzs is required")
	}

	for _, rpz := range c.RPZs {
		if rpz.Name == "" {
			return fmt.Errorf("rpz name is required")
		}
		if rpz.Type == "" {
			return fmt.Errorf("rpz type is required")
		}
		if rpz.Type != string(RPZTypeRemote) && rpz.Type != string(RPZTypeStatic) {
			return fmt.Errorf("rpz type must be 'managed' or 'static'")
		}
		if rpz.Type == string(RPZTypeRemote) && rpz.URL == "" {
			return fmt.Errorf("rpz url is required for managed zones")
		}
		if rpz.Type == string(RPZTypeStatic) && len(rpz.Rules) == 0 {
			return fmt.Errorf("rpz rules is required for static zones")
		}
		if rpz.TTL == 0 && rpz.Type == string(RPZTypeStatic) {
			return fmt.Errorf("rpz ttl is required for static zones")
		}
		if rpz.MinRules < 0 {
			return fmt.Errorf("rpz %s: min_rules must not be negative", rpz.Name)
		}
		if rpz.MaxShrinkPercent < 0 || rpz.MaxShrinkPercent > 100 {
			return fmt.Errorf("rpz %s: max_shrink_percent must be 0-100", rpz.Name)
		}
	}

	if len(c.AlsoNotify) == 0 {
		return fmt.Errorf("also_notify is required")
	}
	for _, h := range c.AlsoNotify {
		if strings.TrimSpace(h) == "" {
			return fmt.Errorf("also_notify has an empty host")
		}
	}

	return nil
}
