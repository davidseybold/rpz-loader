package rpz

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode"

	"github.com/davidseybold/rpz-loader/internal/config"
)

type Opts struct {
	Filename string

	ZoneName        string
	Nameserver      string
	HostmasterEmail string
	TTL             int
}

func WriteZoneFileFromRules(opts Opts, rules []config.RPZRule) error {
	f, err := os.Create(opts.Filename)
	if err != nil {
		return err
	}
	defer f.Close()

	err = writeHeader(f, opts)
	if err != nil {
		return err
	}

	for _, rule := range rules {
		fmt.Fprintf(f, "%s CNAME %s\n", rule.Trigger, rule.Action)
		if rule.IncludeSubdomains {
			fmt.Fprintf(f, "*.%s CNAME %s\n", rule.Trigger, rule.Action)
		}
	}

	return nil
}

func FetchZoneFile(opts Opts, url string) error {
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}

	f, err := os.Create(opts.Filename)
	if err != nil {
		return err
	}
	defer f.Close()

	err = writeHeader(f, opts)
	if err != nil {
		return err
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimFunc(line, unicode.IsSpace)

		if trimmed == "" {
			continue
		}

		isOldHeader := isSoaLine(trimmed) ||
			isNSLine(trimmed) ||
			isTTLLine(trimmed)

		if !isOldHeader {
			_, err := fmt.Fprintln(f, line)
			if err != nil {
				return err
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading response body: %v", err)
	}

	return nil
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
	serial := time.Now().Format("2006010201")

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

	_, err = fmt.Fprintf(w, "@ IN SOA %s %s %s 86400 3600 604800 30\n", nameserver, hostmasterEmail, serial)
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
