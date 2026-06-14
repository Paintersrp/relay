//go:build windows

package handlers

import (
	"fmt"
	"os/exec"
	"strings"
)

func probeProcessAlivePlatform(pid int) processProbeResult {
	cmd := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", pid), "/FO", "CSV", "/NH")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return processProbeResult{Known: false, Error: fmt.Sprintf("tasklist probe failed: %v", err)}
	}

	text := strings.TrimSpace(string(out))
	if text == "" {
		return processProbeResult{Known: false, Error: "tasklist probe returned no output"}
	}
	if strings.Contains(text, "No tasks are running") {
		return processProbeResult{Alive: false, Known: true}
	}
	pidCSV := fmt.Sprintf(`,"%d",`, pid)
	if strings.Contains(text, pidCSV) || strings.Contains(text, fmt.Sprintf(`,"%d"`, pid)) {
		return processProbeResult{Alive: true, Known: true}
	}
	return processProbeResult{Known: false, Error: "unrecognized tasklist output: " + text}
}
