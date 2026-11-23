package statusnotifier

import (
	"strings"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
	"github.com/godbus/dbus/v5/prop"
)

type Service struct {
	conn     *dbus.Conn
	registry *Registry
}

func NewService(conn *dbus.Conn) *Service {
	s := &Service{conn: conn, registry: NewRegistry(conn)}
	return s
}

func (s *Service) Registry() *Registry { return s.registry }

func (s *Service) Export() error {
	const path = "/StatusNotifierWatcher"
	if err := s.conn.Export(s, path, "org.kde.StatusNotifierWatcher"); err != nil {
		return err
	}
	if err := s.conn.Export(s, path, "org.freedesktop.DBus.Properties"); err != nil {
		return err
	}
	node := &introspect.Node{
		Name: path,
		Interfaces: []introspect.Interface{
			{
				Name: "org.kde.StatusNotifierWatcher",
				Methods: []introspect.Method{
					{Name: "RegisterStatusNotifierItem", Args: []introspect.Arg{{Name: "service", Type: "s", Direction: "in"}}},
					{Name: "RegisterStatusNotifierHost", Args: []introspect.Arg{{Name: "service", Type: "s", Direction: "in"}}},
				},
				Signals: []introspect.Signal{
					{Name: "StatusNotifierItemRegistered", Args: []introspect.Arg{{Name: "service", Type: "s", Direction: "out"}}},
					{Name: "StatusNotifierItemUnregistered", Args: []introspect.Arg{{Name: "service", Type: "s", Direction: "out"}}},
					{Name: "StatusNotifierHostRegistered"},
					{Name: "StatusNotifierHostUnregistered"},
				},
				Properties: []introspect.Property{
					{Name: "RegisteredStatusNotifierItems", Type: "as", Access: "read"},
					{Name: "IsStatusNotifierHostRegistered", Type: "b", Access: "read"},
					{Name: "ProtocolVersion", Type: "i", Access: "read"},
				},
			},
			introspect.IntrospectData,
			prop.IntrospectData,
		},
	}
	if err := s.conn.Export(introspect.NewIntrospectable(node), path, "org.freedesktop.DBus.Introspectable"); err != nil {
		return err
	}
	return nil
}

// D-Bus methods delegate to registry.
func (s *Service) RegisterStatusNotifierItem(service string, sender dbus.Sender) *dbus.Error {
	senderName := string(sender)
	var svcName, objPath string
	switch {
	case service == "":
		if senderName == "" {
			return dbus.NewError("org.kde.StatusNotifierWatcher.Error", []interface{}{"Invalid service"})
		}
		svcName = senderName
		objPath = "/StatusNotifierItem"
	case strings.HasPrefix(service, "/"):
		if senderName == "" {
			return dbus.NewError("org.kde.StatusNotifierWatcher.Error", []interface{}{"Invalid service"})
		}
		svcName = senderName
		objPath = "/" + strings.TrimPrefix(service, "/")
		if objPath == "/" {
			objPath = "/StatusNotifierItem"
		}
	default:
		if idx := strings.Index(service, "/"); idx >= 0 {
			svcName = service[:idx]
			objPath = "/" + strings.TrimPrefix(service[idx+1:], "/")
			if objPath == "/" {
				objPath = "/StatusNotifierItem"
			}
		} else {
			svcName = service
			objPath = "/StatusNotifierItem"
		}
	}
	if svcName == "" || !strings.HasPrefix(objPath, "/") {
		return dbus.NewError("org.kde.StatusNotifierWatcher.Error", []interface{}{"Invalid service"})
	}
	s.registry.RegisterItem(svcName, objPath)
	return nil
}

func (s *Service) RegisterStatusNotifierHost(service string) *dbus.Error {
	if service == "" {
		return dbus.NewError("org.kde.StatusNotifierWatcher.Error", []interface{}{"Invalid service"})
	}
	s.registry.RegisterHost(service)
	return nil
}

func (s *Service) GetRegisteredStatusNotifierItems() ([]string, *dbus.Error) {
	return s.registry.RegisteredItemsStrings(), nil
}

func (s *Service) Get(interfaceName, propertyName string) (dbus.Variant, *dbus.Error) {
	if interfaceName != "org.kde.StatusNotifierWatcher" {
		return dbus.Variant{}, dbus.NewError("org.freedesktop.DBus.Error.UnknownInterface", []interface{}{"Unknown interface"})
	}
	switch propertyName {
	case "RegisteredStatusNotifierItems":
		return dbus.MakeVariant(s.registry.RegisteredItemsStrings()), nil
	case "IsStatusNotifierHostRegistered":
		return dbus.MakeVariant(s.registry.HostRegistered()), nil
	case "ProtocolVersion":
		return dbus.MakeVariant(int32(0)), nil
	}
	return dbus.Variant{}, dbus.NewError("org.freedesktop.DBus.Error.InvalidArgs", []interface{}{"Unknown property"})
}

func (s *Service) GetAll(interfaceName string) (map[string]dbus.Variant, *dbus.Error) {
	if interfaceName != "org.kde.StatusNotifierWatcher" {
		return nil, dbus.NewError("org.freedesktop.DBus.Error.UnknownInterface", []interface{}{"Unknown interface"})
	}
	return map[string]dbus.Variant{
		"RegisteredStatusNotifierItems":  dbus.MakeVariant(s.registry.RegisteredItemsStrings()),
		"IsStatusNotifierHostRegistered": dbus.MakeVariant(s.registry.HostRegistered()),
		"ProtocolVersion":                dbus.MakeVariant(int32(0)),
	}, nil
}

func (s *Service) Set(interfaceName, propertyName string, value dbus.Variant) *dbus.Error {
	return dbus.NewError("org.freedesktop.DBus.Error.PropertyReadOnly", []interface{}{"read-only"})
}
