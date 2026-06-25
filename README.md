# RepoGo



Structure créée — 19 fichiers Go + UI

ClipControl/
├── cmd/clipdevd/main.go          ← point d'entrée, flags --config --workdir --port
├── internal/
│   ├── config/config.go          ← chargement config.toml + defaults
│   ├── sandbox/sandbox.go        ← vérification stricte filepath (anti PATH_ESCAPE)
│   ├── parser/parser.go          ← extraction multi-JSON depuis texte libre
│   ├── hitl/hitl.go              ← approbation HITL avec timeout auto-reject
│   ├── broker/broker.go          ← SSE broadcast + templates HTML (journal/HITL/pipeline)
│   ├── dispatch/dispatch.go      ← routeur ops + gate HITL + hook Obsidian
│   ├── ops/
│   │   ├── ops.go                ← types Response, Success, Failure, DryRunResult
│   │   ├── read.go               ← lecture fichier avec plage de lignes
│   │   ├── grep.go               ← regex récursive multi-ext avec contexte
│   │   ├── find.go               ← find avec filtre glob
│   │   ├── write.go              ← create/overwrite/append + blacklist .toml/.env
│   │   ├── patch.go              ← application via `patch -p1`
│   │   ├── git.go                ← git (allowlist: status/diff/log/add/commit…)
│   │   ├── gotool.go             ← go build/test/vet/fmt + mod tidy + timeout
│   │   ├── tree.go               ← arborescence Go pure (pas de dépendance)
│   │   └── python.go             ← python3 avec timeout sandboxé
│   ├── watcher/watcher.go        ← poll 200ms, auto-détect wl-paste/xclip/xsel
│   └── server/
│       ├── server.go             ← HTTP + SSE + HTMX endpoints
│       └── static/index.html     ← UI Tailwind+Alpine+HTMX (embed Go)
├── config.toml
└── go.mod (1 seule dépendance : BurntSushi/toml)

Pour lancer :
./clipdevd --workdir /chemin/vers/projet --port 8080
# Puis ouvrir http://localhost:8080
