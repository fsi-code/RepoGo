package server

import (
	"embed"
	"encoding/json"
	"fmt"
	"html"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"clipdev/internal/broker"
	"clipdev/internal/clip"
	"clipdev/internal/config"
	"clipdev/internal/dispatch"
	"clipdev/internal/hitl"
	"clipdev/internal/sandbox"
)

//go:embed static
var staticFS embed.FS

type Server struct {
	cfg    *config.Config
	broker *broker.Broker
	hitl   *hitl.Manager
	disp   *dispatch.Dispatcher
	sb     *sandbox.Sandbox
	mux    *http.ServeMux
}

func New(cfg *config.Config, b *broker.Broker, h *hitl.Manager, d *dispatch.Dispatcher) *Server {
	s := &Server{
		cfg:    cfg,
		broker: b,
		hitl:   h,
		disp:   d,
		sb:     sandbox.New(cfg.Workdir),
		mux:    http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) Start() {
	addr := fmt.Sprintf(":%d", s.cfg.Port)
	log.Printf("UI available at http://localhost%s", addr)
	if err := http.ListenAndServe(addr, s.mux); err != nil {
		log.Fatalf("server: %v", err)
	}
}

func (s *Server) routes() {
	// Static UI
	sub, _ := fs.Sub(staticFS, "static")
	s.mux.Handle("/", http.FileServer(http.FS(sub)))

	// SSE stream
	s.mux.HandleFunc("/api/stream", s.handleSSE)

	// HITL
	s.mux.HandleFunc("/api/approve/", s.handleApprove)
	s.mux.HandleFunc("/api/reject/", s.handleReject)

	// Stop
	s.mux.HandleFunc("/api/stop", s.handleStop)

	// Config (GET = form pré-rempli, POST = mise à jour)
	s.mux.HandleFunc("/api/config/obsidian", s.handleObsidianConfig)
	// Publish manual Obsidian session marker
	s.mux.HandleFunc("/api/publish/obsidian", s.handlePublishObsidian)

	// Quick commands
	s.mux.HandleFunc("/api/quick/grep", s.handleQuickGrep)
	s.mux.HandleFunc("/api/quick/tree", s.handleQuickTree)
	s.mux.HandleFunc("/api/quick/git-status", s.handleQuickGitStatus)

	// File tree + viewer for left panel
	s.mux.HandleFunc("/api/files", s.handleFiles)
	s.mux.HandleFunc("/api/read", s.handleFileRead)

	// Daemon status
	s.mux.HandleFunc("/api/status", s.handleStatus)
}

// ─── SSE ──────────────────────────────────────────────────────────────────────

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	ch := s.broker.Subscribe()
	defer s.broker.Unsubscribe(ch)

	// Send initial heartbeat
	fmt.Fprintf(w, ": heartbeat\n\n")
	flusher.Flush()

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			fmt.Fprintf(w, ": heartbeat\n\n")
			flusher.Flush()
		case ev, ok := <-ch:
			if !ok {
				return
			}
			// SSE spec: each line of data must be prefixed with "data: "
			lines := strings.Split(ev.Data, "\n")
			fmt.Fprintf(w, "event: %s\n", ev.Event)
			for _, line := range lines {
				fmt.Fprintf(w, "data: %s\n", line)
			}
			fmt.Fprintf(w, "\n")
			flusher.Flush()
		}
	}
}

// ─── HITL ─────────────────────────────────────────────────────────────────────

func (s *Server) handleApprove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/approve/")
	if s.hitl.Approve(id) {
		s.broker.BroadcastLog("✅", "APPROUVÉ", "text-emerald-600 dark:text-emerald-400",
			"bg-white dark:bg-zinc-900/40", "border-zinc-200 dark:border-zinc-900",
			"hitl", "Opération autorisée par l'utilisateur")
	}
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, `<div class="text-center text-xs text-zinc-400 dark:text-zinc-600 py-4 font-sans">Aucune validation en attente</div>`)
}

func (s *Server) handleReject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/reject/")
	if s.hitl.Reject(id) {
		s.broker.BroadcastLog("🚫", "REJETÉ", "text-red-600 dark:text-red-400",
			"bg-red-50 dark:bg-red-950/10", "border-red-200 dark:border-red-900/30",
			"hitl", "Opération refusée par l'utilisateur")
	}
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, `<div class="text-center text-xs text-zinc-400 dark:text-zinc-600 py-4 font-sans">Aucune validation en attente</div>`)
}

// ─── Stop ─────────────────────────────────────────────────────────────────────

func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	log.Println("stop requested via UI")
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, `<span class="text-red-500 text-xs">Arrêt en cours...</span>`)
	go func() {
		time.Sleep(300 * time.Millisecond)
		os.Exit(0)
	}()
}

