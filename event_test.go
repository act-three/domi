package domi

import (
	"strings"
	"testing"
)

type plainMsg struct {
	Tag string `json:"t"`
}

type evMsg struct {
	Tag   string `json:"t"`
	Event Event  `domi:"event" json:"event"`
}

// A Msg without a tagged event field round-trips unchanged.
func TestSpliceNoEventField(t *testing.T) {
	a := On("click", plainMsg{"hi"}).(attr)
	raw, ok := lookupHandler(a.Value)
	if !ok {
		t.Fatal("handler not registered")
	}
	got, err := spliceEvent[plainMsg](raw, []byte(`{"type":"click"}`))
	if err != nil {
		t.Fatalf("splice: %v", err)
	}
	if got.Tag != "hi" {
		t.Fatalf("tag mangled: %+v", got)
	}
}

// A Msg with a tagged event field receives the spliced payload.
func TestSpliceWithEventField(t *testing.T) {
	a := On("input", evMsg{Tag: "EditName"}).(attr)
	raw, _ := lookupHandler(a.Value)
	blob := []byte(`{"type":"input","target":{"tag":"input","name":"name","value":"Em"}}`)
	got, err := spliceEvent[evMsg](raw, blob)
	if err != nil {
		t.Fatalf("splice: %v", err)
	}
	if got.Tag != "EditName" {
		t.Fatalf("tag mangled: %+v", got)
	}
	if got.Event.Type != "input" || got.Event.Target.Value != "Em" {
		t.Fatalf("event not spliced: %+v", got.Event)
	}
}

// A pre-filled event field at construction time must not influence the
// content hash — the registration step zeros it before marshaling.
func TestPrefilledEventFieldDoesNotAffectHash(t *testing.T) {
	a := On("click", evMsg{Tag: "X"}).(attr)
	b := On("click", evMsg{Tag: "X", Event: Event{Type: "click", Key: "Enter"}}).(attr)
	if a.Value != b.Value {
		t.Fatalf("hash diverged on pre-fill; got %q vs %q", a.Value, b.Value)
	}
}

// Multiple handlers on the same event each get the same payload spliced
// into their own Msg independently.
func TestSpliceMultipleHandlersSameEvent(t *testing.T) {
	a := On("keydown", evMsg{Tag: "Save"}).(attr)
	b := On("keydown", evMsg{Tag: "DraftAutosave"}).(attr)
	blob := []byte(`{"type":"keydown","key":"s","ctrl":true,"target":{"tag":"input"}}`)
	for _, hv := range []string{a.Value, b.Value} {
		raw, _ := lookupHandler(hv)
		got, err := spliceEvent[evMsg](raw, blob)
		if err != nil {
			t.Fatalf("splice: %v", err)
		}
		if got.Event.Key != "s" || !got.Event.Ctrl {
			t.Fatalf("payload not threaded into %q: %+v", got.Tag, got.Event)
		}
	}
}

// Form fields land in Event.Form when the JS sends them.
func TestSpliceFormFields(t *testing.T) {
	a := On("submit", evMsg{Tag: "Save"}).(attr)
	raw, _ := lookupHandler(a.Value)
	blob := []byte(`{"type":"submit","target":{"tag":"form"},"form":{"name":"Em","email":"e@x"}}`)
	got, _ := spliceEvent[evMsg](raw, blob)
	if got.Event.Form["name"] != "Em" || got.Event.Form["email"] != "e@x" {
		t.Fatalf("form not spliced: %+v", got.Event.Form)
	}
}

// Two `domi:"event"` tagged fields in one Msg is a registration error.
func TestMultipleEventFieldsPanics(t *testing.T) {
	type bad struct {
		A Event `domi:"event"`
		B Event `domi:"event"`
	}
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on multiple domi:\"event\" fields")
		}
		if !strings.Contains(r.(error).Error(), "multiple") {
			t.Fatalf("wrong panic message: %v", r)
		}
	}()
	On("click", bad{})
}

// Non-struct Msg (e.g. a string) is fine — no event field, no panic.
func TestNonStructMsg(t *testing.T) {
	a := On("click", "hello").(attr)
	raw, _ := lookupHandler(a.Value)
	got, err := spliceEvent[string](raw, []byte(`{"type":"click"}`))
	if err != nil {
		t.Fatalf("splice: %v", err)
	}
	if got != "hello" {
		t.Fatalf("got %q", got)
	}
}

// Empty event blob (e.g. a Cmd-produced dispatch path, hypothetically)
// leaves the event field at its zero value rather than failing.
func TestSpliceEmptyBlob(t *testing.T) {
	a := On("click", evMsg{Tag: "Tick"}).(attr)
	raw, _ := lookupHandler(a.Value)
	got, err := spliceEvent[evMsg](raw, nil)
	if err != nil {
		t.Fatalf("splice: %v", err)
	}
	if got.Event.Type != "" {
		t.Fatalf("expected zero event, got %+v", got.Event)
	}
}
