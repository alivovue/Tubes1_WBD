package adapters

import (
	"sort"
	"strings"
)

type Registry struct{ items map[string]Adapter }

func Normalize(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "postgres", "postgresql":
		return "postgresql"
	case "maria", "mariadb":
		return "mariadb"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}
func NewRegistry(items ...Adapter) (*Registry, error) {
	r := &Registry{items: make(map[string]Adapter)}
	for _, item := range items {
		if item == nil || r.items[item.Name()] != nil {
			return nil, ErrInvalid
		}
		r.items[item.Name()] = item
	}
	return r, nil
}
func (r *Registry) Select(name string) (Adapter, error) {
	item := r.items[Normalize(name)]
	if item == nil {
		return nil, ErrUnsupported
	}
	return item, nil
}
func (r *Registry) Supported() []string {
	names := make([]string, 0, len(r.items))
	for name := range r.items {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
