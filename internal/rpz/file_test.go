package rpz

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/davidseybold/rpz-loader/internal/config"
)

func opts(t *testing.T) Opts {
	t.Helper()
	return Opts{
		Filename:        filepath.Join(t.TempDir(), "rpz.example.zone"),
		ZoneName:        "rpz.example",
		Nameserver:      "ns1.example.internal",
		HostmasterEmail: "hostmaster@example.internal",
		TTL:             30, Refresh: 3600, Retry: 600, Expire: 604800, NegativeTTL: 30,
	}
}

func serve(t *testing.T, contentType, body string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

const upstream = `$TTL 300
@ IN SOA ns.upstream. hostmaster.upstream. 1 3600 600 86400 300
@ IN NS ns.upstream.
; a comment
ads.example CNAME .
*.ads.example CNAME .
tracker.example CNAME .
`

func TestFetchZoneFile(t *testing.T) {
	o := opts(t)
	n, err := FetchZoneFile(o, serve(t, "text/plain", upstream), Guard{MinRules: 1, MaxShrinkPercent: 50})
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("rules = %d, want 3", n)
	}
	b, err := os.ReadFile(o.Filename)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{"$ORIGIN rpz.example.\n", "@ IN SOA ns1.example.internal. hostmaster.example.internal.", "@ IN NS ns1.example.internal.\n", "tracker.example CNAME .\n"} {
		if !strings.Contains(s, want) {
			t.Errorf("zone file lacks %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "upstream") {
		t.Errorf("upstream SOA/NS should be replaced:\n%s", s)
	}
	if c, err := CountRules(o.Filename); err != nil || c != 3 {
		t.Errorf("CountRules = %d, %v; want 3", c, err)
	}
}

// A rejected download leaves the current zone file exactly as it was.
func TestFetchZoneFileRejectsBadDownloads(t *testing.T) {
	for name, c := range map[string]struct {
		contentType, body string
		guard             Guard
		want              string
	}{
		"html page":    {"text/html; charset=utf-8", "<html>rate limited</html>", Guard{MinRules: 1}, "HTML page"},
		"empty list":   {"text/plain", "$TTL 300\n@ IN SOA a. b. 1 1 1 1 1\n", Guard{MinRules: 1}, "fewer than min_rules"},
		"shrunk a lot": {"text/plain", "a.example CNAME .\n", Guard{MinRules: 1, MaxShrinkPercent: 50, Previous: 10}, "more than max_shrink_percent"},
	} {
		t.Run(name, func(t *testing.T) {
			o := opts(t)
			if err := os.WriteFile(o.Filename, []byte("current\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := FetchZoneFile(o, serve(t, c.contentType, c.body), c.guard)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("want error %q, got %v", c.want, err)
			}
			if b, _ := os.ReadFile(o.Filename); string(b) != "current\n" {
				t.Errorf("zone file changed to %q", b)
			}
			if left, _ := filepath.Glob(filepath.Join(filepath.Dir(o.Filename), ".*")); len(left) != 0 {
				t.Errorf("temporary files left behind: %v", left)
			}
		})
	}
}

func TestFetchZoneFileHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	if _, err := FetchZoneFile(opts(t), srv.URL, Guard{}); err == nil || !strings.Contains(err.Error(), "503") {
		t.Errorf("want a 503 error, got %v", err)
	}
}

func TestGuard(t *testing.T) {
	for _, c := range []struct {
		g    Guard
		n    int
		fail bool
	}{
		{Guard{MinRules: 1}, 0, true},
		{Guard{MinRules: 1}, 1, false},
		{Guard{MinRules: 1, MaxShrinkPercent: 50, Previous: 100}, 50, false},
		{Guard{MinRules: 1, MaxShrinkPercent: 50, Previous: 100}, 49, true},
		{Guard{MinRules: 1, MaxShrinkPercent: 100, Previous: 100}, 1, false}, // check off
		{Guard{MinRules: 1, MaxShrinkPercent: 50}, 5, false},                 // no baseline
	} {
		if err := c.g.Check(c.n); (err != nil) != c.fail {
			t.Errorf("%+v Check(%d) = %v, want fail=%v", c.g, c.n, err, c.fail)
		}
	}
}

func TestWriteZoneFileFromRules(t *testing.T) {
	o := opts(t)
	n, err := WriteZoneFileFromRules(o, []config.RPZRule{
		{Trigger: "a.example", Action: config.ActionNXDOMAIN, IncludeSubdomains: true},
		{Trigger: "b.example", Action: config.ActionPassthru},
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("rules = %d, want 3 (a, *.a, b)", n)
	}
	b, _ := os.ReadFile(o.Filename)
	for _, want := range []string{"a.example CNAME .\n", "*.a.example CNAME .\n", "b.example CNAME rpz-passthru.\n"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("zone file lacks %q", want)
		}
	}
}

func TestCountRulesMissingFile(t *testing.T) {
	if n, err := CountRules(filepath.Join(t.TempDir(), "nope.zone")); n != 0 || err != nil {
		t.Errorf("CountRules(missing) = %d, %v", n, err)
	}
}
