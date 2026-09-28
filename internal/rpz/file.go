package rpz

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/davidseybold/rpz-loader/internal/config"
)

type Opts struct {
	Filename string
	// Type is the zone's config type (managed or static), for metrics.
	Type string

	ZoneName        string
	Nameserver      string
	HostmasterEmail string
	TTL             int
	Refresh         int
	Retry           int
	Expire          int
	NegativeTTL     int
}

// Guard decides whether a freshly downloaded list may replace the current one.
type Guard struct {
	// MinRules is the fewest rules the new list may have.
	MinRules int
	// MaxShrinkPercent is how much smaller (in rules) than Previous it may be;
	// 100 turns the check off.
	MaxShrinkPercent int
	// Previous is the rule count of the last good list; 0 when unknown.
	Previous int
}

// Check returns an error when a list of n rules should not replace the current one.
func (g Guard) Check(n int) error {
	if n < g.MinRules {
		return fmt.Errorf("list has %d rules, fewer than min_rules %d (empty or truncated download?)", n, g.MinRules)
	}
	if g.Previous > 0 && g.MaxShrinkPercent < 100 {
		floor := g.Previous * (100 - g.MaxShrinkPercent) / 100
		if n < floor {
			return fmt.Errorf("list has %d rules, down from %d: more than max_shrink_percent %d%% smaller", n, g.Previous, g.MaxShrinkPercent)
		}
	}
	return nil
}

// WriteZoneFileFromRules renders a static zone and returns its rule count.
func WriteZoneFileFromRules(opts Opts, rules []config.RPZRule) (int, error) {
	return writeAtomically(opts.Filename, func(w io.Writer) (int, error) {
		if err := writeHeader(w, opts); err != nil {
			return 0, err
		}
		n := 0
		for _, rule := range rules {
			if _, err := fmt.Fprintf(w, "%s CNAME %s\n", rule.Trigger, rule.Action); err != nil {
				return 0, err
			}
			n++
			if rule.IncludeSubdomains {
				if _, err := fmt.Fprintf(w, "*.%s CNAME %s\n", rule.Trigger, rule.Action); err != nil {
					return 0, err
				}
				n++
			}
		}
		return n, nil
	}, nil)
}

// FetchZoneFile downloads a zone, replaces its header (SOA, NS, $TTL) with ours,
// and writes it to opts.Filename only if guard accepts it; otherwise the current
// file is left untouched. It returns the new rule count.
func FetchZoneFile(opts Opts, url string, guard Guard) (int, error) {
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return 0, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	if ct := resp.Header.Get("Content-Type"); strings.Contains(strings.ToLower(ct), "text/html") {
		return 0, fmt.Errorf("GET %s: got an HTML page (%s), not a zone file", url, ct)
	}

	return writeAtomically(opts.Filename, func(w io.Writer) (int, error) {
		if err := writeHeader(w, opts); err != nil {
			return 0, err
		}
		n := 0
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			trimmed := strings.TrimFunc(line, unicode.IsSpace)
			if trimmed == "" || isSoaLine(trimmed) || isNSLine(trimmed) || isTTLLine(trimmed) {
				continue
			}
			if _, err := fmt.Fprintln(w, line); err != nil {
				return 0, err
			}
			if isRule(trimmed) {
				n++
			}
		}
		if err := scanner.Err(); err != nil {
			return 0, fmt.Errorf("error reading response body: %v", err)
		}
		return n, nil
	}, guard.Check)
}

// CountRules counts the rules in an existing zone file (0 if it doesn't exist),
// so the shrink check has a baseline after a restart.
func CountRules(filename string) (int, error) {
	f, err := os.Open(filename)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer f.Close()

	n := 0
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		trimmed := strings.TrimFunc(scanner.Text(), unicode.IsSpace)
		if trimmed != "" && isRule(trimmed) && !isSoaLine(trimmed) && !isNSLine(trimmed) {
			n++
		}
	}
	return n, scanner.Err()
}

// writeAtomically writes via a temporary file in the same directory and renames it
// over filename, so a failed or rejected write never leaves a partial zone behind.
// check, if set, can reject the result (by rule count) before the rename.
func writeAtomically(filename string, write func(io.Writer) (int, error), check func(int) error) (int, error) {
	tmp, err := os.CreateTemp(filepath.Dir(filename), "."+filepath.Base(filename)+".*")
	if err != nil {
		return 0, err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename

	n, err := write(tmp)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return 0, err
	}
	if check != nil {
		if err := check(n); err != nil {
			return n, err
		}
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return 0, err
	}
	return n, os.Rename(tmp.Name(), filename)
}

// isRule reports whether a zone line is a record (not a comment or directive).
func isRule(line string) bool {
	return !strings.HasPrefix(line, ";") && !strings.HasPrefix(line, "$") && !strings.HasPrefix(line, "@")
}

func isSoaLine(line string) bool {
	return strings.Contains(line, "SOA")
}

func isNSLine(line string) bool {
	return strings.HasPrefix(line, "NS") || strings.HasPrefix(line, "@ IN NS") || strings.HasPrefix(line, "@ NS")
}

func isTTLLine(line string) bool {
	return strings.HasPrefix(line, "$TTL")
}

func writeHeader(w io.Writer, opts Opts) error {
	serial := time.Now().Unix()

	nameserver := addTrailingDot(opts.Nameserver)
	hostmasterEmail := addTrailingDot(hostmasterEmail(opts.HostmasterEmail))
	zoneName := addTrailingDot(opts.ZoneName)

	_, err := fmt.Fprintf(w, "$ORIGIN %s\n", zoneName)
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(w, "$TTL %d\n", opts.TTL)
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(w, "@ IN SOA %s %s %d %d %d %d %d\n", nameserver, hostmasterEmail, serial, opts.Refresh, opts.Retry, opts.Expire, opts.NegativeTTL)
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(w, "@ IN NS %s\n\n", nameserver)
	if err != nil {
		return err
	}

	return nil
}

func addTrailingDot(s string) string {
	if !strings.HasSuffix(s, ".") {
		return s + "."
	}
	return s
}

func hostmasterEmail(e string) string {
	return strings.ReplaceAll(e, "@", ".")
}
