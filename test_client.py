#!/usr/bin/env python3
"""Dummy SNI client for testing the tray host/frontend."""

import dbus
import dbus.service
from dbus.mainloop.glib import DBusGMainLoop
from gi.repository import GLib

BUS_NAME = "com.example.TestTray"
OBJ = "/StatusNotifierItem"
MENU = "/com/example/TestTray/Menu"


class Menu(dbus.service.Object):
    def __init__(self, bus):
        super().__init__(bus, MENU)

    @dbus.service.method("com.canonical.dbusmenu", in_signature="iias", out_signature="u(ia{sv}av)")
    def GetLayout(self, parent_id, recursion_depth, property_names):
        children = dbus.Array([
            dbus.Struct(
                (1, dbus.Dictionary({"label": "Hello", "enabled": True}, signature="sv"),
                 dbus.Array([], signature="v")),
                signature="(ia{sv}av)", variant_level=1),
            dbus.Struct(
                (2, dbus.Dictionary({"label": "Quit", "enabled": True}, signature="sv"),
                 dbus.Array([], signature="v")),
                signature="(ia{sv}av)", variant_level=1),
        ], signature="v")
        layout = (0, dbus.Dictionary({"children-display": "submenu"}, signature="sv"), children)
        return 0, layout

    @dbus.service.method("com.canonical.dbusmenu", in_signature="isvu", out_signature="")
    def Event(self, id, event_id, data, timestamp):
        print(f"[client] menu event id={id} {event_id}", flush=True)


class Item(dbus.service.Object):
    def __init__(self, bus):
        super().__init__(bus, OBJ)
        self._menu = Menu(bus)

    @dbus.service.method("org.freedesktop.DBus.Properties", in_signature="ss", out_signature="v")
    def Get(self, iface, prop):
        if prop == "Title":
            return "Test App"
        if prop == "IconName":
            return "application-x-executable"
        if prop == "Menu":
            return dbus.ObjectPath(MENU)
        if prop == "Category":
            return "ApplicationStatus"
        if prop == "Status":
            return "Active"
        raise Exception("unknown")

    @dbus.service.method("org.freedesktop.DBus.Properties", in_signature="s", out_signature="a{sv}")
    def GetAll(self, iface):
        return {"Title": "Test App", "IconName": "application-x-executable",
                "Menu": dbus.ObjectPath(MENU), "Category": "ApplicationStatus", "Status": "Active"}


def main():
    DBusGMainLoop(set_as_default=True)
    bus = dbus.SessionBus()
    bus_name = dbus.service.BusName(BUS_NAME, bus)  # keep reference
    item = Item(bus)
    watcher = bus.get_object("org.kde.StatusNotifierWatcher", "/StatusNotifierWatcher")
    watcher.RegisterStatusNotifierItem(f"{BUS_NAME}{OBJ}", dbus_interface="org.kde.StatusNotifierWatcher")
    print("[client] registered", flush=True)
    loop = GLib.MainLoop()
    try:
        loop.run()
    except KeyboardInterrupt:
        print("[client] shutting down", flush=True)


if __name__ == "__main__":
    main()
