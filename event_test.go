package domi

import (
	"testing"
)

type plainMsg struct {
	Tag string `json:"t"`
}

// handlerMsg returns the marshaled Msg an On() attr carries under its
// content-hash key — the bytes the session would register and later feed
// to Update when the event fires.
func handlerMsg(a attr) ([]byte, bool) {
	raw, ok := a.handlers[a.attr.Value]
	return raw, ok
}

// A Msg without a tagged event field round-trips unchanged.
func TestSpliceNoEventField(t *testing.T) {
	a := On("click")(plainMsg{"hi"}).(attr)
	raw, ok := handlerMsg(a)
	if !ok {
		t.Fatal("handler not registered")
	}
	got, err := unmarshalMsg[plainMsg](raw, []byte(`{"type":"click"}`))
	if err != nil {
		t.Fatalf("splice: %v", err)
	}
	if got.Tag != "hi" {
		t.Fatalf("tag mangled: %+v", got)
	}
}

// A Msg with a tagged event field receives the spliced payload. The
// field type can be any struct whose JSON tags match what the client
// sends — here we only care about Type and Target.Value.
func TestSpliceWithEventField(t *testing.T) {
	type evt struct {
		Type   string `json:"type"`
		Target struct {
			Value string `json:"value"`
		} `json:"target"`
	}
	type msg struct {
		Tag   string `json:"t"`
		Event evt    `domi:"event" json:"event"`
	}
	a := On("input")(msg{Tag: "EditName"}).(attr)
	raw, _ := handlerMsg(a)
	blob := []byte(`{"type":"input","target":{"tag":"input","name":"name","value":"Em"}}`)
	got, err := unmarshalMsg[msg](raw, blob)
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
	type evt struct {
		Type string `json:"type"`
		Key  string `json:"key"`
	}
	type msg struct {
		Tag   string `json:"t"`
		Event evt    `domi:"event" json:"event"`
	}
	a := On("click")(msg{Tag: "X"}).(attr)
	b := On("click")(msg{Tag: "X", Event: evt{Type: "click", Key: "Enter"}}).(attr)
	if a.attr.Value != b.attr.Value {
		t.Fatalf("hash diverged on pre-fill; got %q vs %q", a.attr.Value, b.attr.Value)
	}
}

// Multiple handlers on the same event each get the same payload spliced
// into their own Msg independently.
func TestSpliceMultipleHandlersSameEvent(t *testing.T) {
	type evt struct {
		Key  string `json:"key"`
		Ctrl bool   `json:"ctrl"`
	}
	type msg struct {
		Tag   string `json:"t"`
		Event evt    `domi:"event" json:"event"`
	}
	a := On("keydown")(msg{Tag: "Save"}).(attr)
	b := On("keydown")(msg{Tag: "DraftAutosave"}).(attr)
	blob := []byte(`{"type":"keydown","key":"s","ctrl":true,"target":{"tag":"input"}}`)
	for _, h := range []attr{a, b} {
		raw, _ := handlerMsg(h)
		got, err := unmarshalMsg[msg](raw, blob)
		if err != nil {
			t.Fatalf("splice: %v", err)
		}
		if got.Event.Key != "s" || !got.Event.Ctrl {
			t.Fatalf("payload not threaded into %q: %+v", got.Tag, got.Event)
		}
	}
}

// When a Msg has more than one `domi:"event"` field, the first one in
// declaration order wins; later ones are ignored.
func TestMultipleEventFieldsFirstWins(t *testing.T) {
	type evt struct {
		Type string `json:"type"`
	}
	type msg struct {
		A evt `domi:"event" json:"a"`
		B evt `domi:"event" json:"b"`
	}
	a := On("click")(msg{}).(attr)
	raw, _ := handlerMsg(a)
	got, err := unmarshalMsg[msg](raw, []byte(`{"type":"click"}`))
	if err != nil {
		t.Fatalf("splice: %v", err)
	}
	if got.A.Type != "click" {
		t.Fatalf("event did not land in first tagged field: %+v", got)
	}
	if got.B.Type != "" {
		t.Fatalf("event leaked into second tagged field: %+v", got)
	}
}

// Non-struct Msg (e.g. a string) is fine — no event field, no panic.
func TestNonStructMsg(t *testing.T) {
	a := On("click")("hello").(attr)
	raw, _ := handlerMsg(a)
	got, err := unmarshalMsg[string](raw, []byte(`{"type":"click"}`))
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
	type evt struct {
		Type string `json:"type"`
	}
	type msg struct {
		Tag   string `json:"t"`
		Event evt    `domi:"event" json:"event"`
	}
	a := On("click")(msg{Tag: "Tick"}).(attr)
	raw, _ := handlerMsg(a)
	got, err := unmarshalMsg[msg](raw, nil)
	if err != nil {
		t.Fatalf("splice: %v", err)
	}
	if got.Event.Type != "" {
		t.Fatalf("expected zero event, got %+v", got.Event)
	}
}
