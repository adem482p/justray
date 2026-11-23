package statusnotifier

import (
	"sync"
)

type Item struct {
	Service string
	Path    string
	Owner   string

	mu    sync.Mutex
	props map[string]any
	rev   int
	pid   int
}

func newItem(service, path, owner string) *Item {
	return &Item{Service: service, Path: path, Owner: owner, props: map[string]any{}}
}

func (it *Item) PID() int {
	it.mu.Lock()
	defer it.mu.Unlock()
	return it.pid
}

func (it *Item) SetPID(pid int) {
	it.mu.Lock()
	it.pid = pid
	it.mu.Unlock()
}

func (it *Item) OwnerName() string {
	it.mu.Lock()
	defer it.mu.Unlock()
	return it.Owner
}

func (it *Item) SetOwner(owner string) {
	it.mu.Lock()
	it.Owner = owner
	it.mu.Unlock()
}

func (it *Item) MatchesSender(sender string) bool {
	it.mu.Lock()
	owner := it.Owner
	it.mu.Unlock()
	return it.Service == sender || owner == sender
}

func (it *Item) OwnedBy(name string) bool {
	it.mu.Lock()
	owner := it.Owner
	it.mu.Unlock()
	return it.Service == name || owner == name
}

func (it *Item) setProps(p map[string]any) {
	it.mu.Lock()
	defer it.mu.Unlock()
	it.props = p
	it.rev++
}

func (it *Item) snapshot() map[string]any {
	it.mu.Lock()
	defer it.mu.Unlock()
	out := map[string]any{}
	for k, v := range it.props {
		out[k] = v
	}
	out["_service"] = it.Service
	out["_path"] = it.Path
	out["_rev"] = it.rev
	return out
}

func (it *Item) Rev() int {
	it.mu.Lock()
	defer it.mu.Unlock()
	return it.rev
}
