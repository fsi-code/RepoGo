package ops

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"clipdev/internal/config"
	"clipdev/internal/parser"
	"clipdev/internal/sandbox"
)

func Grep(cmd *parser.Command, sb *sandbox.Sandbox, cfg *config.Config) *Response {
	start := time.Now()

	if cmd.Pattern == "" {
		return Failure(cmd.ID, "grep", "pattern is required", "MISSING_PARAM")
	}

	re, err := regexp.Compile(cmd.Pattern)
	if err != nil {
		return Failure(cmd.ID, "grep", "invalid pattern: "+err.Error(), "INVALID_PATTERN")
	}

	searchPath := "."
	if cmd.Path != "" {
		searchPath = cmd.Path
	}

	root, err := sb.Resolve(searchPath)
	if err != nil {
		return Failure(cmd.ID, "grep", err.Error(), "PATH_ESCAPE")
	}

	extSet := make(map[string]bool)
	for _, e := range cmd.Ext {
		if !strings.HasPrefix(e, ".") {
			e = "." + e
		}
		extSet[e] = true
	}

	var out strings.Builder
	matchCount := 0

	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "vendor" || d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if len(extSet) > 0 && !extSet[filepath.Ext(path)] {
			return nil
		}

		lines, ferr := grepFile(path, re, cmd.Context)
		if ferr != nil || len(lines) == 0 {
			return nil
		}

		rel, _ := filepath.Rel(sb.Workdir(), path)
		fmt.Fprintf(&out, "--- %s ---\n%s\n", rel, strings.Join(lines, "\n"))
		matchCount += len(lines)
		return nil
	})

	if err != nil {
		return Failure(cmd.ID, "grep", err.Error(), "FS_ERROR")
	}

	result := fmt.Sprintf("Found %d matches\n\n%s", matchCount, out.String())
	result, trunc := truncate(result, cfg.Limits.MaxOutputBytes)
	return Success(cmd.ID, "grep", result, time.Since(start), trunc)
}

func grepFile(path string, re *regexp.Regexp, ctx int) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	sc := bufio.NewScanner(f)
	var allLines []string
	for sc.Scan() {
		allLines = append(allLines, sc.Text())
	}
	if sc.Err() != nil {
		return nil, sc.Err()
	}

	shown := make(map[int]bool)
	for i, line := range allLines {
		if !re.MatchString(line) {
			continue
		}
		from := imax(0, i-ctx)
		to := imin(len(allLines)-1, i+ctx)
		for j := from; j <= to; j++ {
			if shown[j] {
				continue
			}
			shown[j] = true
			prefix := "  "
			if j == i {
				prefix = "> "
			}
			lines = append(lines, fmt.Sprintf("%4d%s%s", j+1, prefix, allLines[j]))
		}
		if ctx > 0 {
			lines = append(lines, "---")
		}
	}
	return lines, nil
}

func imax(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func imin(a, b int) int {
	if a < b {
		return a
	}
	return b
}
