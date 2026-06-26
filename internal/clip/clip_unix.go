//go:build !windows

package clip

import (
	"context"
	"log"
	"os/exec"
	"strings"
	"time"
)

type backend struct {
	readCmd  string
	readArgs []string
	writeCmd string
	wrtArgs  []string
}

// Ordre : wl-paste (Wayland natif) puis xclip/xsel (X11 / XWayland).
// Sur sway et autres compositors minimalistes, les clipboards Wayland et X11
// sont séparés. VS Code/Electron copie dans X11 clipboard via XWayland.
// On surveille les DEUX et on émet le premier qui change.
var knownBackends = []backend{
	{"wl-paste", []string{"--no-newline"}, "wl-copy", nil},
	{"xclip", []string{"-selection", "clipboard", "-o"}, "xclip", []string{"-selection", "clipboard"}},
	{"xsel", []string{"--clipboard", "--output"}, "xsel", []string{"--clipboard", "--input"}},
}

// DetectBackends retourne les outils clipboard présents dans PATH.
func DetectBackends() []string {
	var found []string
	seen := map[string]bool{}
	for _, b := range knownBackends {
		if seen[b.readCmd] {
			continue
		}
		if _, err := exec.LookPath(b.readCmd); err == nil {
			found = append(found, b.readCmd)
			seen[b.readCmd] = true
		}
	}
	return found
}

// Watch retourne un channel qui émet le texte du clipboard dès qu'il change.
// Surveille Wayland ET X11 indépendamment pour couvrir les sessions mixtes
// (VS Code sous XWayland + navigateur Wayland natif, etc.).
func Watch(ctx context.Context) <-chan string {
	ch := make(chan string, 16)

	available := DetectBackends()
	if len(available) == 0 {
		log.Println("[clip] ERREUR: aucun outil clipboard disponible.")
		log.Println("[clip]   Wayland : sudo pacman -S wl-clipboard  (ou apt install wl-clipboard)")
		log.Println("[clip]   X11     : sudo pacman -S xclip          (ou apt install xclip)")
		close(ch)
		return ch
	}
	log.Printf("[clip] backends actifs: %s (poll 200ms)", strings.Join(available, ", "))

	go func() {
		defer close(ch)
		watchAll(ctx, ch)
	}()
	return ch
}

func watchAll(ctx context.Context, ch chan<- string) {
	type source struct {
		b    backend
		last string
	}

	var sources []source
	for _, b := range knownBackends {
		if _, err := exec.LookPath(b.readCmd); err == nil {
			sources = append(sources, source{b: b})
		}
	}

	// lastSent évite d'émettre deux fois le même contenu si Wayland et X11
	// sont synchronisés (clipboard manager actif, GNOME, KDE…).
	var lastSent string

	t := time.NewTicker(200 * time.Millisecond)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			for i := range sources {
				out, err := exec.Command(sources[i].b.readCmd, sources[i].b.readArgs...).Output()
				if err != nil {
					continue
				}
				s := strings.TrimRight(string(out), "\r\n")
				if s == "" || s == sources[i].last {
					continue
				}
				sources[i].last = s
				if s == lastSent {
					// Même contenu vu depuis une autre source — pas de doublon.
					continue
				}
				lastSent = s
				select {
				case ch <- s:
				default:
				}
			}
		}
	}
}

func HasChanged() bool { return true }

// Read retourne le texte du clipboard depuis le premier outil disponible.
func Read() (string, error) {
	var lastErr error
	for _, b := range knownBackends {
		out, err := exec.Command(b.readCmd, b.readArgs...).Output()
		if err != nil {
			lastErr = err
			continue
		}
		// Ne pas retourner tôt sur résultat vide : wl-paste peut retourner ""
		// si le compositor n'a rien copié en Wayland, mais xclip aurait le contenu.
		s := strings.TrimRight(string(out), "\r\n")
		if s != "" {
			return s, nil
		}
	}
	return "", lastErr
}

// Write écrit dans tous les clipboards disponibles pour maximiser la compatibilité
// (réponse visible depuis Wayland ET X11).
func Write(s string) error {
	var firstErr error
	wrote := false
	for _, b := range knownBackends {
		if _, err := exec.LookPath(b.writeCmd); err != nil {
			continue
		}
		args := b.wrtArgs
		cmd := exec.Command(b.writeCmd, args...)
		cmd.Stdin = strings.NewReader(s)
		if err := cmd.Run(); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		wrote = true
	}
	if wrote {
		return nil
	}
	return firstErr
}
