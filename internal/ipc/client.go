package ipc

import (
	"encoding/json"
	"fmt"
	"net"
	"time"
)

func Send(sock string, timeout float64, req map[string]any) (map[string]any, error) {
	conn, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: sock, Net: "unix"})
	if err != nil {
		return nil, fmt.Errorf("Could not connect to the tray host at %s. Is it running? Start it with 'tray host start'. (%v)", sock, err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(time.Duration(timeout * float64(time.Second))))
	enc := json.NewEncoder(conn)
	if err := enc.Encode(req); err != nil {
		return nil, err
	}
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 4096)
	for {
		n, err := conn.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
			if containsNewline(buf) {
				break
			}
		}
		if err != nil {
			if len(buf) > 0 {
				break
			}
			return nil, fmt.Errorf("Connection to the tray host was lost: %v", err)
		}
	}
	var resp map[string]any
	if err := json.Unmarshal(bytesTrim(buf), &resp); err != nil {
		return nil, fmt.Errorf("The tray host returned an invalid response: %v", err)
	}
	return resp, nil
}

func containsNewline(b []byte) bool {
	for _, c := range b {
		if c == '\n' {
			return true
		}
	}
	return false
}

func bytesTrim(b []byte) []byte {
	for i, c := range b {
		if c == '\n' {
			return b[:i]
		}
	}
	return b
}
