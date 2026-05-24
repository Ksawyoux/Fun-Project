package reasoner

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const (
	defaultClaudeCLITimeout = 45 * time.Second
	maxClaudeCLIStderr      = 8192
)

// ClaudeCLI shells out to the `claude` binary (Claude Code) in --print mode.
// It reuses whatever login the host already has — no API key needed — at the
// cost of one subprocess per Complete call. Implements LLM.
type ClaudeCLI struct {
	bin     string
	timeout time.Duration
}

// NewClaudeCLI returns an adapter or an error if the binary isn't on PATH.
// Pass "" to use the default lookup for `claude`.
func NewClaudeCLI(bin string) (*ClaudeCLI, error) {
	if bin == "" {
		bin = "claude"
	}
	resolved, err := exec.LookPath(bin)
	if err != nil {
		return nil, fmt.Errorf("claude CLI not found on PATH: %w", err)
	}
	return &ClaudeCLI{bin: resolved, timeout: defaultClaudeCLITimeout}, nil
}

func (c *ClaudeCLI) Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	timeout := c.timeout
	if timeout <= 0 {
		timeout = defaultClaudeCLITimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Disallow tools — we only want raw text completion, not agentic behavior.
	cmd := exec.CommandContext(runCtx, c.bin,
		"--print",
		"--output-format", "text",
		"--append-system-prompt", systemPrompt,
		"--disallowed-tools",
		"Bash Edit Read Write WebFetch WebSearch Glob Grep Task TodoWrite NotebookEdit",
	)
	cmd.Stdin = strings.NewReader(userPrompt)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &limitedBuffer{buf: &stderr, max: maxClaudeCLIStderr}
	if err := cmd.Run(); err != nil {
		if runCtx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("claude cli timed out after %s", timeout)
		}
		return "", fmt.Errorf("claude cli: %w (stderr: %s)", err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

type limitedBuffer struct {
	buf       *bytes.Buffer
	max       int
	truncated bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.max <= 0 || b.buf.Len() >= b.max {
		b.truncated = true
		return len(p), nil
	}
	remaining := b.max - b.buf.Len()
	if len(p) > remaining {
		_, _ = b.buf.Write(p[:remaining])
		b.truncated = true
		return len(p), nil
	}
	return b.buf.Write(p)
}
