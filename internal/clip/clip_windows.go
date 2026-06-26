//go:build windows

package clip

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procOpenClipboard             = user32.NewProc("OpenClipboard")
	procCloseClipboard            = user32.NewProc("CloseClipboard")
	procGetClipboardData          = user32.NewProc("GetClipboardData")
	procSetClipboardData          = user32.NewProc("SetClipboardData")
	procEmptyClipboard            = user32.NewProc("EmptyClipboard")
	procGetClipboardSequenceNumber = user32.NewProc("GetClipboardSequenceNumber")
	procGlobalAlloc               = kernel32.NewProc("GlobalAlloc")
	procGlobalFree                = kernel32.NewProc("GlobalFree")
	procGlobalLock                = kernel32.NewProc("GlobalLock")
	procGlobalUnlock              = kernel32.NewProc("GlobalUnlock")

	// Pour le message loop / AddClipboardFormatListener
	procCreateWindowExW              = user32.NewProc("CreateWindowExW")
	procDestroyWindow                = user32.NewProc("DestroyWindow")
	procDefWindowProcW               = user32.NewProc("DefWindowProcW")
	procRegisterClassExW             = user32.NewProc("RegisterClassExW")
	procGetMessageW                  = user32.NewProc("GetMessageW")
	procDispatchMessageW             = user32.NewProc("DispatchMessageW")
	procPostMessageW                 = user32.NewProc("PostMessageW")
	procAddClipboardFormatListener   = user32.NewProc("AddClipboardFormatListener")
	procRemoveClipboardFormatListener = user32.NewProc("RemoveClipboardFormatListener")
	procGetModuleHandleW             = kernel32.NewProc("GetModuleHandleW")
)

const (
	cfUnicodeText     = 13
	gmemMoveable      = 0x0002
	wmClipboardUpdate = 0x031D
	wmQuit            = 0x0012
	hwndMessageOnly   = ^uintptr(2) // HWND_MESSAGE = (HWND)(-3)
)

// wndClassExW correspond à WNDCLASSEXW (64-bit).
type wndClassExW struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       uintptr
}

// msgT correspond à MSG (64-bit) — layout validé contre x/sys/windows.
type msgT struct {
	hwnd    uintptr
	message uint32
	_       uint32  // padding pour aligner wParam sur 8 octets
	wParam  uintptr
	lParam  uintptr
	time    uint32
	ptX     int32
	ptY     int32
	_       uint32 // padding final
}

var mu sync.Mutex
var lastSeq atomic.Uint32

// Watch retourne un channel qui émet le texte du presse-papier à chaque changement.
// Utilise AddClipboardFormatListener + WM_CLIPBOARDUPDATE : zéro polling,
// latence ~0ms (notification OS synchrone).
// En cas d'échec d'initialisation, bascule sur un polling de secours à 50ms.
func Watch(ctx context.Context) <-chan string {
	ch := make(chan string, 16)
	go func() {
		defer close(ch)
		if err := runMessageLoop(ctx, ch); err != nil {
			fallbackPoll(ctx, ch)
		}
	}()
	return ch
}

func runMessageLoop(ctx context.Context, ch chan<- string) error {
	// Le message loop Windows doit tourner sur un OS thread fixe.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	hInst, _, _ := procGetModuleHandleW.Call(0)
	className, err := syscall.UTF16PtrFromString("clipdev_watcher_class")
	if err != nil {
		return fmt.Errorf("UTF16PtrFromString: %w", err)
	}

	// WndProc : appelé par Windows sur ce même OS thread via DispatchMessageW.
	// Pas besoin de mutex — on est déjà sur le thread locké.
	wndProc := syscall.NewCallback(func(hwnd, msg, wParam, lParam uintptr) uintptr {
		if msg == wmClipboardUpdate {
			if s := readOnCurrentThread(); s != "" {
				select {
				case ch <- s:
				default: // le watcher est occupé, on perd l'événement (acceptable)
				}
			}
		}
		r, _, _ := procDefWindowProcW.Call(hwnd, msg, wParam, lParam)
		return r
	})

	wc := wndClassExW{
		lpfnWndProc:   wndProc,
		hInstance:     hInst,
		lpszClassName: className,
	}
	wc.cbSize = uint32(unsafe.Sizeof(wc))
	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	hwnd, _, _ := procCreateWindowExW.Call(
		0,                                  // dwExStyle
		uintptr(unsafe.Pointer(className)), // lpClassName
		0, 0,                               // lpWindowName, dwStyle
		0, 0, 0, 0,                         // x, y, w, h
		hwndMessageOnly, 0, hInst, 0,       // HWND_MESSAGE = fenêtre sans UI
	)
	if hwnd == 0 {
		return fmt.Errorf("CreateWindowExW: returned NULL")
	}
	defer procDestroyWindow.Call(hwnd)

	r, _, _ := procAddClipboardFormatListener.Call(hwnd)
	if r == 0 {
		return fmt.Errorf("AddClipboardFormatListener: returned 0")
	}
	defer procRemoveClipboardFormatListener.Call(hwnd)

	// Quand le contexte est annulé, on envoie WM_QUIT pour débloquer GetMessageW.
	go func() {
		<-ctx.Done()
		procPostMessageW.Call(hwnd, wmQuit, 0, 0)
	}()

	var m msgT
	for {
		r, _, _ = procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), hwnd, 0, 0)
		// GetMessageW retourne 0 sur WM_QUIT, -1 (^0) sur erreur.
		if r == 0 || r == ^uintptr(0) {
			break
		}
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	return nil
}

