// Package commands holds the leader-key command registry: key sequences
// typed after Space, resolved one key at a time for the which-key popup.
package commands

import (
	"fmt"
	"sort"
	"strings"
)

// Command is an action reachable as Space followed by Keys ("a f").
type Command struct {
	ID   string
	Keys string
	Desc string
}

// Group labels a key prefix shared by several commands ("a" → "attach").
type Group struct {
	Key  string
	Name string
}

// Option is one row of the which-key popup.
type Option struct {
	Key     string
	Label   string
	IsGroup bool
}

// InvokeMsg is emitted when a key sequence resolves to a command.
type InvokeMsg struct{ ID string }

// Registry resolves leader key sequences.
type Registry struct {
	cmds   map[string]Command // by Keys
	groups map[string]string  // prefix path → name
}

// NewRegistry validates and indexes cmds: no empty or duplicate sequences or
// IDs, no command that is a prefix of another, and a group label for every
// prefix.
func NewRegistry(cmds []Command, groups []Group) (*Registry, error) {
	r := &Registry{cmds: map[string]Command{}, groups: map[string]string{}}
	for _, g := range groups {
		r.groups[g.Key] = g.Name
	}
	ids := map[string]bool{}
	for _, c := range cmds {
		keys := strings.Fields(c.Keys)
		if len(keys) == 0 {
			return nil, fmt.Errorf("command %q has no keys", c.ID)
		}
		c.Keys = strings.Join(keys, " ")
		if _, dup := r.cmds[c.Keys]; dup {
			return nil, fmt.Errorf("duplicate key sequence %q", c.Keys)
		}
		if ids[c.ID] {
			return nil, fmt.Errorf("duplicate command id %q", c.ID)
		}
		ids[c.ID] = true
		for i := 1; i < len(keys); i++ {
			prefix := strings.Join(keys[:i], " ")
			if _, ok := r.groups[prefix]; !ok {
				return nil, fmt.Errorf("prefix %q of %q has no group label", prefix, c.Keys)
			}
		}
		r.cmds[c.Keys] = c
	}
	for seq := range r.cmds {
		for other := range r.cmds {
			if other != seq && strings.HasPrefix(other, seq+" ") {
				return nil, fmt.Errorf("command %q is a prefix of %q", seq, other)
			}
		}
	}
	return r, nil
}

// Step advances prefix by key: cmd is set when the sequence resolves, next is
// the longer prefix otherwise; ok is false for a key that leads nowhere.
func (r *Registry) Step(prefix []string, key string) (next []string, cmd *Command, ok bool) {
	seq := append(append([]string(nil), prefix...), key)
	joined := strings.Join(seq, " ")
	if c, found := r.cmds[joined]; found {
		return nil, &c, true
	}
	if _, isGroup := r.groups[joined]; isGroup {
		return seq, nil, true
	}
	return nil, nil, false
}

// Options lists the keys available after prefix, sorted by key.
func (r *Registry) Options(prefix []string) []Option {
	base := strings.Join(prefix, " ")
	seen := map[string]Option{}
	for seq, c := range r.cmds {
		rest := seq
		if base != "" {
			if !strings.HasPrefix(seq, base+" ") {
				continue
			}
			rest = strings.TrimPrefix(seq, base+" ")
		}
		keys := strings.Fields(rest)
		if len(keys) == 1 {
			seen[keys[0]] = Option{Key: keys[0], Label: c.Desc}
			continue
		}
		groupKey := strings.TrimSpace(base + " " + keys[0])
		seen[keys[0]] = Option{Key: keys[0], Label: r.groups[groupKey], IsGroup: true}
	}
	out := make([]Option, 0, len(seen))
	for _, o := range seen {
		out = append(out, o)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// Title is the popup heading for prefix: "␣" at the root, "␣ a — attach" below.
func (r *Registry) Title(prefix []string) string {
	if len(prefix) == 0 {
		return "␣"
	}
	return "␣ " + strings.Join(prefix, " ") + " — " + r.groups[strings.Join(prefix, " ")]
}

// Commands returns every command sorted by key sequence.
func (r *Registry) Commands() []Command {
	out := make([]Command, 0, len(r.cmds))
	for _, c := range r.cmds {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Keys < out[j].Keys })
	return out
}

// DefaultCommands and DefaultGroups define watui's leader bindings.
var (
	DefaultCommands = []Command{
		{ID: "attach.file", Keys: "a f", Desc: "Attach file (type a path)"},
		{ID: "attach.audio", Keys: "a a", Desc: "Send audio as voice note (type a path)"},
		{ID: "attach.picker", Keys: "a o", Desc: "Pick a file (GUI dialog)"},
		{ID: "help", Keys: "?", Desc: "All keybindings"},
	}
	DefaultGroups = []Group{{Key: "a", Name: "attach"}}
)

// Default returns the registry for DefaultCommands; it panics on an invalid
// definition, which TestDefaultIsValid catches.
func Default() *Registry {
	r, err := NewRegistry(DefaultCommands, DefaultGroups)
	if err != nil {
		panic(err)
	}
	return r
}
