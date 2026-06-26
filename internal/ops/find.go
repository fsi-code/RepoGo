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

func Find(cmd *parser.Command, sb *sandbox.Sandbox, cfg *config.Config) *Response {
	start := time.Now()

	searchPath := "."
	if cmd.Path != "" {
		searchPath = cmd.Path
	}

	root, err := sb.Resolve(searchPath)
	if err != nil {
		return Failure(cmd.ID, "find", err.Error(), "PATH_ESCAPE")
	}

	var pattern string
	if cmd.Name != "" {
		pattern = cmd.Name
	}

	var results []string
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.Name() == ".git" || d.Name() == "vendor" || d.Name() == "node_modules" {
			if d.IsDir() {
				return filepath.SkipDir
			}
		}
		if pattern != "" {
			matched, merr := filepath.Match(pattern, d.Name())
			if merr != nil || !matched {
				return nil
			}
		}
		rel, _ := filepath.Rel(sb.Workdir(), path)
		prefix := "F"
		if d.IsDir() {
			prefix = "D"
		}
		results = append(results, fmt.Sprintf("[%s] %s", prefix, rel))
		return nil
	})

	if err != nil {
		return Failure(cmd.ID, "find", err.Error(), "FS_ERROR")
	}

	result := fmt.Sprintf("Found %d entries\n\n%s", len(results), strings.Join(results, "\n"))
	result, trunc := truncate(result, cfg.Limits.MaxOutputBytes)
	return Success(cmd.ID, "find", result, time.Since(start), trunc)
}
