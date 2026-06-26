//go:build windows

package main

import "log"

func logClipboardDiag() {
	log.Println("  clipboard: user32.dll (AddClipboardFormatListener, natif)")
}
