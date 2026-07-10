package pipeline

type boundedCapture struct {
	limit     int
	total     int64
	truncated bool
	data      []byte
}

func newBoundedCapture(limit int) *boundedCapture {
	return &boundedCapture{limit: limit}
}

func (b *boundedCapture) Write(p []byte) (int, error) {
	written := len(p)
	b.total += int64(written)
	if written == 0 {
		return 0, nil
	}
	if b.limit <= 0 {
		b.data = append(b.data, p...)
		return written, nil
	}
	if written >= b.limit {
		b.data = append(b.data[:0], p[written-b.limit:]...)
		b.truncated = b.total > int64(b.limit)
		return written, nil
	}
	b.data = append(b.data, p...)
	if overflow := len(b.data) - b.limit; overflow > 0 {
		copy(b.data, b.data[overflow:])
		b.data = b.data[:b.limit]
		b.truncated = true
	}
	return written, nil
}

func (b *boundedCapture) String() string {
	return string(b.data)
}

func (b *boundedCapture) TotalBytes() int64 {
	return b.total
}

func (b *boundedCapture) Truncated() bool {
	return b.truncated
}
