package session

import "sync"

// RingBuffer is a fixed-capacity byte buffer. When full, the oldest bytes are
// overwritten silently. Snapshot returns the most recent Size() bytes in
// chronological order.
type RingBuffer struct {
	mu   sync.Mutex
	buf  []byte
	head int // index of the oldest byte when size == cap, otherwise unused
	size int
}

// NewRingBuffer returns a buffer with the given byte capacity. A zero or
// negative capacity is treated as 1 byte.
func NewRingBuffer(capacity int) *RingBuffer {
	if capacity < 1 {
		capacity = 1
	}
	return &RingBuffer{buf: make([]byte, capacity)}
}

// Cap returns the buffer's byte capacity.
func (r *RingBuffer) Cap() int { return cap(r.buf) }

// Size returns the number of valid bytes currently stored.
func (r *RingBuffer) Size() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.size
}

// Write appends p to the buffer, evicting the oldest bytes if necessary.
// It always reports n == len(p) and a nil error.
func (r *RingBuffer) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	c := cap(r.buf)
	// Fast path: incoming chunk is at least the full capacity. Keep only the
	// last c bytes and reset head to zero (chronological order = simple).
	if len(p) >= c {
		copy(r.buf, p[len(p)-c:])
		r.head = 0
		r.size = c
		return len(p), nil
	}

	if r.size < c {
		// We have free space at the tail (head == 0 in growth phase).
		free := c - r.size
		if len(p) <= free {
			copy(r.buf[r.size:], p)
			r.size += len(p)
			return len(p), nil
		}
		// Fill the free part, then we are full and the rest must wrap.
		copy(r.buf[r.size:], p[:free])
		rest := p[free:]
		// We are now exactly full; subsequent writes use the wrap-around path.
		r.size = c
		r.head = 0
		// Wrap path inlined:
		writeAtHead(r.buf, &r.head, rest)
		return len(p), nil
	}

	// Full: overwrite starting at head, advancing head.
	writeAtHead(r.buf, &r.head, p)
	return len(p), nil
}

// writeAtHead writes p into buf treated as a circular buffer whose oldest
// byte is at *head. After the write, *head points to the new oldest byte.
// Caller must guarantee len(p) < cap(buf).
func writeAtHead(buf []byte, head *int, p []byte) {
	c := cap(buf)
	pos := (*head + c) % c // current head; the slot to overwrite
	first := c - pos
	if len(p) <= first {
		copy(buf[pos:], p)
	} else {
		copy(buf[pos:], p[:first])
		copy(buf, p[first:])
	}
	*head = (pos + len(p)) % c
}

// Snapshot returns a fresh slice containing the buffer's current contents in
// chronological order (oldest byte first).
func (r *RingBuffer) Snapshot() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]byte, r.size)
	if r.size == 0 {
		return out
	}
	c := cap(r.buf)
	if r.size < c {
		copy(out, r.buf[:r.size])
		return out
	}
	// Full: data starts at r.head and wraps.
	first := c - r.head
	copy(out, r.buf[r.head:])
	copy(out[first:], r.buf[:r.head])
	return out
}
