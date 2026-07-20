package domi

import (
	"errors"
	"strings"
	"testing"
)

func renderToString(t *testing.T, n Node) string {
	t.Helper()
	var b strings.Builder
	if err := RenderTo(&b, n); err != nil {
		t.Fatalf("RenderTo: %v", err)
	}
	return b.String()
}

func TestRenderToElement(t *testing.T) {
	got := renderToString(t, Tag("p")(Name("class")("x"))(Text("hi")))
	if want := `<p class="x">hi</p>`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRenderToFragmentRendersEachInOrder(t *testing.T) {
	got := renderToString(t, Fragment(Text("a"), Tag("br")(), Text("b")))
	if want := "a<br>b"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRenderToEmptyFragmentWritesNothing(t *testing.T) {
	if got := renderToString(t, Fragment()); got != "" {
		t.Errorf("got %q, want empty", got)
	}
	if got := renderToString(t, nil); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

// Handlers have no session to dispatch to in a static render, so their
// domi-msg-* attributes appear but do nothing.
func TestRenderToHandlerAttrIsInert(t *testing.T) {
	got := renderToString(t, Tag("button")(On("click", msgFn("m")))(Text("ok")))
	if !strings.Contains(got, "domi-msg-click=") {
		t.Errorf("handler attr missing from %q", got)
	}
}

// errWriter fails after writing n bytes, to prove writer errors surface.
type errWriter struct {
	n   int
	err error
}

func (w *errWriter) Write(p []byte) (int, error) {
	if w.n <= 0 {
		return 0, w.err
	}
	if len(p) > w.n {
		p = p[:w.n]
	}
	w.n -= len(p)
	if w.n <= 0 {
		return len(p), w.err
	}
	return len(p), nil
}

func TestRenderToReturnsWriterError(t *testing.T) {
	want := errors.New("boom")
	err := RenderTo(&errWriter{n: 3, err: want}, Tag("p")()(Text("hello")))
	if !errors.Is(err, want) {
		t.Errorf("got %v, want %v", err, want)
	}
}
