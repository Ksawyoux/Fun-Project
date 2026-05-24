package reasoner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClaudeCLIComplete_TimesOut(t *testing.T) {
	bin := writeScript(t, "slow-claude", "#!/bin/sh\nsleep 1\n")
	cli := &ClaudeCLI{bin: bin, timeout: 10 * time.Millisecond}

	_, err := cli.Complete(context.Background(), "system", "user")
	if err == nil {
		t.Fatalf("Complete() error = nil, want timeout")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("Complete() error = %q, want timeout", err)
	}
}

func TestClaudeCLIComplete_TruncatesStderrInError(t *testing.T) {
	bin := writeScript(t, "loud-claude", `#!/bin/sh
i=0
while [ "$i" -lt 9000 ]; do
  printf x >&2
  i=$((i + 1))
done
exit 2
`)
	cli := &ClaudeCLI{bin: bin, timeout: time.Second}

	_, err := cli.Complete(context.Background(), "system", "user")
	if err == nil {
		t.Fatalf("Complete() error = nil, want failure")
	}
	if len(err.Error()) > maxClaudeCLIStderr+512 {
		t.Fatalf("Complete() error length = %d, want bounded stderr", len(err.Error()))
	}
}

func writeScript(t *testing.T, name, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(contents), 0o755); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
	return path
}
