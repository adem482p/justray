package ui

/*
#cgo pkg-config: Qt6Widgets Qt6Gui Qt6Core Qt6DBus
#cgo CXXFLAGS: -std=c++17
#cgo LDFLAGS: -lstdc++
extern void runQtUI();
extern void uiQuit();
extern void uiShow();
extern void uiHide();
extern void uiToggle();
*/
import "C"

import (
	"os"
	"sync/atomic"
)

var uiCommands = make(chan string, 16)
var quitRequested atomic.Bool

func EnqueueUICommand(cmd string) {
	if cmd == "quit" {
		quitRequested.Store(true)
		C.uiQuit()
		return
	}
	select {
	case uiCommands <- cmd:
	default:
	}
}

func RunUI(h interface{}, sock string) {
	os.Setenv("TRAY_SOCKET", sock)
	go func() {
		for cmd := range uiCommands {
			switch cmd {
			case "quit":
				C.uiQuit()
			case "show":
				C.uiShow()
			case "hide":
				C.uiHide()
			case "toggle":
				C.uiToggle()
			}
		}
	}()
	C.runQtUI()
}
