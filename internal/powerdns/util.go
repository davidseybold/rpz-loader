package powerdns

import (
	"bytes"
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

	return executePowerDNSCommand("zone", "load", zoneName, zoneFile)
}

func executePowerDNSCommand(args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), powerDNSCommandTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "pdnsutil", args...)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err == nil {
		return nil
	}

	stdoutStr := strings.TrimSpace(stdout.String())
	stderrStr := strings.TrimSpace(stderr.String())
	outputDetails := ""
	if stdoutStr != "" {
		outputDetails += fmt.Sprintf(" stdout=%q", stdoutStr)
	}
	if stderrStr != "" {
		outputDetails += fmt.Sprintf(" stderr=%q", stderrStr)
	}

	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("pdnsutil %s timed out after %s%s", strings.Join(args, " "), powerDNSCommandTimeout, outputDetails)
	}

	if exitErr, ok := err.(*exec.ExitError); ok {
		return fmt.Errorf("pdnsutil %s exited with code %d%s", strings.Join(args, " "), exitErr.ExitCode(), outputDetails)
	}

	return fmt.Errorf("pdnsutil %s failed: %w%s", strings.Join(args, " "), err, outputDetails)
}
