package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"tray/internal/host"
	"tray/internal/ipc"
)

const pidFileName = "tray-host.pid"

type pidFileData struct {
	PID    int    `json:"pid"`
	Socket string `json:"socket"`
	Exe    string `json:"exe,omitempty"`
}

func runtimeDir() string {
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return dir
	}
	dir := fmt.Sprintf("/tmp/tray-%d", os.Getuid())
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "/tmp"
	}
	_ = os.Chmod(dir, 0o700)
	return dir
}

func pidFilePathForSocket(sock string) string {
	h := sha256.Sum256([]byte(sock))
	name := "tray-host-" + hex.EncodeToString(h[:8]) + ".pid"
	return filepath.Join(runtimeDir(), name)
}

func legacyPIDFilePath() string {
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return filepath.Join(dir, pidFileName)
	}
	return filepath.Join(fmt.Sprintf("/tmp/tray-%d", os.Getuid()), pidFileName)
}

func pidFileCandidates(sock string) []string {
	paths := []string{pidFilePathForSocket(sock)}
	if sock == "" {
		paths = append(paths, legacyPIDFilePath(), filepath.Join("/tmp", pidFileName))
		return paths
	}
	// Legacy fallback for same runtime dir
	legacy := legacyPIDFilePath()
	if legacy != paths[0] {
		paths = append(paths, legacy)
	}
	tmpLegacy := filepath.Join("/tmp", pidFileName)
	if tmpLegacy != paths[0] && tmpLegacy != legacy {
		paths = append(paths, tmpLegacy)
	}
	return paths
}

func writePIDFile(sock string, pid int) error {
	path := pidFilePathForSocket(sock)
	exe, _ := os.Executable()
	data := pidFileData{PID: pid, Socket: sock, Exe: exe}
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp-" + strconv.Itoa(os.Getpid())
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func readPIDFile(sock string) (pidFileData, string, error) {
	for _, path := range pidFileCandidates(sock) {
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var data pidFileData
		if err := json.Unmarshal(b, &data); err == nil && data.PID > 0 {
			if data.Socket == "" {
				data.Socket = sock
			}
			return data, path, nil
		}
		// legacy plain pid
		pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
		if err == nil && pid > 0 {
			return pidFileData{PID: pid, Socket: sock}, path, nil
		}
	}
	return pidFileData{}, "", os.ErrNotExist
}

func removePIDFile(sock string, pid int) {
	data, path, err := readPIDFile(sock)
	if err != nil {
		return
	}
	if pid > 0 && data.PID != pid {
		return
	}
	if !pidFileMatchesProcess(data) {
		// if we can't verify, only remove if pid matches and file is our per-socket file
		if pid > 0 && data.PID == pid && path == pidFilePathForSocket(sock) {
			_ = os.Remove(path)
		}
		return
	}
	_ = os.Remove(path)
}

func removePIDFilePathIfOwned(path string, pid int) {
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var data pidFileData
	if err := json.Unmarshal(b, &data); err != nil {
		p, _ := strconv.Atoi(strings.TrimSpace(string(b)))
		data = pidFileData{PID: p}
	}
	if pid > 0 && data.PID != pid {
		return
	}
	if pid > 0 && !pidAlive(pid) {
		_ = os.Remove(path)
		return
	}
	if pidFileMatchesProcess(data) {
		_ = os.Remove(path)
	}
}

func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

func pidFileMatchesProcess(data pidFileData) bool {
	if data.PID <= 0 {
		return false
	}
	if !pidAlive(data.PID) {
		return false
	}
	exe, _ := os.Executable()
	if exe != "" {
		if target, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", data.PID)); err == nil {
			if sameExecutable(target, exe) || (data.Exe != "" && sameExecutable(target, data.Exe)) {
				return true
			}
		}
	}
	cmdline, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", data.PID))
	if err == nil {
		parts := strings.Split(string(cmdline), "\x00")
		if len(parts) > 0 {
			base := filepath.Base(parts[0])
			if base == "tray" || strings.Contains(parts[0], "/tray") {
				for _, p := range parts {
					if p == "host" {
						return true
					}
				}
			}
			if exe != "" && sameExecutable(parts[0], exe) {
				return true
			}
			if data.Exe != "" && sameExecutable(parts[0], data.Exe) {
				return true
			}
		}
	}
	// If we have exe metadata match but can't inspect proc, be conservative.
	if data.Exe != "" && exe != "" && sameExecutable(data.Exe, exe) {
		return true
	}
	return false
}

func sameExecutable(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	ra, ea := filepath.EvalSymlinks(a)
	rb, eb := filepath.EvalSymlinks(b)
	if ea == nil && eb == nil && ra == rb {
		return true
	}
	return filepath.Base(a) == filepath.Base(b)
}

