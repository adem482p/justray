package host

import "os"

func (h *Host) Shutdown() {
	h.shutdownOnce.Do(func() {
		h.closeListenerOnce()
		h.removeSocketIfOwned()
		if h.Conn != nil {
			_ = h.Conn.Close()
		}
	})
}

func (h *Host) closeListenerOnce() {
	h.closeOnce.Do(func() {
		if h.Listener != nil {
			_ = h.Listener.Close()
		}
	})
}

func (h *Host) removeSocketIfOwned() {
	if h.SocketPath == "" {
		return
	}
	fi, err := os.Lstat(h.SocketPath)
	if err != nil {
		return
	}
	if fi.Mode()&os.ModeSocket == 0 {
		return
	}
	_ = os.Remove(h.SocketPath)
}
