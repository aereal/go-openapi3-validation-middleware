package openapi3middleware

import (
	"bytes"
	"net/http"
)

func newBufferingResponseWriter(rw http.ResponseWriter) *bufferingResponseWriter {
	return &bufferingResponseWriter{rw: rw, buf: new(bytes.Buffer)}
}

type bufferingResponseWriter struct {
	buf        *bytes.Buffer
	rw         http.ResponseWriter
	statusCode int
}

func (rw *bufferingResponseWriter) emit() {
	if rw.statusCode != 0 {
		rw.rw.WriteHeader(rw.statusCode)
	}
	_, _ = rw.buf.WriteTo(rw.rw) //nolint:errcheck
}

func (rw *bufferingResponseWriter) Write(b []byte) (int, error) {
	return rw.buf.Write(b)
}

func (rw *bufferingResponseWriter) Header() http.Header {
	return rw.rw.Header()
}

func (rw *bufferingResponseWriter) WriteHeader(statusCode int) {
	rw.statusCode = statusCode
}
