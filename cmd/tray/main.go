package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"

	"tray/internal/ipc"
)

var (
	socketPathFlag string
	timeoutFlag    float64 = 5.0
)

func defaultSocketPath() string {
	if v := os.Getenv("TRAY_SOCKET"); v != "" {
		return v
	}
	base := os.Getenv("XDG_RUNTIME_DIR")
	if base == "" {
		base = fmt.Sprintf("/tmp/tray-%d", os.Getuid())
		if err := os.MkdirAll(base, 0o700); err != nil {
			base = "/tmp"
		} else {
			_ = os.Chmod(base, 0o700)
		}
	}
	return base + "/tray.sock"
}

func resolveSocket(argsSocket string) string {
	if argsSocket != "" {
		return argsSocket
	}
	if socketPathFlag != "" {
		return socketPathFlag
	}
	return defaultSocketPath()
}

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(argv []string) int {
	if len(argv) == 0 {
		printHelp()
		return 0
	}
	args := append([]string{}, argv...)
	var socketOverride string
	filtered := []string{}
	i := 0
	for i < len(args) {
		switch args[i] {
		case "--socket":
			if i+1 < len(args) {
				socketOverride = args[i+1]
				i += 2
				continue
			}
		case "--timeout":
			if i+1 < len(args) {
				v, _ := strconv.ParseFloat(args[i+1], 64)
				timeoutFlag = v
				i += 2
				continue
			}
		}
		filtered = append(filtered, args[i])
		i++
	}
	socketPathFlag = socketOverride

	if len(filtered) == 0 {
		printHelp()
		return 0
	}

	switch filtered[0] {
	case "start":
		fs := flag.NewFlagSet("start", flag.ContinueOnError)
		fg := fs.Bool("foreground", false, "foreground")
		sock := fs.String("socket", "", "socket")
		to := fs.Float64("timeout", timeoutFlag, "timeout")
		fs.Parse(filtered[1:])
		timeoutFlag = *to
		return cmdHostStart(*fg, resolveSocket(*sock), timeoutFlag)
	case "stop":
		fs := flag.NewFlagSet("stop", flag.ContinueOnError)
		sock := fs.String("socket", "", "socket")
		to := fs.Float64("timeout", timeoutFlag, "timeout")
		fs.Parse(filtered[1:])
		timeoutFlag = *to
		return cmdHostStop(resolveSocket(*sock), timeoutFlag)
	case "restart":
		fs := flag.NewFlagSet("restart", flag.ContinueOnError)
		fg := fs.Bool("foreground", false, "foreground")
		sock := fs.String("socket", "", "socket")
		to := fs.Float64("timeout", timeoutFlag, "timeout")
		fs.Parse(filtered[1:])
		timeoutFlag = *to
		return cmdHostRestart(*fg, resolveSocket(*sock), timeoutFlag)
	case "host":
		if len(filtered) < 2 {
			printHelp()
			return 0
		}
		switch filtered[1] {
		case "start":
			fs := flag.NewFlagSet("host start", flag.ContinueOnError)
			fg := fs.Bool("foreground", false, "foreground")
			sock := fs.String("socket", "", "socket")
			to := fs.Float64("timeout", timeoutFlag, "timeout")
			fs.Parse(filtered[2:])
			timeoutFlag = *to
			return cmdHostStart(*fg, resolveSocket(*sock), timeoutFlag)
		case "stop":
			fs := flag.NewFlagSet("host stop", flag.ContinueOnError)
			sock := fs.String("socket", "", "socket")
			to := fs.Float64("timeout", timeoutFlag, "timeout")
			fs.Parse(filtered[2:])
			timeoutFlag = *to
			return cmdHostStop(resolveSocket(*sock), timeoutFlag)
		case "restart":
			fs := flag.NewFlagSet("host restart", flag.ContinueOnError)
			fg := fs.Bool("foreground", false, "foreground")
			sock := fs.String("socket", "", "socket")
			to := fs.Float64("timeout", timeoutFlag, "timeout")
			fs.Parse(filtered[2:])
			timeoutFlag = *to
			return cmdHostRestart(*fg, resolveSocket(*sock), timeoutFlag)
		default:
			printHelp()
			return 0
		}
	case "list":
		fs := flag.NewFlagSet("list", flag.ContinueOnError)
		asJSON := fs.Bool("json", false, "json")
		sock := fs.String("socket", "", "socket")
		to := fs.Float64("timeout", timeoutFlag, "timeout")
		fs.Parse(filtered[1:])
		timeoutFlag = *to
		return cmdList(resolveSocket(*sock), timeoutFlag, *asJSON)
	case "status":
		fs := flag.NewFlagSet("status", flag.ContinueOnError)
		sock := fs.String("socket", "", "socket")
		to := fs.Float64("timeout", timeoutFlag, "timeout")
		fs.Parse(filtered[1:])
		timeoutFlag = *to
		return cmdStatus(resolveSocket(*sock), timeoutFlag)
	case "ui":
		if len(filtered) < 2 {
			printHelp()
			return 0
		}
		action := filtered[1]
		fs := flag.NewFlagSet("ui", flag.ContinueOnError)
		sock := fs.String("socket", "", "socket")
		to := fs.Float64("timeout", timeoutFlag, "timeout")
		fs.Parse(filtered[2:])
		timeoutFlag = *to
		return cmdUI(action, resolveSocket(*sock), timeoutFlag)
	case "show", "hide", "toggle":
		action := filtered[0]
		fs := flag.NewFlagSet(action, flag.ContinueOnError)
		sock := fs.String("socket", "", "socket")
		to := fs.Float64("timeout", timeoutFlag, "timeout")
		fs.Parse(filtered[1:])
		timeoutFlag = *to
		return cmdUI(action, resolveSocket(*sock), timeoutFlag)
	default:
		printHelp()
		return 0
	}
}

func printHelp() {
	fmt.Println("tray host and CLI")
	fmt.Println("Usage: tray [--socket PATH] [--timeout SECS] <command>")
}

func cmdList(sock string, timeout float64, asJSON bool) int {
	resp, err := ipc.Send(sock, timeout, map[string]any{"cmd": "list"})
	if err != nil {
		fmt.Println(err)
		return 1
	}
	items, _ := resp["items"].([]any)
	if asJSON {
		b, _ := json.MarshalIndent(items, "", "  ")
		fmt.Println(string(b))
		return 0
	}
	if len(items) == 0 {
		fmt.Println("No background apps.")
		return 0
	}
	for i, it := range items {
		m, _ := it.(map[string]any)
		title := "(no title)"
		if t, ok := m["title"].(string); ok && t != "" {
			title = t
		}
		service := "?"
		if s, ok := m["service"].(string); ok {
			service = s
		}
		path := ""
		if p, ok := m["path"].(string); ok {
			path = p
		}
		pidStr := ""
		if pid, ok := m["pid"].(float64); ok {
			pidStr = fmt.Sprintf("  (pid %.0f)", pid)
		}
		extra := ""
		if path != "" {
			extra = "  " + path
		}
		fmt.Printf("%d. %s  [%s]%s%s\n", i+1, title, service, extra, pidStr)
	}
	return 0
}
