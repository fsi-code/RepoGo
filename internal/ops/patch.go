package ops

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"clipdev/internal/config"
	"clipdev/internal/parser"
	"clipdev/internal/sandbox"
)

func Patch(cmd *parser.Command, sb *sandbox.Sandbox, cfg *config.Config) *Response {
	start := time.Now()

	if cmd.Diff == "" {
		return Failure(cmd.ID, "patch", "diff is required", "MISSING_PARAM")
	}

	if _, err := sb.Resolve(cmd.Path); err != nil {
		return Failure(cmd.ID, "patch", err.Error(), "PATH_ESCAPE")
	}

	if cmd.DryRun {
		preview := fmt.Sprintf("Would apply patch to %s (%d bytes diff)", cmd.Path, len(cmd.Diff))
		return DryRunResult(cmd.ID, "patch", preview)
	}

	// `git apply` works identically on Linux, macOS, and Windows
	// (provided Git is installed, which is required for the git op anyway).
	c := exec.Command("git", "apply", "--whitespace=nowarn", "-")
	c.Dir = sb.Workdir()
	c.Stdin = strings.NewReader(cmd.Diff)
	out, err := c.CombinedOutput()
	if err != nil {
		return Failure(cmd.ID, "patch",
			fmt.Sprintf("git apply failed: %s\n%s", err.Error(), string(out)),
			"PATCH_ERROR")
	}

	result := fmt.Sprintf("Patch applied to %s\n%s", cmd.Path, string(out))
	return Success(cmd.ID, "patch", result, time.Since(start), false)
}