func cmdStatus(sock string, timeout float64) int {
	resp, err := ipc.Send(sock, timeout, map[string]any{"cmd": "ping"})
	if err != nil || resp["ok"] != true {
		e := ""
		if err != nil {
			e = err.Error()
		}
		fmt.Printf("Host is not reachable: %s\n", e)
		return 1
	}
	lresp, _ := ipc.Send(sock, timeout, map[string]any{"cmd": "list"})
	count := 0
	if lresp != nil {
		if items, ok := lresp["items"].([]any); ok {
			count = len(items)
		}
	}
	fmt.Printf("Host reachable: yes (%d item(s))\n", count)
	return 0
}

func cmdUI(action, sock string, timeout float64) int {
	resp, err := ipc.Send(sock, timeout, map[string]any{"cmd": action})
	if err != nil {
		fmt.Println(err)
		return 1
	}
	if resp != nil && resp["ok"] == true {
		fmt.Printf("UI %s: ok\n", action)
		return 0
	}
	if resp != nil {
		if e, ok := resp["error"].(string); ok {
			fmt.Println("Error:", e)
			return 1
		}
		fmt.Println(resp)
	}
	return 1
}

func isHostNotRunningErr(err error, resp map[string]any) bool {
	msgs := []string{}
	if err != nil {
		msgs = append(msgs, err.Error())
		var opErr *net.OpError
		if errors.As(err, &opErr) {
			if errors.Is(opErr.Err, syscall.ECONNREFUSED) || errors.Is(opErr.Err, os.ErrNotExist) {
				return true
			}
		}
		if errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, os.ErrNotExist) {
			return true
		}
	}
	if resp != nil {
		if v, ok := resp["error"].(string); ok && v != "" {
			msgs = append(msgs, v)
		}
	}
	for _, e := range msgs {
		if strings.Contains(e, "Could not connect") || strings.Contains(e, "Could not reach host") ||
			strings.Contains(e, "Connection refused") || strings.Contains(e, "connection refused") ||
			strings.Contains(e, "no such file or directory") {
			return true
		}
	}
	return false
}

