package statusnotifier

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"

	"tray/internal/dbusutil"
)

type Registry struct {
	conn *dbus.Conn

	mu      registryMutex
	items   []*Item
	itemSet map[string]bool
	hosts   map[string]bool

	changed chan struct{}

	refreshMu      sync.Mutex
	refreshing     map[string]bool
	refreshPending map[string]bool
}

func NewRegistry(conn *dbus.Conn) *Registry {
	return &Registry{
		conn:           conn,
		itemSet:        map[string]bool{},
		hosts:          map[string]bool{},
		changed:        make(chan struct{}),
		refreshing:     map[string]bool{},
		refreshPending: map[string]bool{},
	}
}

func (r *Registry) notifyChanged() {
	r.mu.Lock()
	ch := r.changed
	r.changed = make(chan struct{})
	r.mu.Unlock()
	close(ch)
}

func (r *Registry) changes() <-chan struct{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.changed
}

func (r *Registry) Changes() <-chan struct{} { return r.changes() }

func (r *Registry) emitWatcherPropsChanged(changed []string, invalidated []string) {
	if len(changed) == 0 && len(invalidated) == 0 {
		return
	}
	props := map[string]dbus.Variant{}
	for _, k := range changed {
		switch k {
		case "RegisteredStatusNotifierItems":
			props[k] = dbus.MakeVariant(r.RegisteredItemsStrings())
		case "IsStatusNotifierHostRegistered":
			props[k] = dbus.MakeVariant(r.HostRegistered())
		case "ProtocolVersion":
			props[k] = dbus.MakeVariant(int32(0))
		}
	}
	if invalidated == nil {
		invalidated = []string{}
	}
	_ = r.conn.Emit("/StatusNotifierWatcher", "org.freedesktop.DBus.Properties.PropertiesChanged", "org.kde.StatusNotifierWatcher", props, invalidated)
}

func itemKey(service, path string) string { return service + "\x00" + path }

const (
	dbusCallTimeoutShort = 2 * time.Second
	dbusCallTimeoutLong  = 3 * time.Second
)

func callObjWithTimeout(obj dbus.BusObject, timeout time.Duration, method string, args ...any) *dbus.Call {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return obj.CallWithContext(ctx, method, 0, args...)
}

