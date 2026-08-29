package server

import (
	"log/slog"
	"net/http"
	"time"
)

// maxRequestBodyBytes caps every request body. The largest legitimate body
// this gateway accepts is a push-send request -- a platform name and one
// registration token, a few hundred bytes at most -- so this leaves well
// over an order of magnitude of headroom while still bounding what an
// unauthenticated caller can make the process allocate.
//
// Deliberately a constant rather than a config knob, which is where this
// departs from freizone-server's equivalent: that server has blob uploads
// whose ceiling an operator has a real reason to tune, whereas every body
// this gateway accepts has a shape fixed by the protocol.
const maxRequestBodyBytes = 8 << 10 // 8 KiB

// withMaxBody caps every request body before any handler reads it -- and,
// the point of it being here rather than in a handler, before
// authentication reads it.
//
// auth.Middleware has to read the entire body to verify the signature
// computed over it, which it necessarily does before it can know whether
// the caller is anyone at all. Without this cap, that read is an
// unauthenticated, unbounded allocation: one request with a large enough
// body exhausts memory, so the capacity of the whole process is one
// request. http.MaxBytesReader instead hands the reader an error once the
// limit is passed, and the request fails as a bad one.
func withMaxBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
		next.ServeHTTP(w, r)
	})
}

// withLogging logs method, path, status code, and duration for each request.
func withLogging(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		if logger != nil {
			logger.Info("request", "method", r.Method, "path", r.URL.Path, "status", sw.status, "duration", time.Since(start))
		}
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// withRecover recovers from panics in next, logging them and returning a
// generic 500 instead of crashing the process.
func withRecover(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				if logger != nil {
					logger.Error("panic recovered", "error", rec, "path", r.URL.Path)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte(`{"error":{"code":"internal","message":"internal server error"}}`))
			}
		}()
		next.ServeHTTP(w, r)
	})
}
