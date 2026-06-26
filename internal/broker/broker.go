package broker

import (
	"fmt"
	"html"
	"strings"
	"sync"
	"html/template"
	"time"

	"clipdev/internal/parser"
)

type SSEEvent struct {
	Event string
	Data  string
}

type Broker struct {
	mu      sync.RWMutex
	clients map[chan SSEEvent]struct{}
}

func New() *Broker {
	return &Broker{clients: make(map[chan SSEEvent]struct{})}
}

func (b *Broker) Subscribe() chan SSEEvent {
	ch := make(chan SSEEvent, 64)
	b.mu.Lock()
	b.clients[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

func (b *Broker) Unsubscribe(ch chan SSEEvent) {
	b.mu.Lock()
	delete(b.clients, ch)
	b.mu.Unlock()
	close(ch)
}

func (b *Broker) broadcast(event, data string) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	ev := SSEEvent{Event: event, Data: data}
	for ch := range b.clients {
		select {
		case ch <- ev:
		default:
		}
	}
}

// ─── Journal (clipboard-event) ───────────────────────────────────────────────

type LogEntry struct {
	Timestamp  string
	StatusIcon string
	StatusText string
	StatusCSS  string
	BgCSS      string
	BorderCSS  string
	Op         string
	Details    string
}

var logTpl = template.Must(template.New("log").Parse(`
<div class="flex items-start space-x-2 font-mono p-2.5 rounded {{.BgCSS}} border {{.BorderCSS}} shadow-sm transition-colors">
  <span class="text-zinc-400 dark:text-zinc-600">[{{.Timestamp}}]</span>
  <span class="{{.StatusCSS}} font-bold">{{.StatusIcon}} {{.StatusText}}</span>
  <span class="text-zinc-700 dark:text-zinc-300 font-semibold">op: {{.Op}}</span>
  <span class="text-zinc-500 dark:text-zinc-400">{{.Details}}</span>
</div>`))

func (b *Broker) BroadcastLog(icon, status, statusCSS, bg, border, op, details string) {
	entry := LogEntry{
		Timestamp:  time.Now().Format("15:04:05"),
		StatusIcon: icon,
		StatusText: status,
		StatusCSS:  statusCSS,
		BgCSS:      bg,
		BorderCSS:  border,
		Op:         op,
		Details:    details,
	}
	var buf strings.Builder
	logTpl.Execute(&buf, entry)
	b.broadcast("clipboard-event", strings.TrimSpace(buf.String()))
}

func (b *Broker) BroadcastDone(op, details string, durMs int64) {
	b.BroadcastLog(
		"✅", "DONE", "text-emerald-600 dark:text-emerald-400",
		"bg-white dark:bg-zinc-900/40",
		"border-zinc-200 dark:border-zinc-900",
		op, fmt.Sprintf("%s (%dms)", details, durMs),
	)
}

func (b *Broker) BroadcastError(op, errMsg string) {
	b.BroadcastLog(
		"❌", "ERROR", "text-red-600 dark:text-red-400",
		"bg-red-50 dark:bg-red-950/10",
		"border-red-200 dark:border-red-900/30",
		op, html.EscapeString(errMsg),
	)
}

func (b *Broker) BroadcastReceived(op string, count int) {
	details := "1 commande détectée"
	if count > 1 {
		details = fmt.Sprintf("%d commandes en pipeline", count)
	}
	b.BroadcastLog(
		"📥", "REÇU", "text-indigo-600 dark:text-indigo-400",
		"bg-indigo-50 dark:bg-indigo-950/10",
		"border-indigo-100 dark:border-indigo-900/30",
		op, details,
	)
}

func (b *Broker) BroadcastObsidian(file string) {
	b.BroadcastLog(
		"🟣", "OBSIDIAN", "text-indigo-600 dark:text-indigo-400",
		"bg-indigo-50 dark:bg-indigo-950/10",
		"border-indigo-100 dark:border-indigo-900/30",
		"obsidian", fmt.Sprintf("Archivé dans %s", html.EscapeString(file)),
	)
}

// ─── HITL (hitl-event) ───────────────────────────────────────────────────────

var hitlTpl = template.Must(template.New("hitl").Parse(`
<div class="border border-amber-300 bg-amber-50/60 dark:border-amber-500/30 dark:bg-amber-950/10 rounded-lg p-4 space-y-3 shadow-sm">
  <div class="flex items-center justify-between">
    <div class="flex items-center space-x-2">
      <span class="px-1.5 py-0.5 rounded bg-amber-500 text-zinc-950 text-[10px] font-bold uppercase tracking-wider animate-pulse">Validation requise</span>
      <span class="text-xs font-semibold text-amber-700 dark:text-amber-400">op: {{.Op}}</span>
    </div>
    <span class="text-[11px] font-medium text-amber-800 dark:text-amber-500 font-mono">{{.Path}}</span>
  </div>
  {{if .Diff}}
  <div class="bg-zinc-900 text-zinc-100 rounded border border-zinc-800 text-[11px] overflow-hidden font-mono shadow-md">
    <div class="bg-zinc-950 px-2 py-1 text-zinc-500 border-b border-zinc-800 text-[10px]">Unified Diff</div>
    <pre class="p-2 overflow-x-auto leading-relaxed">{{.ColoredDiff}}</pre>
  </div>
  {{else if .Content}}
  <div class="bg-zinc-900 text-zinc-100 rounded border border-zinc-800 text-[11px] overflow-hidden font-mono shadow-md">
    <div class="bg-zinc-950 px-2 py-1 text-zinc-500 border-b border-zinc-800 text-[10px]">Contenu ({{.ContentLen}} octets)</div>
    <pre class="p-2 overflow-x-auto leading-relaxed max-h-40">{{.ContentPreview}}</pre>
  </div>
  {{end}}
  <div class="flex items-center justify-between pt-1">
    <div class="text-[10px] text-zinc-500">Auto-reject dans <span class="text-amber-600 dark:text-amber-500 font-bold">{{.TimeoutSecs}}s</span></div>
    <div class="flex space-x-2">
      <button hx-post="/api/reject/{{.ID}}" hx-target="#hitl-container" hx-swap="innerHTML"
              class="px-2.5 py-1 text-xs border border-zinc-300 dark:border-zinc-800 bg-white dark:bg-zinc-900 hover:bg-zinc-50 dark:hover:bg-zinc-800 rounded font-medium text-zinc-600 dark:text-zinc-400 transition-all">
        Rejeter
      </button>
      <button hx-post="/api/approve/{{.ID}}" hx-target="#hitl-container" hx-swap="innerHTML"
              class="px-3 py-1 text-xs bg-amber-500 hover:bg-amber-400 text-zinc-950 font-bold rounded shadow-md transition-all">
        Autoriser (Entrée)
      </button>
    </div>
  </div>
</div>`))

type hitlData struct {
	ID             string
	Op             string
	Path           string
	Diff           string
	ColoredDiff    template.HTML
	Content        string
	ContentLen     int
	ContentPreview string
	TimeoutSecs    int
}

func (b *Broker) BroadcastHITL(cmd *parser.Command, timeoutSecs int) {
	d := hitlData{
		ID:          cmd.ID,
		Op:          cmd.Op,
		Path:        cmd.Path,
		TimeoutSecs: timeoutSecs,
	}

	if cmd.Diff != "" {
		d.Diff = cmd.Diff
		d.ColoredDiff = colorDiff(cmd.Diff)
	} else if cmd.Content != "" {
		d.Content = cmd.Content
		d.ContentLen = len(cmd.Content)
		preview := cmd.Content
		if len(preview) > 500 {
			preview = preview[:500] + "\n... [truncated]"
		}
		d.ContentPreview = html.EscapeString(preview)
	}

	var buf strings.Builder
	hitlTpl.Execute(&buf, d)
	b.broadcast("hitl-event", strings.TrimSpace(buf.String()))
}

func (b *Broker) BroadcastHITLClear() {
	b.broadcast("hitl-event", `<div class="text-center text-xs text-zinc-400 dark:text-zinc-600 py-4 font-sans">Aucune validation en attente</div>`)
}

// ─── Pipeline (pipeline-event) ────────────────────────────────────────────────

var pipelineTpl = template.Must(template.New("pipeline").Parse(`
<div class="border border-indigo-200 dark:border-indigo-500/30 bg-white dark:bg-zinc-900/40 rounded-lg p-3 space-y-2 shadow-sm">
  <div class="flex items-center justify-between">
    <div class="text-[11px] font-bold text-indigo-700 dark:text-indigo-400 uppercase tracking-wider">
      Multi-JSON Pipeline ({{.Total}} actions)
    </div>
    <span class="text-[9px] bg-indigo-100 dark:bg-indigo-950 text-indigo-700 dark:text-indigo-400 px-1.5 py-0.5 rounded font-bold uppercase font-mono">Séquentiel</span>
  </div>
  <div class="space-y-1.5 font-mono text-xs">
    {{range .Steps}}{{.}}{{end}}
  </div>
</div>`))

type pipelineData struct {
	Total int
	Steps []template.HTML
}

func stepHTML(i, total int, op, status string) template.HTML {
	label := fmt.Sprintf("[%d/%d] op: %s", i+1, total, html.EscapeString(op))
	switch status {
	case "done":
		return template.HTML(fmt.Sprintf(`<div class="flex items-center justify-between p-2 bg-zinc-100 dark:bg-zinc-950 rounded border border-zinc-200/40 dark:border-zinc-900">
      <span class="text-emerald-600 dark:text-emerald-400 font-bold">● %s</span>
      <span class="text-[10px] text-zinc-500 font-sans">Succès</span>
    </div>`, label))
	case "running":
		return template.HTML(fmt.Sprintf(`<div class="flex items-center justify-between p-2 bg-amber-50 dark:bg-amber-950/20 border border-amber-300 dark:border-amber-500/20 rounded animate-pulse">
      <span class="text-amber-700 dark:text-amber-400 font-bold">➔ %s</span>
      <span class="px-1.5 py-0.5 rounded bg-amber-500 text-zinc-950 text-[9px] font-bold font-sans uppercase tracking-tight">En cours</span>
    </div>`, label))
	case "error":
		return template.HTML(fmt.Sprintf(`<div class="flex items-center justify-between p-2 bg-red-50 dark:bg-red-950/20 rounded border border-red-200 dark:border-red-900">
      <span class="text-red-600 dark:text-red-400 font-bold">✗ %s</span>
      <span class="text-[10px] text-zinc-500 font-sans">Erreur</span>
    </div>`, label))
	default:
		return template.HTML(fmt.Sprintf(`<div class="flex items-center justify-between p-2 opacity-40 rounded">
      <span class="text-zinc-500">○ %s</span>
      <span class="text-[10px] font-sans">En attente</span>
    </div>`, label))
	}
}

func (b *Broker) BroadcastPipeline(cmds []*parser.Command, statuses []string) {
	steps := make([]template.HTML, len(cmds))
	for i, cmd := range cmds {
		st := "waiting"
		if i < len(statuses) {
			st = statuses[i]
		}
		steps[i] = stepHTML(i, len(cmds), cmd.Op, st)
	}
	var buf strings.Builder
	pipelineTpl.Execute(&buf, pipelineData{Total: len(cmds), Steps: steps})
	b.broadcast("pipeline-event", strings.TrimSpace(buf.String()))
}

func (b *Broker) BroadcastPipelineClear() {
	b.broadcast("pipeline-event", "")
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func colorDiff(diff string) template.HTML {
	var buf strings.Builder
	for _, line := range strings.Split(diff, "\n") {
		var cls string
		switch {
		case strings.HasPrefix(line, "@@"):
			cls = "text-zinc-500"
		case strings.HasPrefix(line, "---") || strings.HasPrefix(line, "+++"):
			cls = "text-zinc-400"
		case strings.HasPrefix(line, "-"):
			cls = "text-red-400 bg-red-950/40"
		case strings.HasPrefix(line, "+"):
			cls = "text-emerald-400 bg-emerald-950/40"
		default:
			cls = "text-zinc-400"
		}
		fmt.Fprintf(&buf, "<span class=%q>%s</span>\n", cls, html.EscapeString(line))
	}
	return template.HTML(buf.String())
}
