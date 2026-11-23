package statusnotifier

type ItemView struct {
	Service string `json:"service"`
	Path    string `json:"path"`
	Title   string `json:"title"`
	Menu    string `json:"menu"`
	Icon    string `json:"icon"`
	Tooltip string `json:"tooltip"`
	PID     int    `json:"pid,omitempty"`
	Rev     int    `json:"rev,omitempty"`
}
