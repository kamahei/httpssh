package session

import (
	"bytes"
	"testing"
)

func TestRingBuffer_BelowCapacity(t *testing.T) {
	rb := NewRingBuffer(8)
	if _, err := rb.Write([]byte("abc")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := rb.Snapshot(); !bytes.Equal(got, []byte("abc")) {
		t.Fatalf("snapshot=%q want abc", got)
	}
}

func TestRingBuffer_ExactCapacity(t *testing.T) {
	rb := NewRingBuffer(8)
	rb.Write([]byte("01234567"))
	if got := rb.Snapshot(); !bytes.Equal(got, []byte("01234567")) {
		t.Fatalf("snapshot=%q want 01234567", got)
	}
}

func TestRingBuffer_OverCapacitySingleWrite(t *testing.T) {
	rb := NewRingBuffer(4)
	rb.Write([]byte("abcdefgh"))
	if got := rb.Snapshot(); !bytes.Equal(got, []byte("efgh")) {
		t.Fatalf("snapshot=%q want efgh", got)
	}
}

func TestRingBuffer_WrapAcrossWrites(t *testing.T) {
	rb := NewRingBuffer(4)
	rb.Write([]byte("abc"))
	rb.Write([]byte("def"))
	if got := rb.Snapshot(); !bytes.Equal(got, []byte("cdef")) {
		t.Fatalf("snapshot=%q want cdef", got)
	}
	rb.Write([]byte("g"))
	if got := rb.Snapshot(); !bytes.Equal(got, []byte("defg")) {
		t.Fatalf("snapshot=%q want defg", got)
	}
}

func TestRingBuffer_ManyWritesPreservesLastN(t *testing.T) {
	rb := NewRingBuffer(64)
	for i := 0; i < 200; i++ {
		rb.Write([]byte{byte(i)})
	}
	got := rb.Snapshot()
	if len(got) != 64 {
		t.Fatalf("len=%d want 64", len(got))
	}
	for i, b := range got {
		want := byte(200 - 64 + i)
		if b != want {
			t.Fatalf("byte %d = %d want %d", i, b, want)
		}
	}
}

func TestRingBuffer_EmptyWrite(t *testing.T) {
	rb := NewRingBuffer(8)
	n, err := rb.Write(nil)
	if err != nil || n != 0 {
		t.Fatalf("nil write: n=%d err=%v", n, err)
	}
	if got := rb.Snapshot(); len(got) != 0 {
		t.Fatalf("snapshot=%q want empty", got)
	}
}

func TestRingBuffer_ZeroCapacityCoercedToOne(t *testing.T) {
	rb := NewRingBuffer(0)
	if rb.Cap() != 1 {
		t.Fatalf("cap=%d want 1", rb.Cap())
	}
	rb.Write([]byte("xyz"))
	if got := rb.Snapshot(); !bytes.Equal(got, []byte("z")) {
		t.Fatalf("snapshot=%q want z", got)
	}
}
