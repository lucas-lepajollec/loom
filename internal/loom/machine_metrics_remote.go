package loom

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func nodeMachineMetrics(ctx context.Context, m RemoteMachine) (any, error) {
	var metrics MachineMetrics
	if err := nodeMachineJSON(ctx, m, http.MethodGet, "/api/node/observe", nil, &metrics); err != nil {
		return nil, err
	}
	if metrics.At <= 0 || metrics.OS == "" {
		return nil, errors.New("invalid node metrics response")
	}
	if metrics.GPUs == nil {
		metrics.GPUs = []MachineGPUMetrics{}
	}
	return metrics, nil
}

const machineMetricsMarker = "LOOM-METRICS:"

// Sections keep /proc text unmodified and tolerate SSH login banners. Only
// fixed commands are sent: no browser-supplied path or shell text is accepted.
func machineMetricsSSHScript() string {
	script := remotePathPreamble + remoteOutputStart + `if [ "$(uname -s)" != Linux ]; then
 printf 'LOOM-METRICS:unsupported\n'
 exit 0
fi
printf 'LOOM-METRICS:stat1\n'; cat /proc/stat
sleep 0.3
printf 'LOOM-METRICS:stat2\n'; cat /proc/stat
printf 'LOOM-METRICS:meminfo\n'; cat /proc/meminfo
printf 'LOOM-METRICS:loadavg\n'; cat /proc/loadavg
printf 'LOOM-METRICS:uptime\n'; cat /proc/uptime
printf 'LOOM-METRICS:home\n%s\n' "$HOME"
printf 'LOOM-METRICS:disk\n'; stat -f -c '%S %b %f' -- "$HOME" 2>/dev/null
`
	for _, q := range gpuQueries {
		parts := []string{shellQuote(q.name)}
		for _, arg := range q.args {
			parts = append(parts, shellQuote(arg))
		}
		script += "printf '\\n%s\\n' " + shellQuote(machineMetricsMarker+q.id) + "\n"
		script += "if command -v " + shellQuote(q.name) + " >/dev/null 2>&1; then\n"
		// Most Linux hosts have timeout; bound vendor readers there as well as SSH.
		script += " if command -v timeout >/dev/null 2>&1; then timeout 1 " + strings.Join(parts, " ") + "; else " + strings.Join(parts, " ") + "; fi 2>/dev/null\nfi\n"
	}
	script += "printf '\\n%s\\n' " + shellQuote(machineMetricsMarker+"end") + "\n"
	return script
}

// The runner hook exercises the complete SSH parser path without SSH or sockets.
var runMachineMetricsSSH = execMachineMetricsSSH

func execMachineMetricsSSH(ctx context.Context, m RemoteMachine, script string) ([]byte, error) {
	m, err := validRemoteMachine(m)
	if err != nil {
		return nil, err
	}
	// Observation never generates keys. Use Loom's existing key, or the user's
	// normal SSH identities when no Loom key has been installed yet.
	key := filepath.Join(LoomHome(), "ssh", "loom_ed25519")
	if !regularFile(key) {
		key = ""
	}
	cmd := hideCmd(exec.CommandContext(ctx, "ssh", sshArgs(m, key, "sh", "-s")...))
	cmd.Stdin = strings.NewReader(script)
	cmd.Stderr = io.Discard
	output := &machineMetricsOutput{}
	cmd.Stdout = output
	cmd.WaitDelay = 100 * time.Millisecond
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, errors.New("SSH machine metrics timed out")
		}
		return nil, errors.New("SSH machine metrics unavailable; check machine access")
	}
	return []byte(output.String()), nil
}

type machineMetricsOutput struct{ strings.Builder }

func (b *machineMetricsOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 1<<20 {
		return 0, errors.New("machine metrics output too large")
	}
	return b.Builder.Write(p)
}

func sshMachineMetrics(ctx context.Context, m RemoteMachine) (any, error) {
	if m.OS != "" && !strings.EqualFold(m.OS, "linux") {
		return map[string]any{"supported": false}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := runMachineMetricsSSH(ctx, m, machineMetricsSSHScript())
	if err != nil {
		return nil, err
	}
	return parseSSHMachineMetrics(string(out))
}

func parseSSHMachineMetrics(text string) (any, error) {
	sections := map[string]string{}
	section := ""
	var content strings.Builder
	flush := func() {
		if section != "" {
			sections[section] = content.String()
		}
		content.Reset()
	}
	for _, line := range strings.Split(afterRemoteMarker(text), "\n") {
		if strings.HasPrefix(line, machineMetricsMarker) {
			flush()
			section = strings.TrimPrefix(line, machineMetricsMarker)
		} else if section != "" {
			content.WriteString(line)
			content.WriteByte('\n')
		}
	}
	flush()
	if _, ok := sections["unsupported"]; ok {
		return map[string]any{"supported": false}, nil
	}
	if sections["stat1"] == "" || sections["stat2"] == "" || sections["meminfo"] == "" {
		return nil, errors.New("unreadable SSH machine metrics response")
	}
	m := parseProcMachineMetrics(sections["stat1"], sections["stat2"], sections["meminfo"], sections["loadavg"], sections["uptime"])
	m.Disk.Path = strings.TrimSpace(sections["home"])
	f := strings.Fields(sections["disk"])
	if len(f) == 3 {
		size, e1 := strconv.ParseUint(f[0], 10, 64)
		blocks, e2 := strconv.ParseUint(f[1], 10, 64)
		free, e3 := strconv.ParseUint(f[2], 10, 64)
		if e1 == nil && e2 == nil && e3 == nil && size > 0 && free <= blocks && blocks <= ^uint64(0)/size {
			m.Disk.Total, m.Disk.Used = size*blocks, size*(blocks-free)
		}
	}
	if m.Disk.Total == 0 || m.Disk.Path == "" {
		m.Partial = true
	}
	m.GPUs = machineGPUs(liveGPUsWithRunner(func(name string, args ...string) ([]byte, error) {
		for _, q := range gpuQueries {
			if name == q.name && strings.Join(args, "\x00") == strings.Join(q.args, "\x00") {
				if data := strings.TrimSpace(sections[q.id]); data != "" {
					return []byte(data), nil
				}
			}
		}
		return nil, errors.New("GPU observation unavailable")
	}))
	return m, nil
}
