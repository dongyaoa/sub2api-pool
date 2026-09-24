package poolupdate

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
)

// Runner never invokes a shell. Config supplies trusted executable paths only.
type Runner interface {
	Run(context.Context, string, string, []string) ([]byte, error)
}
type ExecRunner struct{}

type boundedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.Len() {
		return 0, errors.New("command output exceeds limit")
	}
	return b.Buffer.Write(p)
}
func (ExecRunner) Run(ctx context.Context, dir, command string, args []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, command, args...) //nolint:gosec // executable and argument prefixes come only from root-owned host configuration.
	cmd.Dir = dir
	cmd.Env = []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", "HOME=/root", "DOCKER_HOST=unix:///var/run/docker.sock"}
	if value := os.Getenv("SYSTEMROOT"); value != "" {
		cmd.Env = append(cmd.Env, "SYSTEMROOT="+value)
	}
	stdout := &boundedBuffer{limit: 2 << 20}
	stderr := &boundedBuffer{limit: 2 << 20}
	cmd.Stdout = stdout
	// Output is consumed only by strict parsers. Never persist it or forward
	// it across the socket: Compose errors may contain deployment values.
	cmd.Stderr = stderr
	err := cmd.Run()
	if err != nil {
		return nil, errors.New("docker command failed")
	}
	if len(args) == 4 && args[0] == "exec" && args[2] == "/app/sub2api" && args[3] == "-version" {
		return append(stdout.Bytes(), stderr.Bytes()...), nil
	}
	return stdout.Bytes(), nil
}
