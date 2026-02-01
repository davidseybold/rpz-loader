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

	return executePDNSUtilCommand("zone", "load", zoneName, zoneFile)
}

func SetMetadataAlsoNotify(zoneName string, host string) error {
	return executePDNSUtilCommand("metadata", "set", zoneName, "ALSO-NOTIFY", host)
}

func NotifyZone(zoneName string) error {
	return executePDNSControlCommand("notify", zoneName)
}

func executePDNSUtilCommand(args ...string) error {
	return executeCommand("pdnsutil", args...)
}

func executePDNSControlCommand(args ...string) error {
	return executeCommand("pdns_control", args...)
}

func executeCommand(name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), powerDNSCommandTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)

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
		return fmt.Errorf("%s %s timed out after %s%s", name, strings.Join(args, " "), powerDNSCommandTimeout, outputDetails)
	}

	if exitErr, ok := err.(*exec.ExitError); ok {
		return fmt.Errorf("%s %s exited with code %d%s", name, strings.Join(args, " "), exitErr.ExitCode(), outputDetails)
	}

	return fmt.Errorf("%s %s failed: %w%s", name, strings.Join(args, " "), err, outputDetails)
}
