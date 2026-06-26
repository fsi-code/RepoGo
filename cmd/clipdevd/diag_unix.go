//go:build !windows

package main

import (
	"log"
	"strings"

	"clipdev/internal/clip"
)

func logClipboardDiag() {
	backends := clip.DetectBackends()
	if len(backends) == 0 {
		log.Println("  clipboard: AUCUN outil clipboard trouvé !")
		log.Println("             Wayland → sudo pacman -S wl-clipboard")
		log.Println("             X11     → sudo pacman -S xclip")
		return
	}
	log.Printf("  clipboard: %s", strings.Join(backends, ", "))

	s, err := clip.Read()
	switch {
	case err != nil:
		log.Printf("  clipboard: lecture test ÉCHOUÉE: %v", err)
	case s == "":
		log.Println("  clipboard: lecture test OK (presse-papier vide)")
	default:
		preview := s
		if len(preview) > 60 {
			preview = preview[:60] + "…"
		}
		log.Printf("  clipboard: lecture test OK — %q", preview)
	}
}
