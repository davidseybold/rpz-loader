package rpz

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/davidseybold/rpz-loader/internal/config"
)

func WriteZoneFile(filename string, name string, ttl int, rules []config.RPZRule) error {
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	fmt.Fprintf(f, "$ORIGIN %s.\n", name)
	fmt.Fprintf(f, "$TTL %d\n", ttl)
	fmt.Fprintf(f, "%s. IN SOA localhost. admin.%s. (1 60 60 60 60)\n", name, name)
	fmt.Fprintf(f, "NS localhost.\n")

	for _, rule := range rules {
		fmt.Fprintf(f, "%s CNAME %s\n", rule.Trigger, rule.Action)
		if rule.IncludeSubdomains {
			fmt.Fprintf(f, "*.%s CNAME %s\n", rule.Trigger, rule.Action)
		}
	}

	return nil
}

func FetchZoneFile(filename string, name string, url string) error {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	fmt.Fprintf(f, "$ORIGIN %s.\n", name)

	_, err = io.Copy(f, resp.Body)
	return err
}