func (r *Registry) RegisterItem(service, path string) {
	if service == "" || path == "" {
		return
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	key := itemKey(service, path)
	r.mu.Lock()
	if r.itemSet[key] {
		var existing *Item
		for _, it := range r.items {
			if it.Service == service && it.Path == path {
				existing = it
				break
			}
		}
		r.mu.Unlock()
		if existing != nil {
			go func(it *Item) {
				if r.fetchPropsAndResolveOwner(it) {
					r.notifyChanged()
				}
			}(existing)
		}
		return
	}
	r.itemSet[key] = true
	it := newItem(service, path, service)
	r.items = append(r.items, it)
	r.mu.Unlock()

	// Emit registration early so consumers see the item promptly; removal remains
	// robust because it is keyed by the registry set. Initial fetch runs async.
	r.conn.Emit("/StatusNotifierWatcher", "org.kde.StatusNotifierWatcher.StatusNotifierItemRegistered", service+path)
	r.emitWatcherPropsChanged([]string{"RegisteredStatusNotifierItems"}, nil)
	r.notifyChanged()
	go func(it *Item) {
		if r.fetchPropsAndResolveOwner(it) {
			r.notifyChanged()
		}
	}(it)
}

// fetchPropsAndResolveOwner fetches all properties and resolves the unique owner.
// On failure it preserves last known good properties. Returns true when properties were updated.
func (r *Registry) fetchPropsAndResolveOwner(it *Item) bool {
	conn := r.conn
	obj := conn.Object(it.Service, dbus.ObjectPath(it.Path))
	call := callObjWithTimeout(obj, dbusCallTimeoutLong, "org.freedesktop.DBus.Properties.GetAll", "org.kde.StatusNotifierItem")
	updated := false
	if call.Err != nil {
		log.Printf("[tray] Warning: GetAll failed for %s%s: %v", it.Service, it.Path, call.Err)
	} else if len(call.Body) > 0 {
		props := map[string]any{}
		switch m := call.Body[0].(type) {
		case map[string]dbus.Variant:
			for k, v := range m {
				props[k] = v
			}
		case map[string]any:
			for k, v := range m {
				props[k] = v
			}
		default:
			log.Printf("[tray] Warning: GetAll returned unexpected type %T for %s%s", call.Body[0], it.Service, it.Path)
		}
		if len(props) > 0 {
			it.setProps(props)
			updated = true
		} else if call.Err == nil {
			log.Printf("[tray] Warning: GetAll returned no usable properties for %s%s", it.Service, it.Path)
		}
	}

	if !strings.HasPrefix(it.Service, ":") {
		busObj := conn.BusObject()
		var owner string
		ownerCall := callObjWithTimeout(busObj, dbusCallTimeoutShort, "org.freedesktop.DBus.GetNameOwner", it.Service)
		if err := ownerCall.Store(&owner); err == nil && owner != "" {
			it.SetOwner(owner)
		} else if err != nil {
			log.Printf("[tray] Warning: GetNameOwner failed for %s: %v", it.Service, err)
		}
	}
	r.resolvePIDForItem(it)
	return updated
}

func (r *Registry) RefreshItem(service, path string) {
	r.requestRefresh(service, path)
}

func (r *Registry) requestRefresh(service, path string) {
	key := itemKey(service, path)
	r.refreshMu.Lock()
	if r.refreshing[key] {
		r.refreshPending[key] = true
		r.refreshMu.Unlock()
		return
	}
	r.refreshing[key] = true
	r.refreshMu.Unlock()
	go r.runRefresh(service, path, key)
}

func (r *Registry) runRefresh(service, path, key string) {
	for {
		r.mu.Lock()
		var target *Item
		for _, it := range r.items {
			if it.Service == service && it.Path == path {
				target = it
				break
			}
		}
		r.mu.Unlock()
		if target != nil {
			if r.fetchPropsAndResolveOwner(target) {
				r.notifyChanged()
			}
		}

		r.refreshMu.Lock()
		if r.refreshPending[key] {
			delete(r.refreshPending, key)
			r.refreshMu.Unlock()
			continue
		}
		delete(r.refreshing, key)
		r.refreshMu.Unlock()
		return
	}
}

func (r *Registry) RegisterHost(service string) {
	if service == "" {
		return
	}
	r.mu.Lock()
	wasRegistered := len(r.hosts) > 0
	r.hosts[service] = true
	isRegistered := len(r.hosts) > 0
	r.mu.Unlock()
	_ = r.conn.Emit("/StatusNotifierWatcher", "org.kde.StatusNotifierWatcher.StatusNotifierHostRegistered", service)
	if !wasRegistered && isRegistered {
		r.emitWatcherPropsChanged([]string{"IsStatusNotifierHostRegistered"}, nil)
	}
	r.notifyChanged()
}

func (r *Registry) RemoveItem(service, path string) {
	r.removeItem(service, path, nil)
}

func (r *Registry) removeItem(service, path string, target *Item) {
	key := itemKey(service, path)
	r.mu.Lock()
	if !r.itemSet[key] {
		r.mu.Unlock()
		return
	}
	if target != nil {
		found := false
		for _, it := range r.items {
			if it == target {
				found = true
				break
			}
		}
		if !found {
			r.mu.Unlock()
			return
		}
	}
	delete(r.itemSet, key)
	newItems := make([]*Item, 0, len(r.items))
	for _, it := range r.items {
		if it.Service == service && it.Path == path {
			if target == nil || it == target {
				continue
			}
		}
		newItems = append(newItems, it)
	}
	r.items = newItems
	r.mu.Unlock()

	// Emit after unlock (consistent with notifyChanged pattern: release lock before external calls)
	_ = r.conn.Emit("/StatusNotifierWatcher", "org.kde.StatusNotifierWatcher.StatusNotifierItemUnregistered", service+path)
	r.emitWatcherPropsChanged([]string{"RegisteredStatusNotifierItems"}, nil)
	r.notifyChanged()
}

func (r *Registry) RemoveHost(service string) {
	r.mu.Lock()
	if _, ok := r.hosts[service]; !ok {
		r.mu.Unlock()
		return
	}
	delete(r.hosts, service)
	isRegistered := len(r.hosts) > 0
	r.mu.Unlock()

	// Emit after unlock (consistent with notifyChanged pattern: release lock before external calls)
	_ = r.conn.Emit("/StatusNotifierWatcher", "org.kde.StatusNotifierWatcher.StatusNotifierHostUnregistered", service)
	if !isRegistered {
		r.emitWatcherPropsChanged([]string{"IsStatusNotifierHostRegistered"}, nil)
	}
	r.notifyChanged()
}

func (r *Registry) NameHasOwner(name string) bool {
	busObj := r.conn.BusObject()
	var has bool
	call := callObjWithTimeout(busObj, dbusCallTimeoutShort, "org.freedesktop.DBus.NameHasOwner", name)
	if err := call.Store(&has); err != nil {
		return false
	}
	return has
}

func tooltipText(v any) string {
	sl := dbusutil.AsSlice(v)
	if sl == nil {
		return ""
	}
	get := func(idx int) string {
		if idx < 0 || idx >= len(sl) {
			return ""
		}
		val := dbusutil.UnwrapVariant(sl[idx])
		s, ok := val.(string)
		if !ok {
			return ""
		}
		return s
	}
	if s := get(2); s != "" {
		return s
	}
	if s := get(3); s != "" {
		return s
	}
	return get(0)
}

func (r *Registry) ItemsSnapshot() []ItemView {
	r.mu.Lock()
	snapshot := append([]*Item(nil), r.items...)
	r.mu.Unlock()
	out := make([]ItemView, 0, len(snapshot))
	for _, it := range snapshot {
		p := it.snapshot()
		title := dbusutil.PropString(p, "Title")
		menuPath := ""
		if v, ok := p["Menu"]; ok {
			v = dbusutil.UnwrapVariant(v)
			switch m := v.(type) {
			case string:
				menuPath = m
			case dbus.ObjectPath:
				menuPath = string(m)
			}
		}
		iconName := dbusutil.PropString(p, "IconName")
		tooltip := ""
		if v, ok := p["ToolTip"]; ok {
			tooltip = tooltipText(v)
		}
		pid := it.PID()
		if pid == 0 {
			item := it
			go r.resolvePIDForItem(item)
		}
		out = append(out, ItemView{
			Service: it.Service,
			Path:    it.Path,
			Title:   title,
			Menu:    menuPath,
			Icon:    iconName,
			Tooltip: tooltip,
			PID:     pid,
			Rev:     it.Rev(),
		})
	}
	return out
}

func (r *Registry) HostRegistered() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.hosts) > 0
}