// ─── Config ───────────────────────────────────────────────────────────────────

func (s *Server) handleObsidianConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		// Retourne le formulaire Obsidian pré-rempli avec les valeurs actuelles du config.
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, s.obsidianFormHTML())
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if v := r.FormValue("obsidian_vault_path"); v != "" {
		s.cfg.Obsidian.VaultPath = v
	}
	if f := r.FormValue("obsidian_file"); f != "" {
		s.cfg.Obsidian.File = f
	}
	if t := r.FormValue("obsidian_title"); t != "" {
		s.cfg.Obsidian.Title = t
	}
	if c := r.FormValue("obsidian_category"); c != "" {
		s.cfg.Obsidian.Category = c
	}
	if m := r.FormValue("obsidian_write_mode"); m != "" {
		s.cfg.Obsidian.WriteMode = m
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) obsidianFormHTML() string {
	obs := s.cfg.Obsidian
	cats := []struct{ val, label string }{
		{"Dev", "💻 Dev / Conception"},
		{"Fix", "🔧 Fix / Debug"},
		{"Refactor", "♻️ Refactorisation"},
		{"Architecture", "📐 Architecture"},
	}
	var catOpts strings.Builder
	for _, c := range cats {
		sel := ""
		if obs.Category == c.val {
			sel = ` selected`
		}
		fmt.Fprintf(&catOpts, `<option value="%s"%s>%s</option>`, c.val, sel, c.label)
	}
	appendChecked, newChecked := "", ""
	if obs.WriteMode == "new" {
		newChecked = " checked"
	} else {
		appendChecked = " checked"
	}
	inputClass := `w-full px-2.5 py-1.5 text-xs rounded bg-white dark:bg-zinc-900 border border-zinc-200 dark:border-zinc-800 font-mono text-zinc-800 dark:text-zinc-200 focus:outline-none focus:border-indigo-500 transition-all`
	labelClass := `block text-[10px] text-zinc-500 dark:text-zinc-400 uppercase font-bold tracking-tight mb-1`

	return fmt.Sprintf(`
<div>
  <label class="%s">Vault Obsidian</label>
  <input type="text" name="obsidian_vault_path" value="%s" class="%s" placeholder="/home/…/Obsidian Vault/">
</div>
<div>
  <label class="%s">Nom du fichier MD</label>
  <input type="text" name="obsidian_file" value="%s" class="%s">
</div>
<div>
  <label class="%s">Titre par défaut</label>
  <input type="text" name="obsidian_title" value="%s" class="%s">
</div>
<div>
  <label class="%s">Catégorie (Metadata)</label>
  <select name="obsidian_category" class="%s">%s</select>
</div>
<div>
  <label class="%s">Stratégie d'écriture</label>
  <div class="grid grid-cols-2 gap-2">
    <label class="flex items-center justify-center p-1.5 rounded border text-xs cursor-pointer font-sans transition-all"
           x-bind:class="obsidianMode==='append'?'bg-indigo-500/10 border-indigo-500 text-indigo-600 dark:text-indigo-400 font-semibold':'border-zinc-200 dark:border-zinc-800 text-zinc-400'"
           @click="obsidianMode='append'">
      <input type="radio" name="obsidian_write_mode" value="append" class="hidden"%s> 📝 Append
    </label>
    <label class="flex items-center justify-center p-1.5 rounded border text-xs cursor-pointer font-sans transition-all"
           x-bind:class="obsidianMode==='new'?'bg-indigo-500/10 border-indigo-500 text-indigo-600 dark:text-indigo-400 font-semibold':'border-zinc-200 dark:border-zinc-800 text-zinc-400'"
           @click="obsidianMode='new'">
      <input type="radio" name="obsidian_write_mode" value="new" class="hidden"%s> 🆕 New File
    </label>
  </div>
</div>`,
		labelClass, html.EscapeString(obs.VaultPath), inputClass,
		labelClass, html.EscapeString(obs.File), inputClass,
		labelClass, html.EscapeString(obs.Title), inputClass,
		labelClass, inputClass, catOpts.String(),
		labelClass,
		appendChecked, newChecked,
	)
}

// handlePublishObsidian insère manuellement un encart bleu de session dans le vault.
func (s *Server) handlePublishObsidian(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	label := r.FormValue("label")
	if label == "" {
		label = "📌 Publication manuelle"
	}
	w.Header().Set("Content-Type", "text/html")
	if err := s.disp.PublishSession(label); err != nil {
		fmt.Fprintf(w, `<span class="text-red-500 text-xs">%s</span>`, html.EscapeString(err.Error()))
		return
	}
	fmt.Fprintf(w, `<span class="text-sky-500 dark:text-sky-400 text-xs font-medium">✅ Encart publié dans %s</span>`,
		html.EscapeString(s.cfg.Obsidian.File))
}

