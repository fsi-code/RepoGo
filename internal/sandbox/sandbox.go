package sandbox

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

var ErrPathEscape = errors.New("path escapes workdir sandbox")

type Sandbox struct {
	workdir string
}

func New(workdir string) *Sandbox {
	abs, err := filepath.Abs(workdir)
	if err != nil {
		abs = filepath.Clean(workdir)
	}
	return &Sandbox{workdir: abs + string(filepath.Separator)}
}

func (s *Sandbox) Resolve(path string) (string, error) {
	var abs string
	if filepath.IsAbs(path) {
		abs = filepath.Clean(path)
	} else {
		abs = filepath.Clean(filepath.Join(s.Workdir(), path))
	}
	if !strings.HasPrefix(abs+string(filepath.Separator), s.workdir) {
		return "", fmt.Errorf("%w: %s", ErrPathEscape, path)
	}
	return abs, nil
}

func (s *Sandbox) Workdir() string {
	return strings.TrimSuffix(s.workdir, string(filepath.Separator))
}
