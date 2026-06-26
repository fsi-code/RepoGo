package ops

import (
	"fmt"
	"os/exec"
	"time"

	"clipdev/internal/config"
	"clipdev/internal/parser"
)

var allowedGitSubs = map[string]bool{
	"status": true, "diff": true, "log": true,
	"add": true, "commit": true, "branch": true,
	"stash": true, "show": true, "blame": true,
}

func Git(cmd *parser.Command, cfg *config.Config) *Response {
	start := time.Now()

	if !allowedGitSubs[cmd.Sub] {
		return Failure(cmd.ID, "git", fmt.Sprintf("git sub-command not allowed: %s", cmd.Sub), "FORBIDDEN_SUBCMD")
	}

	args := append([]string{cmd.Sub}, cmd.Args...)
	c := exec.Command("git", args...)
	c.Dir = cfg.Workdir

	out, err := c.CombinedOutput()
	output := string(out)

	if err != nil {
		return Failure(cmd.ID, "git", fmt.Sprintf("git %s failed: %s\n%s", cmd.Sub, err.Error(), output), "GIT_ERROR")
	}

	output, trunc := truncate(output, cfg.Limits.MaxOutputBytes)
	return Success(cmd.ID, "git", output, time.Since(start), trunc)
}
