package dbusutil

import (
	"fmt"

	"github.com/godbus/dbus/v5"
)

func UnwrapVariant(v any) any {
	if dv, ok := v.(dbus.Variant); ok {
		return dv.Value()
	}
	return v
}

func AsSlice(v any) []any {
	v = UnwrapVariant(v)
	if s, ok := v.([]any); ok {
		return s
	}
	if s, ok := v.([]dbus.Variant); ok {
		out := make([]any, len(s))
		for i, e := range s {
			out[i] = e
		}
		return out
	}
	return nil
}

func AsMap(v any) map[string]any {
	v = UnwrapVariant(v)
	if m, ok := v.(map[string]any); ok {
		return m
	}
	if m, ok := v.(map[string]dbus.Variant); ok {
		out := map[string]any{}
		for k, val := range m {
			out[k] = val
		}
		return out
	}
	return nil
}

func PropString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok {
		return ""
	}
	v = UnwrapVariant(v)
	switch t := v.(type) {
	case string:
		return t
	case dbus.ObjectPath:
		return string(t)
	case bool:
		return fmt.Sprintf("%v", t)
	case int, int32, int64, uint, uint32, uint64:
		return fmt.Sprintf("%v", t)
	default:
		return fmt.Sprintf("%v", t)
	}
}

func PropBool(m map[string]any, key string, def bool) bool {
	if m == nil {
		return def
	}
	v, ok := m[key]
	if !ok {
		return def
	}
	v = UnwrapVariant(v)
	if b, ok := v.(bool); ok {
		return b
	}
	return def
}

// SessionBusConn returns a shared session bus connection, or an error if
// neither the shared nor a private session connection can be established.
func SessionBusConn() (*dbus.Conn, error) {
	conn, err := dbus.SessionBus()
	if err == nil {
		return conn, nil
	}
	conn, err = dbus.ConnectSessionBus()
	if err != nil {
		return nil, err
	}
	return conn, nil
}

// SessionBus returns a shared session bus connection, or nil on failure.
// Prefer SessionBusConn for new code so callers can handle connection errors.
func SessionBus() *dbus.Conn {
	conn, err := SessionBusConn()
	if err != nil {
		return nil
	}
	return conn
}

func StripMnemonic(s string) string {
	out := ""
	skip := false
	for _, r := range s {
		if r == '_' && !skip {
			skip = true
			continue
		}
		skip = false
		out += string(r)
	}
	return out
}
