package commands

import (
	"reflect"
	"testing"
)

func testRegistry(t *testing.T) *Registry {
	t.Helper()
	r, err := NewRegistry(
		[]Command{
			{ID: "attach.file", Keys: "a f", Desc: "Attach file"},
			{ID: "attach.audio", Keys: "a a", Desc: "Send audio"},
			{ID: "help", Keys: "?", Desc: "Keybindings"},
		},
		[]Group{{Key: "a", Name: "attach"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestStep(t *testing.T) {
	r := testRegistry(t)

	next, cmd, ok := r.Step(nil, "a")
	if !ok || cmd != nil || !reflect.DeepEqual(next, []string{"a"}) {
		t.Fatalf("Step(root, a) = %v, %v, %v; want prefix [a]", next, cmd, ok)
	}
	_, cmd, ok = r.Step(next, "f")
	if !ok || cmd == nil || cmd.ID != "attach.file" {
		t.Fatalf("Step([a], f) = %v, %v; want attach.file", cmd, ok)
	}
	_, cmd, ok = r.Step(nil, "?")
	if !ok || cmd == nil || cmd.ID != "help" {
		t.Fatalf("Step(root, ?) = %v, %v; want help", cmd, ok)
	}
	if _, _, ok := r.Step(nil, "j"); ok {
		t.Error("Step(root, j) ok, want unknown")
	}
	if _, _, ok := r.Step([]string{"a"}, "z"); ok {
		t.Error("Step([a], z) ok, want unknown")
	}
}

func TestOptionsAndTitle(t *testing.T) {
	r := testRegistry(t)
	want := []Option{{Key: "?", Label: "Keybindings"}, {Key: "a", Label: "attach", IsGroup: true}}
	if got := r.Options(nil); !reflect.DeepEqual(got, want) {
		t.Errorf("Options(root) = %+v, want %+v", got, want)
	}
	want = []Option{{Key: "a", Label: "Send audio"}, {Key: "f", Label: "Attach file"}}
	if got := r.Options([]string{"a"}); !reflect.DeepEqual(got, want) {
		t.Errorf("Options([a]) = %+v, want %+v", got, want)
	}
	if got := r.Title(nil); got != "␣" {
		t.Errorf("Title(root) = %q", got)
	}
	if got := r.Title([]string{"a"}); got != "␣ a — attach" {
		t.Errorf("Title([a]) = %q", got)
	}
}

func TestNewRegistryRejectsInvalid(t *testing.T) {
	cases := map[string]struct {
		cmds   []Command
		groups []Group
	}{
		"empty keys":      {[]Command{{ID: "x", Keys: " "}}, nil},
		"duplicate":       {[]Command{{ID: "x", Keys: "?"}, {ID: "y", Keys: "?"}}, nil},
		"prefix conflict": {[]Command{{ID: "x", Keys: "a"}, {ID: "y", Keys: "a f"}}, []Group{{Key: "a", Name: "a"}}},
		"missing group":   {[]Command{{ID: "x", Keys: "a f"}}, nil},
		"duplicate id":    {[]Command{{ID: "x", Keys: "?"}, {ID: "x", Keys: "h"}}, nil},
	}
	for name, c := range cases {
		if _, err := NewRegistry(c.cmds, c.groups); err == nil {
			t.Errorf("%s: NewRegistry() = nil error", name)
		}
	}
}

func TestDefaultIsValid(t *testing.T) {
	r := Default()
	for _, id := range []string{"attach.file", "attach.audio", "attach.picker", "help"} {
		found := false
		for _, c := range r.Commands() {
			found = found || c.ID == id
		}
		if !found {
			t.Errorf("default registry lacks %s", id)
		}
	}
}