func (r *Registry) RegisteredItemsStrings() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.items))
	for _, it := range r.items {
		out = append(out, it.Service+it.Path)
	}
	return out
}

func (r *Registry) resolvePIDForItem(it *Item) {
	if it == nil {
		return
	}
	name := it.OwnerName()
	if name == "" {
		name = it.Service
	}
	busObj := r.conn.BusObject()
	if !strings.HasPrefix(name, ":") {
		var owner string
		call := callObjWithTimeout(busObj, dbusCallTimeoutShort, "org.freedesktop.DBus.GetNameOwner", name)
		if err := call.Store(&owner); err != nil || owner == "" {
			return
		}
		name = owner
	}
	var pid uint32
	call := callObjWithTimeout(busObj, dbusCallTimeoutShort, "org.freedesktop.DBus.GetConnectionUnixProcessID", name)
	if err := call.Store(&pid); err != nil {
		return
	}
	it.SetPID(int(pid))
}

func (r *Registry) HandleNameOwnerChanged(name, oldOwner, newOwner string) {
	if newOwner == "" {
		// remove items/hosts owned by name
		r.mu.Lock()
		toRemoveItems := []*Item{}
		for _, it := range r.items {
			if it.Service == name || it.OwnedBy(name) {
				toRemoveItems = append(toRemoveItems, it)
			}
		}
		_, hostExists := r.hosts[name]
		r.mu.Unlock()

		for _, it := range toRemoveItems {
			r.removeItem(it.Service, it.Path, it)
		}
		if hostExists {
			r.RemoveHost(name)
		}
		return
	}

	// Owner changed/replaced: update cached owner for well-known-name items.
	r.mu.Lock()
	var targets []*Item
	for _, it := range r.items {
		if it.Service == name {
			targets = append(targets, it)
		}
	}
	r.mu.Unlock()
	for _, it := range targets {
		it.SetOwner(newOwner)
		it.SetPID(0)
		item := it
		go r.resolvePIDForItem(item)
	}
}