// ─── Quick commands ───────────────────────────────────────────────────────────

func (s *Server) handleQuickGrep(w http.ResponseWriter, r *http.Request) {
	template := `{"_clipdev":"1.0","op":"grep","id":"quick-grep","pattern":"YOUR_PATTERN","path":".","ext":[".go"],"context":3}`
	writeClipboard(template)
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, flash("🔍 Template grep injecté dans le clipboard"))
}

func (s *Server) handleQuickTree(w http.ResponseWriter, r *http.Request) {
	treecmd := map[string]interface{}{
		"_clipdev": "1.0", "op": "tree", "id": "quick-tree",
		"path": ".", "depth": 4,
	}
	data, _ := json.Marshal(treecmd)
	writeClipboard(string(data))
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, flash("🌲 Commande tree injectée dans le clipboard"))
}

func (s *Server) handleQuickGitStatus(w http.ResponseWriter, r *http.Request) {
	gitcmd := map[string]interface{}{
		"_clipdev": "1.0", "op": "git", "id": "quick-git-status", "sub": "status",
	}
	data, _ := json.Marshal(gitcmd)
	writeClipboard(string(data))
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, flash("🌿 Commande git status injectée dans le clipboard"))
}

func flash(msg string) string {
	return fmt.Sprintf(`<span class="text-emerald-600 dark:text-emerald-400 text-xs font-medium">%s</span>`, msg)
}

// ─── File tree ────────────────────────────────────────────────────────────────

func (s *Server) handleFiles(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	var buf strings.Builder
	fmt.Fprintf(&buf, `<div class="px-2 py-1 text-[10px] text-zinc-400 bg-zinc-100 dark:bg-zinc-900/50 rounded mb-2">Workdir: <span class="text-zinc-700 dark:text-zinc-300">%s</span></div>`, s.cfg.Workdir)
	walkDir(&buf, s.cfg.Workdir, s.cfg.Workdir, 0, 3)
	fmt.Fprint(w, buf.String())
}

func walkDir(buf *strings.Builder, root, path string, depth, maxDepth int) {
	if depth >= maxDepth {
		return
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.Name() == ".git" || e.Name() == "vendor" || e.Name() == "node_modules" {
			continue
		}
		rel, _ := filepath.Rel(root, filepath.Join(path, e.Name()))
		relURL := strings.ReplaceAll(rel, string(filepath.Separator), "/")
		indent := strings.Repeat("pl-4 ", depth+1)
		if e.IsDir() {
			fmt.Fprintf(buf, `<div class="%sborder-l border-zinc-200 dark:border-zinc-800/60 ml-2">`, indent)
			fmt.Fprintf(buf, `<div class="flex items-center space-x-1 py-1 text-zinc-800 dark:text-zinc-300">📁 %s</div>`, html.EscapeString(e.Name()))
			walkDir(buf, root, filepath.Join(path, e.Name()), depth+1, maxDepth)
			fmt.Fprintf(buf, `</div>`)
		} else {
			fmt.Fprintf(buf, `<div class="%sborder-l border-zinc-200 dark:border-zinc-800/60 ml-2">`, indent)
			fmt.Fprintf(buf,
				`<button hx-get="/api/read?path=%s" hx-target="#file-viewer" hx-swap="innerHTML"`+
					` onclick="document.getElementById('file-modal-backdrop').classList.remove('hidden')"`+
					` class="flex items-center space-x-1.5 py-0.5 w-full text-left hover:text-indigo-600 dark:hover:text-indigo-400 truncate">📄 %s</button>`,
				url.QueryEscape(relURL), html.EscapeString(e.Name()))
			fmt.Fprintf(buf, `</div>`)
		}
	}
}

// ─── File viewer ──────────────────────────────────────────────────────────────

