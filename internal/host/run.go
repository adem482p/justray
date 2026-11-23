package host

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/godbus/dbus/v5"

	"tray/internal/statusnotifier"
	"tray/internal/ui"
)

type Host struct {
	SocketPath   string
	Conn         *dbus.Conn
	Service      *statusnotifier.Service
	Registry     *statusnotifier.Registry
	Listener     net.Listener
	ReadyFD      int
	closeOnce    sync.Once
	shutdownOnce sync.Once
}

func Run(socketPath string) int {
	os.Setenv("TRAY_SOCKET", socketPath)

	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		fmt.Printf("[tray] cannot connect to session bus: %v\n", err)
		return 1
	}

	reply, err := conn.RequestName("org.kde.StatusNotifierWatcher", dbus.NameFlagDoNotQueue)
	if err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		if err != nil {
			fmt.Printf("[tray] Could not acquire D-Bus name org.kde.StatusNotifierWatcher: reply=%v err=%v\n", reply, err)
		} else {
			fmt.Printf("[tray] Could not acquire D-Bus name org.kde.StatusNotifierWatcher: reply=%v\n", reply)
		}
		conn.Close()
		return 1
	}

	svc := statusnotifier.NewService(conn)
	if err := svc.Export(); err != nil {
		fmt.Printf("[tray] export failed: %v\n", err)
		conn.Close()
		return 1
	}
	reg := svc.Registry()
	reg.StartSignalLoop()

	unique := ""
	for _, n := range conn.Names() {
		if strings.HasPrefix(n, ":") {
			unique = n
			break
		}
	}
	if unique == "" {
		if ns := conn.Names(); len(ns) > 0 {
			unique = ns[0]
		}
	}
	reg.RegisterHost(unique)

	if fi, err := os.Lstat(socketPath); err == nil {
		if fi.Mode()&os.ModeSocket == 0 {
			fmt.Printf("[tray] refusing to replace non-socket path: %s\n", socketPath)
			conn.Close()
			return 1
		}
		if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
			fmt.Printf("[tray] failed to remove existing socket: %v\n", err)
			conn.Close()
			return 1
		}
	} else if !os.IsNotExist(err) {
		fmt.Printf("[tray] failed to stat socket path: %v\n", err)
		conn.Close()
		return 1
	}

	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		fmt.Printf("[tray] socket bind failed: %v\n", err)
		conn.Close()
		return 1
	}
	if err := os.Chmod(socketPath, 0o600); err != nil {
		fmt.Printf("[tray] failed to chmod socket: %v\n", err)
		ln.Close()
		conn.Close()
		return 1
	}

	h := &Host{
		SocketPath: socketPath,
		Conn:       conn,
		Service:    svc,
		Registry:   reg,
		Listener:   ln,
	}
	h.ReadyNotify()
	go socketServer(ln, h)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		signal.Stop(sigCh)
		h.Shutdown()
		ui.EnqueueUICommand("quit")
	}()

	ui.RunUI(h, socketPath)
	signal.Stop(sigCh)
	h.Shutdown()
	return 0
}

func (h *Host) ReadyNotify() {
	v := os.Getenv("TRAY_READY_FD")
	if v == "" {
		return
	}
	fd, err := strconv.Atoi(v)
	if err != nil || fd <= 2 {
		return
	}
	f := os.NewFile(uintptr(fd), "ready")
	if f == nil {
		return
	}
	if _, err := f.WriteString("ready\n"); err == nil {
		_ = f.Close()
		return
	}
	_ = f.Close()
}

func socketServer(ln net.Listener, h *Host) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			// Transient accept failure; avoid busy-looping and keep serving.
			fmt.Printf("[tray] accept error: %v\n", err)
			time.Sleep(100 * time.Millisecond)
			continue
		}
		go handleConn(conn, h)
	}
}
