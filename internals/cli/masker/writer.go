// Package masker provides a wrapper around an io.Writer that replaces sensitive values in its output.
package masker

import (
	"io"
	"sync"
	"time"
)

// matcher is an interface used by MaskedWriter to find matches of sequences to mask.
type matcher interface {
	Read(byte) int
	InProgress() bool
	Reset()
}

type sequenceMatcher struct {
	sequence     []byte
	currentIndex int
}

// Read takes in a new byte to match against.
// If the given byte results in a match with sequence, the number of matched bytes is returned.
func (m *sequenceMatcher) Read(in byte) int {
	if m.sequence[m.currentIndex] == in {
		m.currentIndex++

		if m.currentIndex == len(m.sequence) {
			m.currentIndex = 0
			return len(m.sequence)
		}
		return 0
	}

	m.currentIndex -= m.findShift()
	if m.sequence[m.currentIndex] == in {
		return m.Read(in)
	}
	return 0
}

// findShift checks whether we can also make a partial match by decreasing the currentIndex .
// For example, if the sequence is foofoobar, if someone inserts foofoofoobar, we still want to match.
// So after the third f is inserted, the currentIndex is decreased by 3 with the following code.
func (m *sequenceMatcher) findShift() int {
	for offset := 1; offset <= m.currentIndex; offset++ {
		ok := true
		for i := 0; i < m.currentIndex-offset; i++ {
			if m.sequence[i] != m.sequence[i+offset] {
				ok = false
				break
			}
		}
		if ok {
			return offset
		}
	}
	return m.currentIndex
}

// InProgress returns whether this sequenceMatcher is currently partially matching.
//
// For example, if the sequence is "foobar" and the registered input is "foob", InProgress() returns true.
func (m *sequenceMatcher) InProgress() bool {
	return m.currentIndex > 0
}

// Reset forgets the current match.
func (m *sequenceMatcher) Reset() {
	m.currentIndex = 0
}

// maskByte represents a byte and whether the byte should be masked or not.
type maskByte struct {
	byte
	masked bool
}

// MaskedWriter wraps an io.Writer which masks all occurrences of masks by maskString.
// If no write is made for timeout on the io.Writer, any matches in progress are reset
// and the buffer is flushed. This is to ensure that the writer does not hang on partial matches.
type MaskedWriter struct {
	w          io.Writer
	maskString string
	matchers   []matcher
	timeout    time.Duration

	buf    []maskByte
	lock   *sync.Mutex
	output chan []maskByte
	err    error
	nIn    int64
	nOut   int64
}

// NewMaskedWriter returns a new MaskedWriter that masks all occurrences of sequences in masks with maskString.
func NewMaskedWriter(w io.Writer, masks [][]byte, maskString string, timeout time.Duration) *MaskedWriter {
	var lock sync.Mutex
	matchers := make([]matcher, len(masks))
	for i, mask := range masks {
		matchers[i] = &sequenceMatcher{
			sequence: mask,
		}
	}
	return &MaskedWriter{
		w:          w,
		maskString: maskString,
		matchers:   matchers,
		timeout:    timeout,
		lock:       &lock,
		output:     make(chan []maskByte, 1),
	}
}

// Write implements Write from io.Writer
// It is responsible for finding any matches to mask and mark the appropriate bytes as masked.
// This function never returns an error. These can instead be caught with Flush().
func (mw *MaskedWriter) Write(p []byte) (n int, err error) {
	for _, b := range p {
		matchInProgress := false

		mw.lock.Lock()
		mw.buf = append(mw.buf, maskByte{byte: b})

		for _, matcher := range mw.matchers {
			maskLen := matcher.Read(b)
			for i := 0; i < maskLen; i++ {
				mw.buf[len(mw.buf)-1-i].masked = true
			}
			matchInProgress = matchInProgress || matcher.InProgress()
		}

		if !matchInProgress {
			mw.flushBuffer()
		}

		mw.lock.Unlock()
	}

	mw.nIn += int64(len(p))

	return len(p), nil
}

func (mw *MaskedWriter) flushBuffer() {
	tmp := make([]maskByte, len(mw.buf))
	copy(tmp, mw.buf)
	mw.output <- tmp
	mw.buf = mw.buf[:0]
}

// Run writes any processed data from the output channel to the underlying io.Writer.
// If no new data is received on the output channel for timeout, the output buffer is forced flushed
// and all ongoing matches are reset.
//
// This should be run in a separate goroutine.
func (mw *MaskedWriter) Run() {
	masking := false
	for {
		select {
		case <-time.After(mw.timeout):
			mw.lock.Lock()
			if len(mw.output) == 0 {
				for _, matcher := range mw.matchers {
					matcher.Reset()
				}
				mw.flushBuffer()
			}
			mw.lock.Unlock()
		case output := <-mw.output:
			for _, b := range output {
				var err error
				if b.masked {
					if !masking {
						_, err = mw.w.Write([]byte(mw.maskString))
						if err != nil {
							mw.err = err
							return
						}
					}
					masking = true
				} else {
					_, err = mw.w.Write([]byte{b.byte})
					if err != nil {
						mw.err = err
						return
					}
					masking = false
				}
			}
			mw.nOut += int64(len(output))
		}
	}
}

// Flush is called to make sure that all output is written to the underlying io.Writer.
// Returns any errors caused by the writing.
func (mw *MaskedWriter) Flush() error {
	for mw.nIn != mw.nOut && mw.err == nil {
		time.Sleep(time.Microsecond)
	}
	return mw.err
}