func (s *Server) handleFileRead(w http.ResponseWriter, r *http.Request) {
	relPath := r.URL.Query().Get("path")
	if relPath == "" {
		http.Error(w, "missing path", http.StatusBadRequest)
		return
	}
	absPath, err := s.sb.Resolve(relPath)
	if err != nil {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, fileViewerError("Accès refusé : "+html.EscapeString(err.Error())))
		return
	}
	info, err := os.Stat(absPath)
	if err != nil || info.IsDir() {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, fileViewerError("Fichier introuvable : "+html.EscapeString(relPath)))
		return
	}

	const maxBytes = 512 * 1024
	f, err := os.Open(absPath)
	if err != nil {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, fileViewerError("Erreur lecture : "+html.EscapeString(err.Error())))
		return
	}
	defer f.Close()

	raw := make([]byte, maxBytes+1)
	n, _ := f.Read(raw)
	truncated := n > maxBytes
	if truncated {
		n = maxBytes
	}
	content := strings.TrimRight(string(raw[:n]), "\n")
	lines := strings.Split(content, "\n")
	sizeKB := float64(info.Size()) / 1024
	lang := extToLang(filepath.Ext(absPath))

	var sb strings.Builder

	// Header
	fmt.Fprintf(&sb,
		`<div class="flex items-center justify-between px-4 py-3 border-b border-zinc-200 dark:border-zinc-800 shrink-0">
  <div class="flex items-center space-x-2 min-w-0">
    <span class="text-indigo-500 dark:text-indigo-400 shrink-0">📄</span>
    <span class="font-mono text-xs text-zinc-700 dark:text-zinc-300 truncate" title="%s">%s</span>
  </div>
  <div class="flex items-center space-x-3 shrink-0 ml-3">
    <span class="text-[10px] text-zinc-400 dark:text-zinc-500">%d lignes · %.1f KB</span>
    <button onclick="document.getElementById('file-modal-backdrop').classList.add('hidden')"
            class="p-1 rounded hover:bg-zinc-100 dark:hover:bg-zinc-800 text-zinc-400 hover:text-zinc-700 dark:hover:text-zinc-200 transition-colors text-sm leading-none">✕</button>
  </div>
</div>`,
		html.EscapeString(relPath), html.EscapeString(relPath), len(lines), sizeKB)

	if truncated {
		fmt.Fprintf(&sb,
			`<div class="px-4 py-1 bg-amber-50 dark:bg-amber-950/20 border-b border-amber-200 dark:border-amber-900/40 text-[10px] text-amber-600 dark:text-amber-500">
  ⚠️ Fichier tronqué — seuls les premiers 512 KB sont affichés
</div>`)
	}

	// Line numbers column (aligned with the code pre via same font/size/leading)
	var nums strings.Builder
	for i := range lines {
		fmt.Fprintf(&nums, "%d\n", i+1)
	}

	// Code block — Highlight.js processes <code class="language-X"> after swap
	fmt.Fprintf(&sb,
		`<div class="overflow-auto flex-1 min-h-0 flex text-xs leading-5">
  <pre class="select-none text-right pr-4 pl-3 py-3 m-0 shrink-0 text-zinc-300 dark:text-zinc-700 border-r border-zinc-100 dark:border-zinc-900">%s</pre>
  <pre class="flex-1 py-3 px-4 m-0 overflow-x-auto"><code class="language-%s">%s</code></pre>
</div>`,
		nums.String(), lang, html.EscapeString(content))

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, sb.String())
}

// extToLang maps common file extensions to Highlight.js language identifiers.
func extToLang(ext string) string {
	m := map[string]string{
		".go":   "go",
		".js":   "javascript",
		".ts":   "typescript",
		".jsx":  "javascript",
		".tsx":  "typescript",
		".py":   "python",
		".rs":   "rust",
		".c":    "c",
		".cpp":  "cpp",
		".h":    "c",
		".java": "java",
		".rb":   "ruby",
		".sh":   "bash",
		".bash": "bash",
		".zsh":  "bash",
		".fish": "bash",
		".json": "json",
		".yaml": "yaml",
		".yml":  "yaml",
		".toml": "ini",
		".md":   "markdown",
		".html": "xml",
		".xml":  "xml",
		".css":  "css",
		".sql":  "sql",
		".diff": "diff",
		".patch": "diff",
	}
	if l, ok := m[strings.ToLower(ext)]; ok {
		return l
	}
	return "plaintext"
}

func fileViewerError(msg string) string {
	return fmt.Sprintf(
		`<div class="flex items-center justify-between px-4 py-3 border-b border-zinc-200 dark:border-zinc-800">
  <span class="text-red-500 text-xs">%s</span>
  <button onclick="document.getElementById('file-modal-backdrop').classList.add('hidden')"
          class="p-1 rounded hover:bg-zinc-100 dark:hover:bg-zinc-800 text-zinc-400 hover:text-zinc-600">✕</button>
</div>`, msg)
}

// ─── Status ───────────────────────────────────────────────────────────────────

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":      true,
		"workdir": s.cfg.Workdir,
		"port":    s.cfg.Port,
	})
}

// ─── clipboard write (used by quick commands) ─────────────────────────────────

func writeClipboard(s string) {
	_ = clip.Write(s)
}
