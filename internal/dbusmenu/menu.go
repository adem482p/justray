package dbusmenu

import (
	"context"
	"time"

	"github.com/godbus/dbus/v5"

	"tray/internal/dbusutil"
)

const dbusMenuCallTimeout = 3 * time.Second

type MenuEntry struct {
	ID          int         `json:"id"`
	Label       string      `json:"label"`
	Type        string      `json:"type,omitempty"`
	Enabled     bool        `json:"enabled"`
	ToggleType  string      `json:"toggleType,omitempty"`
	ToggleState int         `json:"toggleState"`
	Icon        string      `json:"icon,omitempty"`
	Children    []MenuEntry `json:"children,omitempty"`
}

func MenuEntries(service, menuPath string) []MenuEntry {
	return MenuEntriesFromVariants(dbusMenuItems(service, menuPath))
}

func MenuEntriesFromVariants(vars []any) []MenuEntry {
	out := []MenuEntry{}
	for _, e := range vars {
		sl := dbusutil.AsSlice(dbusutil.UnwrapVariant(e))
		if sl == nil || len(sl) < 2 {
			continue
		}
		id := 0
		switch v := sl[0].(type) {
		case int:
			id = v
		case int32:
			id = int(v)
		case int64:
			id = int(v)
		case uint32:
			id = int(v)
		case float64:
			id = int(v)
		}
		props := dbusutil.AsMap(sl[1])
		if props == nil {
			props = map[string]any{}
		}
		if !dbusutil.PropBool(props, "visible", true) {
			continue
		}
		entryType := dbusutil.PropString(props, "type")
		if entryType == "separator" {
			out = append(out, MenuEntry{ID: id, Type: "separator", Enabled: true})
			continue
		}
		label := dbusutil.StripMnemonic(dbusutil.PropString(props, "label"))
		enabled := dbusutil.PropBool(props, "enabled", true)
		toggleType := dbusutil.PropString(props, "toggle-type")
		toggleState := propInt(props, "toggle-state", 0)
		icon := dbusutil.PropString(props, "icon-name")
		children := []MenuEntry{}
		if len(sl) > 2 {
			if cs := dbusutil.AsSlice(dbusutil.UnwrapVariant(sl[2])); cs != nil {
				for _, c := range cs {
					children = append(children, MenuEntriesFromVariants([]any{c})...)
				}
			}
		}
		out = append(out, MenuEntry{
			ID:          id,
			Label:       label,
			Type:        entryType,
			Enabled:     enabled,
			ToggleType:  toggleType,
			ToggleState: toggleState,
			Icon:        icon,
			Children:    children,
		})
	}
	return out
}

func propInt(m map[string]any, key string, def int) int {
	if m == nil {
		return def
	}
	v, ok := m[key]
	if !ok {
		return def
	}
	v = dbusutil.UnwrapVariant(v)
	switch t := v.(type) {
	case int:
		return t
	case int32:
		return int(t)
	case int64:
		return int(t)
	case uint32:
		return int(t)
	case uint64:
		return int(t)
	case float64:
		return int(t)
	default:
		return def
	}
}

func dbusMenuItems(service, menuPath string) []any {
	conn, err := dbusutil.SessionBusConn()
	if err != nil || conn == nil {
		return nil
	}
	obj := conn.Object(service, dbus.ObjectPath(menuPath))
	// AboutToShow is best-effort; dynamic menus may populate on this signal.
	ctx, cancel := context.WithTimeout(context.Background(), dbusMenuCallTimeout)
	_ = obj.CallWithContext(ctx, "com.canonical.dbusmenu.AboutToShow", 0, int32(0)).Err
	cancel()

	ctx, cancel = context.WithTimeout(context.Background(), dbusMenuCallTimeout)
	call := obj.CallWithContext(ctx, "com.canonical.dbusmenu.GetLayout", 0, 0, -1, []string{})
	cancel()
	if call.Err != nil {
		return nil
	}
	if len(call.Body) < 2 {
		return nil
	}
	layout := dbusutil.UnwrapVariant(call.Body[1])
	sl := dbusutil.AsSlice(layout)
	if sl == nil || len(sl) < 3 {
		return nil
	}
	children := dbusutil.UnwrapVariant(sl[2])
	childSlice := dbusutil.AsSlice(children)
	if childSlice == nil {
		return nil
	}
	return childSlice
}
