package capsulecache

import (
	"bytes"
	"net/http"
	"sync"
)

// ResponseRecorder captures a response written by the handler.
type ResponseRecorder struct {
	mu         sync.Mutex
	status     int
	header     http.Header
	body       *bytes.Buffer
	capReached bool

	underlying http.ResponseWriter // original writer, used only for Flush
	maxBytes   int64
	written    bool
}

func NewResponseRecorder(responseWriter http.ResponseWriter, maxBodyBytes int64) *ResponseRecorder {
	return &ResponseRecorder{
		status:     http.StatusOK,
		header:     make(http.Header),
		body:       &bytes.Buffer{},
		underlying: responseWriter,
		maxBytes:   maxBodyBytes,
	}
}

// Header implements http.ResponseWriter
func (r *ResponseRecorder) Header() http.Header {
	return r.header
}

// Write implements http.ResponseWriter
func (r *ResponseRecorder) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Copy to buffer up to max
	if r.maxBytes <= 0 {
		// disabled - still record but beware
		n, _ := r.body.Write(p)
		return n, nil
	}

	remaining := r.maxBytes - int64(r.body.Len())
	if remaining <= 0 {
		r.capReached = true
		// still accept the write but don't grow buffer
		return len(p), nil
	}

	if int64(len(p)) <= remaining {
		return r.body.Write(p)
	}

	// partial write to buffer up to remaining
	n, _ := r.body.Write(p[:remaining])
	r.capReached = true
	return n, nil
}

// WriteHeader implements http.ResponseWriter
func (r *ResponseRecorder) WriteHeader(status int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.status = status
}

// Body returns the recorded body bytes.
func (r *ResponseRecorder) Body() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.body.Bytes()
}

func (r *ResponseRecorder) StatusCode() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.status
}

// Flush writes recorded headers, status and body to the underlying writer (original client).
// It must be called only once (it's safe if called multiple times but will not duplicate content).
func (r *ResponseRecorder) Flush() {
	r.mu.Lock()
	if r.written {
		r.mu.Unlock()
		return
	}
	r.written = true
	hdr := r.header.Clone()
	status := r.status
	body := r.body.Bytes()
	r.mu.Unlock()

	// write headers
	dest := r.underlying
	for k, vv := range hdr {
		for _, v := range vv {
			dest.Header().Add(k, v)
		}
	}

	// If status not explicitly set by handler, default to 200 (Go default).
	dest.WriteHeader(status)
	if len(body) > 0 {
		_, _ = dest.Write(body)
	}
}
