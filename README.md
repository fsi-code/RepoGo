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


Bonne démo. Ce JSON est parfaitement formé pour notre daemon. Voici exactement ce qui va se passer quand tu le copies :

---
1 — Parser détecte {"_clipdev": "1.0", ...} dans le clipboard → extrait la commande op: write

2 — Dispatcher voit write dans require_approval → bloque l'exécution

3 — UI reçoit l'événement SSE hitl-event et affiche :

┌─ Validation requise ─────────────── main.go ─┐
│                                               │
│  Contenu (152 octets)                         │
│  ┌─────────────────────────────────────────┐  │
│  │ package main                            │  │
│  │                                         │  │
│  │ import (                                │  │
│  │     "fmt"                               │  │
│  │     "net/http"                          │  │
│  │ )                                       │  │
│  │ ...                                     │  │
│  └─────────────────────────────────────────┘  │
│                                               │
│  Auto-reject dans 30s    [Rejeter] [Autoriser]│
└───────────────────────────────────────────────┘

4a — Si tu cliques Autoriser → ops.Write() crée main.go (mode create échoue si le fichier existe déjà), réponse dans clipboard :
{
"_clipdev": "1.0",
"id": "w1",
"ok": true,
"op": "write",
"result": "Written 152 bytes to main.go",
"duration_ms": 2
}

4b — Si tu cliques Rejeter (ou timeout 30s) :
{
"_clipdev": "1.0",
"id": "w1",
"ok": false,
"op": "write",
"error": "operation rejected by user",
"code": "REJECTED"
}

---
Un seul point à surveiller pour ton test : le workdir configuré dans config.toml. Si tu lances le daemon depuis C:\MonProjet, main.go sera
créé là. Si tu veux tester sans toucher à de vrais fichiers, ajoute "dry_run": true :

{
"_clipdev": "1.0",
"op": "write",
"id": "w1",
"path": "main.go",
"mode": "create",
"content": "...",
"dry_run": true
}

→ Pas de HITL, pas d'écriture, réponse immédiate : "Would write 152 bytes to main.go (mode: create)".




