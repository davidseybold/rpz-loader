package config

import "fmt"

type Config struct {
	DataDir string `yaml:"data_dir"`
	RPZs    []RPZ  `yaml:"rpzs"`
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

	Rules []RPZRule
	TTL   int
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
	}

	return nil
}