func waitForHostExit(sock string, pid int, timeout float64) bool {
	deadline := time.Now().Add(time.Duration(timeout * float64(time.Second)))
	for time.Now().Before(deadline) {
		socketGone := os.IsNotExist(func() error { _, err := os.Stat(sock); return err }())
		if pid > 0 {
			if !pidAlive(pid) {
				return true
			}
		} else if socketGone {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	if pid > 0 {
		return !pidAlive(pid)
	}
	_, err := os.Stat(sock)
	return os.IsNotExist(err)
}

func forceKillHost(data pidFileData) error {
	if data.PID <= 0 {
		return fmt.Errorf("no pid")
	}
	if !pidFileMatchesProcess(data) {
		return fmt.Errorf("refusing to kill pid %d: identity mismatch", data.PID)
	}
	if err := syscall.Kill(data.PID, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("kill pid %d: %w", data.PID, err)
	}
	return nil
}

func cmdHostStop(sock string, timeout float64) int {
	pidData, pidPath, _ := readPIDFile(sock)
	resp, err := ipc.Send(sock, timeout, map[string]any{"cmd": "shutdown"})
	if err == nil && resp != nil && resp["ok"] == true {
		if waitForHostExit(sock, pidData.PID, timeout) {
			if pidPath != "" {
				removePIDFilePathIfOwned(pidPath, pidData.PID)
			} else {
				removePIDFile(sock, pidData.PID)
			}
			fmt.Println("Host stopped.")
			return 0
		}
		if pidData.PID > 0 {
			if kerr := forceKillHost(pidData); kerr != nil {
				fmt.Printf("Host stop timed out and force-kill failed: %v\n", kerr)
				return 1
			}
			if waitForHostExit(sock, pidData.PID, 2) {
				if pidPath != "" {
					removePIDFilePathIfOwned(pidPath, pidData.PID)
				} else {
					removePIDFile(sock, pidData.PID)
				}
				fmt.Printf("Host force-killed (pid %d).\n", pidData.PID)
				return 0
			}
			fmt.Printf("Host force-killed (pid %d) but did not exit.\n", pidData.PID)
			return 1
		}
		fmt.Println("Host stop timed out (no PID file).")
		return 1
	}

	if isHostNotRunningErr(err, resp) {
		if pidData.PID > 0 && pidFileMatchesProcess(pidData) {
			if kerr := forceKillHost(pidData); kerr != nil {
				fmt.Printf("Host not reachable; force-kill failed: %v\n", kerr)
				return 1
			}
			if waitForHostExit(sock, pidData.PID, 2) {
				if pidPath != "" {
					removePIDFilePathIfOwned(pidPath, pidData.PID)
				} else {
					removePIDFile(sock, pidData.PID)
				}
				fmt.Printf("Host force-killed (pid %d).\n", pidData.PID)
				return 0
			}
		}
		if pidPath != "" && pidData.PID > 0 && !pidAlive(pidData.PID) {
			removePIDFilePathIfOwned(pidPath, pidData.PID)
		} else if pidData.PID == 0 {
			// stale or missing pidfile; best effort remove only clearly owned file
			if pidPath != "" {
				removePIDFilePathIfOwned(pidPath, 0)
			}
		}
		fmt.Println("Host not running.")
		return 0
	}

	if err != nil {
		fmt.Println("Error:", err)
		return 1
	}
	if resp != nil {
		if v, ok := resp["error"].(string); ok && v != "" {
			fmt.Println("Error:", v)
			return 1
		}
		fmt.Println(resp)
		return 1
	}
	fmt.Println("Error: unknown stop failure")
	return 1
}

func cmdHostRestart(fg bool, sock string, timeout float64) int {
	if rc := cmdHostStop(sock, timeout); rc != 0 {
		fmt.Println("Restart aborted: old host was not safely stopped.")
		return rc
	}
	return cmdHostStart(fg, sock, timeout)
}

func cleanupFailedBackgroundStart(pid int, sock string, logPath string) {
	if pid > 0 && pidAlive(pid) {
		_ = syscall.Kill(pid, syscall.SIGTERM)
		deadline := time.Now().Add(500 * time.Millisecond)
		for time.Now().Before(deadline) {
			if !pidAlive(pid) {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		if pidAlive(pid) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	}
	removePIDFilePathIfOwned(pidFilePathForSocket(sock), pid)
	if logPath != "" {
		fmt.Println("Host failed to start. Check logs:", logPath)
	} else {
		fmt.Println("Host failed to start.")
	}
}

func cmdHostStart(fg bool, sock string, timeout float64) int {
	// already running?
	if resp, err := ipc.Send(sock, timeout, map[string]any{"cmd": "ping"}); err == nil && resp != nil && resp["ok"] == true {
		fmt.Println("Host is already running.")
		return 0
	}
	if !fg {
		exe, err := os.Executable()
		if err != nil {
			fmt.Println("Failed to resolve executable:", err)
			return 1
		}
		logPath := filepath.Join(runtimeDir(), "tray-host.log")
		if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
			fmt.Println("Failed to create log directory:", err)
			return 1
		}
		logf, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			fmt.Println("Failed to open log file:", err)
			return 1
		}

		r, w, err := os.Pipe()
		if err != nil {
			_ = logf.Close()
			fmt.Println("pipe error:", err)
			return 1
		}

		env := make([]string, 0, len(os.Environ())+2)
		for _, kv := range os.Environ() {
			if strings.HasPrefix(kv, "TRAY_READY_FD=") || strings.HasPrefix(kv, "TRAY_UI_HIDDEN=") {
				continue
			}
			env = append(env, kv)
		}
		env = append(env, "TRAY_READY_FD=3", "TRAY_UI_HIDDEN=1")

		cmd := exec.Command(exe, "host", "start", "--foreground", "--socket", sock, "--timeout", fmt.Sprintf("%g", timeout))
		cmd.Stdin = nil
		cmd.Stdout = logf
		cmd.Stderr = logf
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		cmd.ExtraFiles = []*os.File{w} // fd 3 in child
		cmd.Env = env

		if err := cmd.Start(); err != nil {
			_ = r.Close()
			_ = w.Close()
			_ = logf.Close()
			fmt.Println("Failed to start the tray host:", err)
			return 1
		}
		pid := cmd.Process.Pid
		if err := writePIDFile(sock, pid); err != nil {
			_ = w.Close()
			_ = r.Close()
			_ = logf.Close()
			cleanupFailedBackgroundStart(pid, sock, logPath)
			fmt.Println("Failed to write PID file:", err)
			return 1
		}
		_ = w.Close()
		go func() { _ = cmd.Wait() }()
		_ = logf.Close()

		type readResult struct {
			data []byte
			err  error
		}
		done := make(chan readResult, 1)
		go func() {
			buf := make([]byte, 64)
			n, readErr := r.Read(buf)
			if n > 0 {
				done <- readResult{data: append([]byte(nil), buf[:n]...), err: readErr}
				return
			}
			done <- readResult{data: nil, err: readErr}
		}()

		select {
		case res := <-done:
			_ = r.Close()
			if string(res.data) != "ready\n" {
				cleanupFailedBackgroundStart(pid, sock, logPath)
				return 1
			}
			fmt.Printf("Host started in background (pid %d). Logs: %s\n", pid, logPath)
			fmt.Println("Run `tray ui show` to show the UI.")
			return 0
		case <-time.After(time.Duration(timeout * float64(time.Second))):
			_ = r.Close()
			cleanupFailedBackgroundStart(pid, sock, logPath)
			return 1
		}
	}

	os.Setenv("TRAY_SOCKET", sock)
	os.Setenv("TRAY_UI_HIDDEN", "1")
	if err := writePIDFile(sock, os.Getpid()); err != nil {
		fmt.Println("Failed to write PID file:", err)
		return 1
	}
	defer removePIDFile(sock, os.Getpid())
	fmt.Println("Run `tray ui show` to show the UI.")
	return host.Run(sock)
}