// readOnCurrentThread lit le presse-papier sans mutex ni LockOSThread —
// uniquement appelé depuis le WndProc, lui-même sur le thread locké du message loop.
func readOnCurrentThread() string {
	r, _, _ := procOpenClipboard.Call(0)
	if r == 0 {
		return ""
	}
	defer procCloseClipboard.Call()

	h, _, _ := procGetClipboardData.Call(cfUnicodeText)
	if h == 0 {
		return ""
	}
	p, _, _ := procGlobalLock.Call(h)
	if p == 0 {
		return ""
	}
	defer procGlobalUnlock.Call(h)

	utf16 := (*[1 << 20]uint16)(unsafe.Pointer(p))
	n := 0
	for utf16[n] != 0 {
		n++
	}
	return syscall.UTF16ToString(utf16[:n])
}

// fallbackPoll est utilisé si AddClipboardFormatListener échoue (rare).
func fallbackPoll(ctx context.Context, ch chan<- string) {
	var last string
	t := time.NewTicker(50 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if s, err := Read(); err == nil && s != last {
				last = s
				select {
				case ch <- s:
				default:
				}
			}
		}
	}
}

func Read() (string, error) {
	mu.Lock()
	defer mu.Unlock()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	r, _, err := procOpenClipboard.Call(0)
	if r == 0 {
		return "", fmt.Errorf("OpenClipboard: %w", err)
	}
	defer procCloseClipboard.Call()

	h, _, _ := procGetClipboardData.Call(cfUnicodeText)
	if h == 0 {
		return "", nil
	}
	p, _, err := procGlobalLock.Call(h)
	if p == 0 {
		return "", fmt.Errorf("GlobalLock: %w", err)
	}
	defer procGlobalUnlock.Call(h)

	utf16 := (*[1 << 20]uint16)(unsafe.Pointer(p))
	n := 0
	for utf16[n] != 0 {
		n++
	}
	return syscall.UTF16ToString(utf16[:n]), nil
}

func Write(s string) error {
	mu.Lock()
	defer mu.Unlock()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	r, _, err := procOpenClipboard.Call(0)
	if r == 0 {
		return fmt.Errorf("OpenClipboard: %w", err)
	}
	defer procCloseClipboard.Call()

	r, _, err = procEmptyClipboard.Call()
	if r == 0 {
		return fmt.Errorf("EmptyClipboard: %w", err)
	}

	encoded := syscall.StringToUTF16(s)
	size := uintptr(len(encoded) * 2)

	h, _, err := procGlobalAlloc.Call(gmemMoveable, size)
	if h == 0 {
		return fmt.Errorf("GlobalAlloc: %w", err)
	}
	p, _, err := procGlobalLock.Call(h)
	if p == 0 {
		procGlobalFree.Call(h)
		return fmt.Errorf("GlobalLock: %w", err)
	}
	copy((*[1 << 20]uint16)(unsafe.Pointer(p))[:], encoded)
	procGlobalUnlock.Call(h)

	r, _, err = procSetClipboardData.Call(cfUnicodeText, h)
	if r == 0 {
		procGlobalFree.Call(h)
		return fmt.Errorf("SetClipboardData: %w", err)
	}
	return nil
}

// DetectBackends retourne nil sur Windows — l'accès au clipboard passe par
// user32.dll directement, sans outil externe.
func DetectBackends() []string { return nil }

// HasChanged est conservé pour un éventuel usage externe.
// Avec Watch(), il n'est plus nécessaire dans le watcher.
func HasChanged() bool {
	r, _, _ := procGetClipboardSequenceNumber.Call()
	seq := uint32(r)
	old := lastSeq.Swap(seq)
	return seq != old
}