func (r *Registry) HandlePropertiesChanged(sender, path string, iface string, changed map[string]any, invalidated []string) {
	if iface != "org.kde.StatusNotifierItem" {
		return
	}
	r.mu.Lock()
	var target *Item
	for _, it := range r.items {
		if it.Path == path && it.MatchesSender(sender) {
			target = it
			break
		}
	}
	r.mu.Unlock()
	if target == nil {
		return
	}

	if len(invalidated) > 0 {
		// Conservative: invalidated SNI properties require a fresh GetAll.
		r.requestRefresh(target.Service, target.Path)
		return
	}

	if len(changed) == 0 {
		return
	}
	// merge changed into cached props
	target.mu.Lock()
	if target.props == nil {
		target.props = map[string]any{}
	}
	for k, v := range changed {
		target.props[k] = v
	}
	target.rev++
	target.mu.Unlock()
	r.notifyChanged()
}

func (r *Registry) HandleSignal(sender, path, member string) {
	// NewTitle etc -> refresh item
	r.mu.Lock()
	var target *Item
	for _, it := range r.items {
		if it.Path == path && it.MatchesSender(sender) {
			target = it
			break
		}
	}
	r.mu.Unlock()
	if target == nil {
		return
	}
	switch member {
	case "NewTitle", "NewIcon", "NewAttentionIcon", "NewOverlayIcon", "NewToolTip", "NewStatus", "NewMenu":
		r.requestRefresh(target.Service, target.Path)
	}
}

func (r *Registry) FetchIconPixmap(service, path string) (int, int, []byte, error) {
	obj := r.conn.Object(service, dbus.ObjectPath(path))
	call := callObjWithTimeout(obj, dbusCallTimeoutLong, "org.freedesktop.DBus.Properties.Get", "org.kde.StatusNotifierItem", "IconPixmap")
	if call.Err != nil {
		return 0, 0, nil, call.Err
	}
	if len(call.Body) == 0 {
		return 0, 0, nil, fmt.Errorf("no body")
	}
	// parse nested array of structs
	// Use variant
	v := call.Body[0]
	// unwrap
	if dv, ok := v.(dbus.Variant); ok {
		v = dv.Value()
	}
	arr, ok := v.([]any)
	// godbus may return []dbus.Variant or struct
	_ = arr
	_ = ok
	// Try QDBus-like: godbus represents a(ssay) as []interface{} of dbus struct? Actually IconPixmap is a(iiay). godbus decodes as []interface{} where each is struct {int32,int32,[]byte} possibly as dbus.Struct or []interface{}
	// We'll iterate
	var bestW, bestH int
	var bestData []byte
	// handle []interface{}
	slice := dbusutil.AsSlice(v)
	if slice == nil {
		// maybe []dbus.Variant
		if dv2, ok := v.([]dbus.Variant); ok {
			tmp := make([]any, len(dv2))
			for i, e := range dv2 {
				tmp[i] = e
			}
			slice = tmp
		}
	}
	for _, e := range slice {
		es := dbusutil.AsSlice(dbusutil.UnwrapVariant(e))
		if es == nil || len(es) < 3 {
			continue
		}
		w := 0
		h := 0
		switch vv := es[0].(type) {
		case int32:
			w = int(vv)
		case int:
			w = vv
		case uint32:
			w = int(vv)
		}
		switch vv := es[1].(type) {
		case int32:
			h = int(vv)
		case int:
			h = vv
		case uint32:
			h = int(vv)
		}
		// data
		var data []byte
		switch vv := es[2].(type) {
		case []byte:
			data = vv
		case dbus.Variant:
			if b, ok := vv.Value().([]byte); ok {
				data = b
			}
		}
		if w <= 0 || h <= 0 || w > 256 || h > 256 {
			continue
		}
		if len(data) != w*h*4 {
			continue
		}
		if w*h > bestW*bestH {
			bestW, bestH = w, h
			bestData = data
		}
	}
	if bestData == nil {
		return 0, 0, nil, fmt.Errorf("no pixmap")
	}
	return bestW, bestH, bestData, nil
}
