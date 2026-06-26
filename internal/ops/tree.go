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

var defaultIgnore = []string{".git", "vendor", "node_modules", "__pycache__", ".DS_Store"}

func Tree(cmd *parser.Command, sb *sandbox.Sandbox, cfg *config.Config) *Response {
	start := time.Now()

	root := "."
	if cmd.Path != "" {
		root = cmd.Path
	}

	absRoot, err := sb.Resolve(root)
	if err != nil {
		return Failure(cmd.ID, "tree", err.Error(), "PATH_ESCAPE")
	}

	depth := cmd.Depth
	if depth <= 0 {
		depth = 4
	}

	ignore := make(map[string]bool)
	for _, d := range defaultIgnore {
		ignore[d] = true
	}
	for _, d := range cmd.Ignore {
		ignore[d] = true
	}

	var buf strings.Builder
	rel, _ := filepath.Rel(sb.Workdir(), absRoot)
	if rel == "" || rel == "." {
		rel = filepath.Base(absRoot)
	}
	fmt.Fprintf(&buf, "%s\n", rel)

	buildTree(&buf, absRoot, "", 0, depth, ignore)

	result, trunc := truncate(buf.String(), cfg.Limits.MaxOutputBytes)
	return Success(cmd.ID, "tree", result, time.Since(start), trunc)
}

func buildTree(buf *strings.Builder, path, prefix string, depth, maxDepth int, ignore map[string]bool) {
	if depth >= maxDepth {
		return
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return
	}

	for i, entry := range entries {
		if ignore[entry.Name()] {
			continue
		}

		connector := "├── "
		childPrefix := prefix + "│   "
		if i == len(entries)-1 {
			connector = "└── "
			childPrefix = prefix + "    "
		}

		icon := "📄"
		if entry.IsDir() {
			icon = "📁"
		}
		fmt.Fprintf(buf, "%s%s%s %s\n", prefix, connector, icon, entry.Name())

		if entry.IsDir() {
			buildTree(buf, filepath.Join(path, entry.Name()), childPrefix, depth+1, maxDepth, ignore)
		}
	}
}
