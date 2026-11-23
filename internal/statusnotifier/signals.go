package statusnotifier

import (
	"log"
	"strings"

	"github.com/godbus/dbus/v5"
)

func (r *Registry) StartSignalLoop() {
	sig := make(chan *dbus.Signal, 64)
	r.conn.Signal(sig)
	// match rules
	rules := []string{
		"type='signal',interface='org.freedesktop.DBus',member='NameOwnerChanged'",
		"type='signal',interface='org.freedesktop.DBus.Properties',member='PropertiesChanged'",
		"type='signal',interface='org.kde.StatusNotifierItem',member='NewTitle'",
		"type='signal',interface='org.kde.StatusNotifierItem',member='NewIcon'",
		"type='signal',interface='org.kde.StatusNotifierItem',member='NewAttentionIcon'",
		"type='signal',interface='org.kde.StatusNotifierItem',member='NewOverlayIcon'",
		"type='signal',interface='org.kde.StatusNotifierItem',member='NewToolTip'",
		"type='signal',interface='org.kde.StatusNotifierItem',member='NewStatus'",
		"type='signal',interface='org.kde.StatusNotifierItem',member='NewMenu'",
	}
	busObj := r.conn.BusObject()
	for _, rule := range rules {
		call := callObjWithTimeout(busObj, dbusCallTimeoutShort, "org.freedesktop.DBus.AddMatch", rule)
		if call.Err != nil {
			log.Printf("[tray] Warning: AddMatch failed for %q: %v", rule, call.Err)
		}
	}
	go func() {
		for s := range sig {
			if s == nil {
				return
			}
			switch {
			case s.Name == "org.freedesktop.DBus.NameOwnerChanged":
				if len(s.Body) >= 3 {
					name, _ := s.Body[0].(string)
					oldOwner, _ := s.Body[1].(string)
					newOwner, _ := s.Body[2].(string)
					r.HandleNameOwnerChanged(name, oldOwner, newOwner)
				}
			case s.Name == "org.freedesktop.DBus.Properties.PropertiesChanged":
				// body: interface name, changed dict, invalidated
				if len(s.Body) >= 2 {
					iface, _ := s.Body[0].(string)
					cmap := map[string]any{}
					if changed, ok := s.Body[1].(map[string]dbus.Variant); ok {
						for k, v := range changed {
							cmap[k] = v
						}
					} else if changed, ok := s.Body[1].(map[string]any); ok {
						for k, v := range changed {
							cmap[k] = v
						}
					}
					var invalidated []string
					if len(s.Body) >= 3 {
						switch inv := s.Body[2].(type) {
						case []string:
							invalidated = inv
						case []any:
							for _, v := range inv {
								if s, ok := v.(string); ok {
									invalidated = append(invalidated, s)
								}
							}
						case []dbus.Variant:
							for _, v := range inv {
								if s, ok := v.Value().(string); ok {
									invalidated = append(invalidated, s)
								}
							}
						}
					}
					r.HandlePropertiesChanged(s.Sender, string(s.Path), iface, cmap, invalidated)
				}
			default:
				// item signals
				member := ""
				parts := strings.Split(s.Name, ".")
				if len(parts) > 0 {
					member = parts[len(parts)-1]
				}
				r.HandleSignal(s.Sender, string(s.Path), member)
			}
		}
	}()
}
