package session

import (
	"reflect"
	"testing"
)

func TestCWDTracker_BELTerminator(t *testing.T) {
	tr := newCWDTracker()
	got := tr.feed([]byte("\x1b]9;9;C:\\Users\\foo\x07PS> "))
	want := []string{`C:\Users\foo`}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
}

func TestCWDTracker_STTerminator(t *testing.T) {
	tr := newCWDTracker()
	got := tr.feed([]byte("\x1b]9;9;C:\\proj\x1b\\C:\\proj> "))
	want := []string{`C:\proj`}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
}

func TestCWDTracker_QuotedPath(t *testing.T) {
	tr := newCWDTracker()
	got := tr.feed([]byte("\x1b]9;9;\"C:\\with space\"\x07"))
	want := []string{`C:\with space`}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
}

func TestCWDTracker_SplitAcrossReads(t *testing.T) {
	tr := newCWDTracker()
	if got := tr.feed([]byte("noise\x1b]9;9;C:\\partia")); got != nil {
		t.Fatalf("first chunk got %#v want nil", got)
	}
	got := tr.feed([]byte("l\x07rest"))
	want := []string{`C:\partial`}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("second chunk got %#v want %#v", got, want)
	}
}

func TestCWDTracker_IgnoresOtherOSC(t *testing.T) {
	tr := newCWDTracker()
	got := tr.feed([]byte("\x1b]0;window title\x07hello"))
	if got != nil {
		t.Fatalf("got %#v want nil", got)
	}
}

func TestCWDTracker_MultiplePromptsInOneChunk(t *testing.T) {
	tr := newCWDTracker()
	got := tr.feed([]byte("\x1b]9;9;C:\\a\x07PS> cd b\r\n\x1b]9;9;C:\\a\\b\x07PS> "))
	want := []string{`C:\a`, `C:\a\b`}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
}

func TestCWDTracker_HandlesAbortedEscape(t *testing.T) {
	tr := newCWDTracker()
	// ESC followed by a non-`]` byte is not OSC; the tracker must keep scanning.
	got := tr.feed([]byte("\x1b[31mred\x1b]9;9;C:\\x\x07"))
	want := []string{`C:\x`}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
}

func TestCWDTracker_SkipsEmptyPath(t *testing.T) {
	tr := newCWDTracker()
	if got := tr.feed([]byte("\x1b]9;9;\x07")); got != nil {
		t.Fatalf("got %#v want nil", got)
	}
}

func TestCWDTracker_OverlongOSCDropped(t *testing.T) {
	tr := newCWDTracker()
	long := make([]byte, 0, cwdMaxBody+200)
	long = append(long, []byte("\x1b]9;9;")...)
	for i := 0; i < cwdMaxBody+100; i++ {
		long = append(long, 'a')
	}
	long = append(long, 0x07)
	got := tr.feed(long)
	if len(got) != 1 {
		t.Fatalf("expected one truncated emission, got %#v", got)
	}
	if len(got[0]) != cwdMaxBody-len("9;9;") {
		t.Fatalf("payload len = %d, want %d", len(got[0]), cwdMaxBody-len("9;9;"))
	}
}
