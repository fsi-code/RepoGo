package ops

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"clipdev/internal/config"
	"clipdev/internal/parser"
	"clipdev/internal/sandbox"
)

var forbiddenExts = map[string]bool{
	".toml": true, ".env": true, ".yaml": true, ".yml": true,
}

func Write(cmd *parser.Command, sb *sandbox.Sandbox, cfg *config.Config) *Response {
	start := time.Now()

	path, err := sb.Resolve(cmd.Path)
	if err != nil {
		return Failure(cmd.ID, "write", err.Error(), "PATH_ESCAPE")
	}

	if forbiddenExts[strings.ToLower(filepath.Ext(path))] {
		return Failure(cmd.ID, "write", "writing to config/env files is forbidden", "FORBIDDEN_EXT")
	}

	if cmd.DryRun {
		preview := fmt.Sprintf("Would write %d bytes to %s (mode: %s)", len(cmd.Content), cmd.Path, cmd.Mode)
		return DryRunResult(cmd.ID, "write", preview)
	}

	mode := cmd.Mode
	if mode == "" {
		mode = "overwrite"
	}

	switch mode {
	case "create":
		if _, err := os.Stat(path); err == nil {
			return Failure(cmd.ID, "write", "file already exists: "+cmd.Path, "FILE_EXISTS")
		}
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return Failure(cmd.ID, "write", err.Error(), "IO_ERROR")
		}
		if err := os.WriteFile(path, []byte(cmd.Content), 0644); err != nil {
			return Failure(cmd.ID, "write", err.Error(), "IO_ERROR")
		}

	case "overwrite":
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return Failure(cmd.ID, "write", err.Error(), "IO_ERROR")
		}
		if err := os.WriteFile(path, []byte(cmd.Content), 0644); err != nil {
			return Failure(cmd.ID, "write", err.Error(), "IO_ERROR")
		}

	case "append":
		f, ferr := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if ferr != nil {
			return Failure(cmd.ID, "write", ferr.Error(), "IO_ERROR")
		}
		_, ferr = f.WriteString(cmd.Content)
		f.Close()
		if ferr != nil {
			return Failure(cmd.ID, "write", ferr.Error(), "IO_ERROR")
		}

	default:
		return Failure(cmd.ID, "write", "unknown mode: "+mode, "INVALID_MODE")
	}

	result := fmt.Sprintf("Written %d bytes to %s", len(cmd.Content), cmd.Path)
	return Success(cmd.ID, "write", result, time.Since(start), false)
}
