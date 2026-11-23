package ipc

import "encoding/json"

type Request struct {
	Cmd     string `json:"cmd"`
	Service string `json:"service,omitempty"`
	Path    string `json:"path,omitempty"`
	Menu    string `json:"menu,omitempty"`
	ID      int    `json:"id,omitempty"`
}

type Response struct {
	OK    bool            `json:"ok,omitempty"`
	Error string          `json:"error,omitempty"`
	Items []map[string]any `json:"items,omitempty"`
	Width int             `json:"width,omitempty"`
	Height int            `json:"height,omitempty"`
	Length int             `json:"length,omitempty"`
}

type ItemSnapshot struct {
	Service string `json:"service"`
	Path    string `json:"path"`
	Title   string `json:"title"`
	Menu    string `json:"menu"`
	Icon    string `json:"icon"`
	Tooltip string `json:"tooltip"`
	PID     int    `json:"pid,omitempty"`
	Rev     int    `json:"rev,omitempty"`
}

type Snapshot struct {
	Items []ItemSnapshot `json:"items"`
}

func Encode(v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}
