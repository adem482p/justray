package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"

	"tray/internal/dbusmenu"
	"tray/internal/dbusutil"
	"tray/internal/statusnotifier"
	"tray/internal/ui"
)

const (
	hostReadBufLimit      = 1 << 20 // 1 MiB per connection line buffer
	hostReadDeadline      = 30 * time.Second
	hostDBusCallTimeout   = 2 * time.Second
	hostWatchWriteTimeout = 5 * time.Second
)

func handleConn(conn net.Conn, h *Host) {
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(hostReadDeadline))
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 4096)
	for {
		n, err := conn.Read(tmp)
		if n > 0 {
			_ = conn.SetReadDeadline(time.Now().Add(hostReadDeadline))
			if len(buf)+n > hostReadBufLimit {
				_ = writeResp(conn, map[string]any{"error": "request too large"})
				return
			}
			buf = append(buf, tmp[:n]...)
			for {
				idx := indexOfNewline(buf)
				if idx < 0 {
					break
				}
				line := strings.TrimSpace(string(buf[:idx]))
				buf = buf[idx+1:]
				if line == "" {
					continue
				}
				var req map[string]any
				if err := json.Unmarshal([]byte(line), &req); err != nil {
					if werr := writeResp(conn, map[string]any{"error": "bad json"}); werr != nil {
						return
					}
					continue
				}
				if req == nil {
					if werr := writeResp(conn, map[string]any{"error": "bad request"}); werr != nil {
						return
					}
					continue
				}
				cmd, _ := req["cmd"].(string)
				if cmd == "watch" {
					handleWatch(conn, h)
					return
				}
				if cmd == "icon" {
					handleIcon(conn, h, req)
					return
				}
				resp := dispatchCmd(cmd, req, h)
				if werr := writeResp(conn, resp); werr != nil {
					return
				}
			}
		}
		if err != nil {
			return
		}
	}
}

func indexOfNewline(b []byte) int {
	for i, c := range b {
		if c == '\n' {
			return i
		}
	}
	return -1
}

func writeResp(conn net.Conn, resp map[string]any) error {
	b, err := json.Marshal(resp)
	if err != nil {
		b = []byte(`{"error":"marshal error"}`)
	}
	b = append(b, '\n')
	_ = conn.SetWriteDeadline(time.Now().Add(hostWatchWriteTimeout))
	_, err = conn.Write(b)
	return err
}

func dispatchCmd(cmd string, req map[string]any, h *Host) map[string]any {
	reg := h.Registry
	switch cmd {
	case "list":
		views := reg.ItemsSnapshot()
		items := make([]map[string]any, 0, len(views))
		for _, v := range views {
			m := map[string]any{
				"service": v.Service,
				"path":    v.Path,
				"title":   v.Title,
				"menu":    v.Menu,
				"icon":    v.Icon,
				"tooltip": v.Tooltip,
				"rev":     v.Rev,
			}
			if v.PID > 0 {
				m["pid"] = v.PID
			}
			items = append(items, m)
		}
		return map[string]any{"items": items}
	case "ping":
		return map[string]any{"ok": true}
	case "show", "hide", "toggle":
		ui.EnqueueUICommand(cmd)
		return map[string]any{"ok": true}
	case "activate":
		service, _ := req["service"].(string)
		path, _ := req["path"].(string)
		if service != "" && path != "" {
			if err := callSNI(service, path, "org.kde.StatusNotifierItem.Activate", int32(0), int32(0)); err != nil {
				return map[string]any{"ok": false, "error": err.Error()}
			}
		}
		return map[string]any{"ok": true}
	case "secondary":
		service, _ := req["service"].(string)
		path, _ := req["path"].(string)
		if service != "" && path != "" {
			if err := callSNI(service, path, "org.kde.StatusNotifierItem.SecondaryActivate", int32(0), int32(0)); err != nil {
				return map[string]any{"ok": false, "error": err.Error()}
			}
		}
		return map[string]any{"ok": true}
	case "menu":
		service, _ := req["service"].(string)
		menuPath, _ := req["menu"].(string)
		if service == "" || menuPath == "" {
			return map[string]any{"items": []dbusmenu.MenuEntry{}}
		}
		return map[string]any{"items": dbusmenu.MenuEntries(service, menuPath)}
	case "menuevent":
		service, _ := req["service"].(string)
		menuPath, _ := req["menu"].(string)
		id := 0
		switch v := req["id"].(type) {
		case float64:
			id = int(v)
		case int:
			id = v
		}
		if service != "" && menuPath != "" {
			if err := callDBusMethod(service, menuPath, "com.canonical.dbusmenu.Event", int32(id), "clicked", dbus.MakeVariant(""), uint32(0)); err != nil {
				return map[string]any{"ok": false, "error": err.Error()}
			}
		}
		return map[string]any{"ok": true}
	case "shutdown", "quit", "stop":
		ui.EnqueueUICommand("quit")
		if h != nil {
			h.Shutdown()
		}
		return map[string]any{"ok": true}
	default:
		return map[string]any{"error": "unknown cmd"}
	}
}

