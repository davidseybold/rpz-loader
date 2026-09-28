package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const base = `data_dir: /var/lib/rpz-loader
nameserver: ns1.example.internal
hostmaster_email: hostmaster@example.internal
rpzs:
  - name: rpz.example
    type: managed
    reload_schedule: "0 0 * * *"
    url: https://example.com/rpz
`

func load(t *testing.T, yaml string) (*Config, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	return Load(path)
}

func TestAlsoNotifyForms(t *testing.T) {
	for name, c := range map[string]struct {
		yaml string
		want HostList
	}{
		"single string":   {"also_notify: 192.168.5.21:53\n", HostList{"192.168.5.21:53"}},
		"comma separated": {"also_notify: 192.168.5.21:53, 192.168.5.22:53\n", HostList{"192.168.5.21:53", "192.168.5.22:53"}},
		"list":            {"also_notify: [192.168.5.21:53, 192.168.5.22:53]\n", HostList{"192.168.5.21:53", "192.168.5.22:53"}},
	} {
		t.Run(name, func(t *testing.T) {
			cfg, err := load(t, base+c.yaml)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(cfg.AlsoNotify, c.want) {
				t.Errorf("also_notify = %q, want %q", cfg.AlsoNotify, c.want)
			}
			if err := cfg.Validate(); err != nil {
				t.Errorf("Validate: %v", err)
			}
		})
	}
}

func TestUnknownKeysAreErrors(t *testing.T) {
	// The misspelling that once left every zone with nameserver ".".
	_, err := load(t, strings.Replace(base, "nameserver:", "namserver:", 1)+"also_notify: a:53\n")
	if err == nil || !strings.Contains(err.Error(), "namserver") {
		t.Errorf("want an error naming the unknown key, got %v", err)
	}
}

func TestDefaults(t *testing.T) {
	cfg, err := load(t, base+"also_notify: a:53\n")
	if err != nil {
		t.Fatal(err)
	}
	r := cfg.RPZs[0]
	if r.MinRules != 1 || r.MaxShrinkPercent != 50 || r.TTL != 30 {
		t.Errorf("defaults: min_rules=%d max_shrink_percent=%d ttl=%d", r.MinRules, r.MaxShrinkPercent, r.TTL)
	}
}

func TestValidate(t *testing.T) {
	for _, c := range []struct{ from, to, want string }{
		{"nameserver: ns1.example.internal\n", "", "nameserver is required"},
		{"hostmaster_email: hostmaster@example.internal\n", "", "hostmaster_email is required"},
		{"    url: https://example.com/rpz\n", "", "url is required"},
		{"    url: https://example.com/rpz\n", "    url: https://example.com/rpz\n    max_shrink_percent: 101\n", "max_shrink_percent must be 0-100"},
		{"    url: https://example.com/rpz\n", "    url: https://example.com/rpz\n    min_rules: -1\n", "min_rules must not be negative"},
	} {
		src := strings.Replace(base, c.from, c.to, 1) + "also_notify: a:53\n"
		cfg, err := load(t, src)
		if err != nil {
			t.Fatal(err)
		}
		if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q -> %q: want %q, got %v", c.from, c.to, c.want, err)
		}
	}
	cfg, err := load(t, base)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "also_notify is required") {
		t.Errorf("missing also_notify: got %v", err)
	}
}
