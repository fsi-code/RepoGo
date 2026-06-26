package ops

import (
	"bufio"
	"os"
	"strings"
	"time"

	"clipdev/internal/config"
	"clipdev/internal/parser"
	"clipdev/internal/sandbox"
)

func Read(cmd *parser.Command, sb *sandbox.Sandbox, cfg *config.Config) *Response {
	start := time.Now()

	path, err := sb.Resolve(cmd.Path)
	if err != nil {
		return Failure(cmd.ID, "read", err.Error(), "PATH_ESCAPE")
	}

	f, err := os.Open(path)
	if err != nil {
		return Failure(cmd.ID, "read", err.Error(), "IO_ERROR")
	}
	defer f.Close()

	var result string
	if len(cmd.Lines) == 2 && cmd.Lines[0] > 0 {
		result, err = readRange(f, cmd.Lines[0], cmd.Lines[1])
	} else {
		var buf strings.Builder
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			buf.WriteString(sc.Text())
			buf.WriteByte('\n')
		}
		result = buf.String()
		err = sc.Err()
	}

	if err != nil {
		return Failure(cmd.ID, "read", err.Error(), "IO_ERROR")
	}

	result, trunc := truncate(result, cfg.Limits.MaxOutputBytes)
	return Success(cmd.ID, "read", result, time.Since(start), trunc)
}

func readRange(f *os.File, from, to int) (string, error) {
	var buf strings.Builder
	sc := bufio.NewScanner(f)
	line := 0
	for sc.Scan() {
		line++
		if line < from {
			continue
		}
		if line > to {
			break
		}
		buf.WriteString(sc.Text())
		buf.WriteByte('\n')
	}
	return buf.String(), sc.Err()
}
