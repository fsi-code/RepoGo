package ops

import (
	"context"
	"fmt"
	"os/exec"
	"time"

	"clipdev/internal/config"
	"clipdev/internal/parser"
)

func Python(cmd *parser.Command, cfg *config.Config) *Response {
	start := time.Now()

	if cmd.Script == "" {
		return Failure(cmd.ID, "python", "script is required", "MISSING_PARAM")
	}

	timeout := 10 * time.Second
	if cmd.Timeout != "" {
		if d, err := time.ParseDuration(cmd.Timeout); err == nil {
			if d > cfg.Limits.MaxTimeout.Duration {
				d = cfg.Limits.MaxTimeout.Duration
			}
			timeout = d
		}
	}

	exe := findPython()
	if exe == "" {
		return Failure(cmd.ID, "python", "python3 / python not found in PATH", "NOT_FOUND")
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	c := exec.CommandContext(ctx, exe, "-c", cmd.Script)
	c.Dir = cfg.Workdir
	out, err := c.CombinedOutput()
	output := string(out)

	if ctx.Err() == context.DeadlineExceeded {
		return Failure(cmd.ID, "python", "script timed out", "TIMEOUT")
	}
	if err != nil {
		return Failure(cmd.ID, "python",
			fmt.Sprintf("%s failed: %s\n%s", exe, err.Error(), output),
			"PYTHON_ERROR")
	}

	output, trunc := truncate(output, cfg.Limits.MaxOutputBytes)
	return Success(cmd.ID, "python", output, time.Since(start), trunc)
}

// findPython returns the first Python interpreter found in PATH.
// On Windows the executable is typically "python"; on Linux/macOS "python3".
func findPython() string {
	for _, name := range []string{"python3", "python"} {
		if _, err := exec.LookPath(name); err == nil {
			return name
		}
	}
	return ""
}
