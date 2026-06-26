package dispatch

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"clipdev/internal/broker"
	"clipdev/internal/config"
	"clipdev/internal/hitl"
	"clipdev/internal/ops"
	"clipdev/internal/parser"
	"clipdev/internal/sandbox"
)

type Dispatcher struct {
	cfg         *config.Config
	sb          *sandbox.Sandbox
	hitl        *hitl.Manager
	broker      *broker.Broker
	sessionOnce sync.Once // écrit l'en-tête de session la première fois
}

func New(cfg *config.Config, h *hitl.Manager, b *broker.Broker) *Dispatcher {
	return &Dispatcher{
		cfg:    cfg,
		sb:     sandbox.New(cfg.Workdir),
		hitl:   h,
		broker: b,
	}
}

func (d *Dispatcher) Dispatch(cmd *parser.Command) *ops.Response {
	if cmd.ID == "" {
		cmd.ID = fmt.Sprintf("%d", time.Now().UnixMilli())
	}

	if !d.isAllowed(cmd.Op) {
		return ops.Failure(cmd.ID, cmd.Op, "unknown operation: "+cmd.Op, "UNKNOWN_OP")
	}

	// HITL gate for write/patch
	if d.hitl.Requires(cmd.Op) && !cmd.DryRun {
		d.broker.BroadcastHITL(cmd, d.hitl.TimeoutSecs(cmd.ID))
		pending := d.hitl.Submit(cmd)
		decision := d.hitl.Wait(pending)
		d.broker.BroadcastHITLClear()

		if !decision.Approved {
			return ops.Failure(cmd.ID, cmd.Op, "operation rejected by user", "REJECTED")
		}
	}

	resp := d.execute(cmd)

	if resp.OK && d.cfg.Obsidian.VaultPath != "" {
		go d.writeObsidian(cmd, resp)
	}

	return resp
}

func (d *Dispatcher) execute(cmd *parser.Command) *ops.Response {
	switch cmd.Op {
	case "read":
		return ops.Read(cmd, d.sb, d.cfg)
	case "grep":
		return ops.Grep(cmd, d.sb, d.cfg)
	case "find":
		return ops.Find(cmd, d.sb, d.cfg)
	case "write":
		return ops.Write(cmd, d.sb, d.cfg)
	case "patch":
		return ops.Patch(cmd, d.sb, d.cfg)
	case "git":
		return ops.Git(cmd, d.cfg)
	case "go":
		return ops.GoTool(cmd, d.cfg)
	case "tree":
		return ops.Tree(cmd, d.sb, d.cfg)
	case "python":
		return ops.Python(cmd, d.cfg)
	default:
		return ops.Failure(cmd.ID, cmd.Op, "unknown op: "+cmd.Op, "UNKNOWN_OP")
	}
}

func (d *Dispatcher) isAllowed(op string) bool {
	allowed := []string{"read", "grep", "find", "write", "patch", "git", "go", "tree", "python"}
	for _, a := range allowed {
		if a == op {
			return true
		}
	}
	return false
}

// writeObsidian écrit l'op dans le vault. À la première écriture de la session,
// un encart de séparation bleu (callout Obsidian) est automatiquement inséré.
func (d *Dispatcher) writeObsidian(cmd *parser.Command, resp *ops.Response) {
	obs := d.cfg.Obsidian
	if obs.VaultPath == "" {
		return
	}

	target := filepath.Join(obs.VaultPath, obs.File)
	os.MkdirAll(filepath.Dir(target), 0755)

	if obs.WriteMode == "append" {
		// Separator de session automatique (une seule fois par démarrage du daemon)
		d.sessionOnce.Do(func() {
			writeSessionSeparator(target, obs, d.cfg.Workdir, "🔵 Nouvelle Session")
		})
	}

	var resultStr string
	json.Unmarshal(resp.Result, &resultStr)

	var content strings.Builder
	fmt.Fprintf(&content, "\n### %s\n\n", obs.Title)
	fmt.Fprintf(&content, "| Champ | Valeur |\n|---|---|\n")
	fmt.Fprintf(&content, "| **op** | `%s` |\n", cmd.Op)
	fmt.Fprintf(&content, "| **date** | %s |\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Fprintf(&content, "| **catégorie** | %s |\n", obs.Category)
	if cmd.Path != "" {
		fmt.Fprintf(&content, "| **fichier** | `%s` |\n", cmd.Path)
	}
	fmt.Fprintf(&content, "\n")
	if resultStr != "" {
		snippet := resultStr
		if len(snippet) > 1000 {
			snippet = snippet[:1000] + "\n... [truncated]"
		}
		fmt.Fprintf(&content, "```\n%s\n```\n", snippet)
	}

	if obs.WriteMode == "new" {
		base := strings.TrimSuffix(obs.File, filepath.Ext(obs.File))
		ts := time.Now().Format("20060102-150405")
		newFile := fmt.Sprintf("%s_%s%s", base, ts, filepath.Ext(obs.File))
		target = filepath.Join(obs.VaultPath, newFile)
		os.WriteFile(target, []byte(content.String()), 0644)
	} else {
		appendToFile(target, content.String())
	}

	d.broker.BroadcastObsidian(obs.File)
}

// PublishSession insère manuellement un encart de séparation dans le MD Obsidian.
// Appelé depuis le bouton "Publish" de l'UI.
func (d *Dispatcher) PublishSession(label string) error {
	obs := d.cfg.Obsidian
	if obs.VaultPath == "" {
		return fmt.Errorf("vault path non configuré")
	}
	target := filepath.Join(obs.VaultPath, obs.File)
	os.MkdirAll(filepath.Dir(target), 0755)

	if label == "" {
		label = "📌 Publication manuelle"
	}
	writeSessionSeparator(target, obs, d.cfg.Workdir, label)
	// Réinitialise le Once pour que la prochaine op écrive à la suite sans doublon.
	d.sessionOnce = sync.Once{}
	d.broker.BroadcastObsidian(obs.File)
	return nil
}

// writeSessionSeparator écrit un callout bleu Obsidian (style [!info]) comme séparateur.
func writeSessionSeparator(target string, obs config.ObsidianConfig, workdir, label string) {
	ts := time.Now().Format("02 Jan 2006 · 15:04:05")
	var sep strings.Builder
	fmt.Fprintf(&sep, "\n\n---\n\n")
	fmt.Fprintf(&sep, "> [!info] %s — %s\n", label, ts)
	fmt.Fprintf(&sep, "> **Workdir** : `%s`  \n", workdir)
	fmt.Fprintf(&sep, "> **Fichier** : `%s`  \n", obs.File)
	fmt.Fprintf(&sep, "> **Catégorie** : %s\n\n", obs.Category)
	appendToFile(target, sep.String())
}

func appendToFile(path, content string) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err == nil {
		f.WriteString(content)
		f.Close()
	}
}