func callSNI(service, path, method string, args ...any) error {
	return callDBusMethod(service, path, method, args...)
}

func callDBusMethod(service, path, method string, args ...any) error {
	conn, err := dbusutil.SessionBusConn()
	if err != nil {
		return fmt.Errorf("session bus unavailable: %w", err)
	}
	if conn == nil {
		return errors.New("session bus unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), hostDBusCallTimeout)
	defer cancel()
	obj := conn.Object(service, dbus.ObjectPath(path))
	call := obj.CallWithContext(ctx, method, 0, args...)
	if call.Err != nil {
		return call.Err
	}
	return nil
}

func handleWatch(conn net.Conn, h *Host) {
	reg := h.Registry
	// Watchers only need write deadlines; disable read deadline and monitor
	// peer disconnect so idle clients don't leak goroutines.
	_ = conn.SetReadDeadline(time.Time{})
	closed := make(chan struct{})
	go func() {
		defer close(closed)
		buf := make([]byte, 1)
		for {
			if _, err := conn.Read(buf); err != nil {
				_ = conn.Close()
				return
			}
		}
	}()

	for {
		ch := reg.Changes()
		if err := writeSnapshot(conn, reg); err != nil {
			_ = conn.Close()
			<-closed
			return
		}
		select {
		case <-ch:
			// next iteration subscribes before snapshot to avoid missed updates
		case <-closed:
			return
		}
	}
}

func writeSnapshot(conn net.Conn, reg *statusnotifier.Registry) error {
	views := reg.ItemsSnapshot()
	items := make([]map[string]any, 0, len(views))
	for _, v := range views {
		m := map[string]any{
			"service": v.Service,
			"path":    v.Path,
			"title":   v.Title,
			"menu":    v.Menu,
			"icon":    v.Icon,
			"tooltip": v.Tooltip,
			"rev":     v.Rev,
		}
		if v.PID > 0 {
			m["pid"] = v.PID
		}
		items = append(items, m)
	}
	resp := map[string]any{"items": items}
	b, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	_ = conn.SetWriteDeadline(time.Now().Add(hostWatchWriteTimeout))
	_, err = conn.Write(b)
	return err
}

func handleIcon(conn net.Conn, h *Host, req map[string]any) {
	service, _ := req["service"].(string)
	path, _ := req["path"].(string)
	w, height, data, err := h.Registry.FetchIconPixmap(service, path)
	if err != nil {
		_ = writeResp(conn, map[string]any{"error": err.Error()})
		return
	}
	header := map[string]any{"width": w, "height": height, "length": len(data)}
	b, err := json.Marshal(header)
	if err != nil {
		_ = writeResp(conn, map[string]any{"error": err.Error()})
		return
	}
	b = append(b, '\n')
	_ = conn.SetWriteDeadline(time.Now().Add(hostWatchWriteTimeout))
	if _, err := conn.Write(b); err != nil {
		return
	}
	_, _ = conn.Write(data)
}
