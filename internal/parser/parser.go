package parser

import (
	"encoding/json"
	"fmt"
	"strings"
)

const Version = "1.0"
const SentinelKey = "_clipdev"

type Command struct {
	Clipdev string `json:"_clipdev"`
	Op      string `json:"op"`
	ID      string `json:"id"`

	// read
	Path  string `json:"path"`
	Lines []int  `json:"lines"`

	// grep
	Pattern string   `json:"pattern"`
	Ext     []string `json:"ext"`
	Context int      `json:"context"`

	// find
	Name          string `json:"name"`
	ModifiedAfter string `json:"modified_after"`

	// write
	Content string `json:"content"`
	Mode    string `json:"mode"` // create | overwrite | append

	// patch
	Diff string `json:"diff"`

	// git / go
	Sub     string   `json:"sub"`
	Args    []string `json:"args"`
	Pkg     string   `json:"pkg"`
	Run     string   `json:"run"`
	Timeout string   `json:"timeout"`

	// tree
	Depth  int      `json:"depth"`
	Ignore []string `json:"ignore"`

	// python
	Script string `json:"script"`

	// meta
	DryRun bool   `json:"dry_run"`
	Sig    string `json:"_sig,omitempty"`
}

// Extract scans arbitrary text for embedded clipdev JSON objects.
func Extract(content string) ([]*Command, error) {
	var cmds []*Command
	i := 0
	for i < len(content) {
		idx := strings.Index(content[i:], "{")
		if idx < 0 {
			break
		}
		start := i + idx
		obj, end, err := extractObject(content, start)
		if err != nil {
			i = start + 1
			continue
		}
		var cmd Command
		if jsonErr := json.Unmarshal([]byte(obj), &cmd); jsonErr != nil {
			i = end
			continue
		}
		if cmd.Clipdev != Version || cmd.Op == "" {
			i = end
			continue
		}
		// Les réponses du daemon contiennent "ok": — les ignorer pour éviter
		// de retraiter notre propre output (double défense avec lastWritten).
		if strings.Contains(obj, `"ok":`) {
			i = end
			continue
		}
		cmds = append(cmds, &cmd)
		i = end
	}
	return cmds, nil
}

func extractObject(s string, start int) (string, int, error) {
	depth, inStr, escape := 0, false, false
	for i := start; i < len(s); i++ {
		c := s[i]
		if escape {
			escape = false
			continue
		}
		if c == '\\' && inStr {
			escape = true
			continue
		}
		if c == '"' {
			inStr = !inStr
			continue
		}
		if inStr {
			continue
		}
		switch c {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				candidate := s[start : i+1]
				if strings.Contains(candidate, `"`+SentinelKey+`"`) {
					return candidate, i + 1, nil
				}
				return "", i + 1, fmt.Errorf("not a clipdev object")
			}
		}
	}
	return "", len(s), fmt.Errorf("unclosed JSON object")
}
