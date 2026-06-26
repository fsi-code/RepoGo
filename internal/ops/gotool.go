package ops

import (
	"context"
	"fmt"
	"os/exec"
	"time"

	"clipdev/internal/config"
	"clipdev/internal/parser"
)

var allowedGoSubs = map[string]bool{
	"build": true, "test": true, "vet": true,
	"fmt": true, "generate": true,
}

func GoTool(cmd *parser.Command, cfg *config.Config) *Response {
	start := time.Now()

	if !allowedGoSubs[cmd.Sub] {
		// "mod tidy" is two words; handle specially
		if cmd.Sub == "mod" && len(cmd.Args) > 0 && cmd.Args[0] == "tidy" {
			return runGo(cmd, cfg, []string{"mod", "tidy"}, start)
		}
		return Failure(cmd.ID, "go", fmt.Sprintf("go sub-command not allowed: %s", cmd.Sub), "FORBIDDEN_SUBCMD")
	}

	var args []string
	switch cmd.Sub {
	case "test":
		args = []string{"test"}
		if cmd.Pkg != "" {
			args = append(args, cmd.Pkg)
		} else {
			args = append(args, "./...")
		}
		if cmd.Run != "" {
			args = append(args, "-run", cmd.Run)
		}
		if cmd.Timeout != "" {
			args = append(args, "-timeout", cmd.Timeout)
		}
		args = append(args, cmd.Args...)
	case "build":
		args = []string{"build"}
		if cmd.Pkg != "" {
			args = append(args, cmd.Pkg)
		}
		args = append(args, cmd.Args...)
	default:
		args = append([]string{cmd.Sub}, cmd.Args...)
	}

	return runGo(cmd, cfg, args, start)
}

func runGo(cmd *parser.Command, cfg *config.Config, args []string, start time.Time) *Response {
	timeout := cfg.Limits.MaxTimeout.Duration
	if cmd.Timeout != "" {
		if d, err := time.ParseDuration(cmd.Timeout); err == nil && d < timeout {
			timeout = d
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	c := exec.CommandContext(ctx, "go", args...)
	c.Dir = cfg.Workdir
	out, err := c.CombinedOutput()
	output := string(out)

	if ctx.Err() == context.DeadlineExceeded {
		return Failure(cmd.ID, "go", "command timed out", "TIMEOUT")
	}
	if err != nil {
		return Failure(cmd.ID, "go", fmt.Sprintf("go %s failed: %s\n%s", args[0], err.Error(), output), "GO_ERROR")
	}

	output, trunc := truncate(output, cfg.Limits.MaxOutputBytes)
	return Success(cmd.ID, "go", output, time.Since(start), trunc)
}
