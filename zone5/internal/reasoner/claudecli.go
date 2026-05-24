package reasoner

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// ClaudeCLI shells out to the `claude` binary (Claude Code) in --print mode.
// It reuses whatever login the host already has — no API key needed — at the
// cost of one subprocess per Complete call. Implements LLM.
type ClaudeCLI struct {
	bin string
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
	return &ClaudeCLI{bin: resolved}, nil
}

func (c *ClaudeCLI) Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	// Disallow tools — we only want raw text completion, not agentic behavior.
	cmd := exec.CommandContext(ctx, c.bin,
		"--print",
		"--output-format", "text",
		"--append-system-prompt", systemPrompt,
		"--disallowed-tools",
		"Bash Edit Read Write WebFetch WebSearch Glob Grep Task TodoWrite NotebookEdit",
	)
	cmd.Stdin = strings.NewReader(userPrompt)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("claude cli: %w (stderr: %s)", err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}
