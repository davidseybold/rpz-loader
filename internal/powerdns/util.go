package powerdns

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const powerDNSCommandTimeout = 30 * time.Second

func SyncZoneFromFile(zoneName string, zoneFile string) error {

	if strings.TrimSpace(zoneName) == "" {
		return fmt.Errorf("zoneName is required")
	}
	if strings.TrimSpace(zoneFile) == "" {
		return fmt.Errorf("zoneFile is required")
	}

	return executePowerDNSCommand("load", "zone", zoneName, zoneFile)
}

func executePowerDNSCommand(args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), powerDNSCommandTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "pdnsutil", args...)

	err := cmd.Run()
	if err == nil {
		return nil
	}

	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("pdnsutil %s timed out after %s", strings.Join(args, " "), powerDNSCommandTimeout)
	}

	if exitErr, ok := err.(*exec.ExitError); ok {
		return fmt.Errorf("pdnsutil %s exited with code %d", strings.Join(args, " "), exitErr.ExitCode())
	}

	return fmt.Errorf("pdnsutil %s failed: %w", strings.Join(args, " "), err)
}
