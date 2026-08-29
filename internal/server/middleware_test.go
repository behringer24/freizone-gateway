package server

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/behringer24/freizone-gateway/internal/config"
)

// readBodyHandler reads the whole request body the way auth.Middleware
// does -- unconditionally, before deciding anything about the caller --
// and reports what it got.
func readBodyHandler(read *int, readErr *error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		*read = len(body)
		*readErr = err
	})
}

func TestWithMaxBodyAllowsBodyAtTheLimit(t *testing.T) {
	var read int
	var readErr error
	handler := withMaxBody(readBodyHandler(&read, &readErr))

	body := strings.Repeat("a", maxRequestBodyBytes)
	req := httptest.NewRequest(http.MethodPost, "/v1/push/send", strings.NewReader(body))
	handler.ServeHTTP(httptest.NewRecorder(), req)

	if readErr != nil {
		t.Fatalf("reading a body at the limit failed: %v", readErr)
	}
	if read != maxRequestBodyBytes {
		t.Errorf("read %d bytes, want %d", read, maxRequestBodyBytes)
	}
}

func TestWithMaxBodyRejectsOversizedBody(t *testing.T) {
	var read int
	var readErr error
	handler := withMaxBody(readBodyHandler(&read, &readErr))

	// Far past the limit, and far past what any legitimate caller sends:
	// this is the shape of the request the cap exists to stop.
	body := strings.Repeat("a", maxRequestBodyBytes*4)
	req := httptest.NewRequest(http.MethodPost, "/v1/push/send", strings.NewReader(body))
	handler.ServeHTTP(httptest.NewRecorder(), req)

	if readErr == nil {
		t.Fatal("reading an oversized body succeeded; the cap is not in effect")
	}
	var maxErr *http.MaxBytesError
	if !errors.As(readErr, &maxErr) {
		t.Errorf("read error = %v, want a *http.MaxBytesError", readErr)
	}
	if read > maxRequestBodyBytes {
		t.Errorf("handler still saw %d bytes, more than the %d cap", read, maxRequestBodyBytes)
	}
}

// TestNewCapsBodiesAheadOfTheHandler is the one that matters for the
// defect this closes: the cap has to be wrapped around the router by
// New, not applied inside a handler, because authentication reads the
// body before it knows whether the caller is anyone at all.
func TestNewCapsBodiesAheadOfTheHandler(t *testing.T) {
	var read int
	var readErr error
	srv, err := New(Options{TLSMode: config.TLSModeOff, HTTPAddr: ":0", Handler: readBodyHandler(&read, &readErr)})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	body := strings.Repeat("a", maxRequestBodyBytes*4)
	req := httptest.NewRequest(http.MethodPost, "/v1/push/send", strings.NewReader(body))
	srv.servers[0].Handler.ServeHTTP(httptest.NewRecorder(), req)

	if readErr == nil {
		t.Fatal("a handler wired up through New read an unbounded body")
	}
}

func TestNewSetsAllFourTimeouts(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts Options
	}{
		{"off", Options{TLSMode: config.TLSModeOff, HTTPAddr: ":0"}},
		{"manual", Options{TLSMode: config.TLSModeManual, HTTPSAddr: ":0"}},
		{"autocert", Options{TLSMode: config.TLSModeAutocert, Domain: "gateway.example", HTTPAddr: ":0", HTTPSAddr: ":0"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.opts.Handler = http.NotFoundHandler()
			srv, err := New(tc.opts)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			for i, s := range srv.servers {
				if s.ReadHeaderTimeout == 0 {
					t.Errorf("server %d: ReadHeaderTimeout is 0 -- Slowloris is unbounded", i)
				}
				if s.ReadTimeout == 0 {
					t.Errorf("server %d: ReadTimeout is 0", i)
				}
				if s.WriteTimeout == 0 {
					t.Errorf("server %d: WriteTimeout is 0", i)
				}
				if s.IdleTimeout == 0 {
					t.Errorf("server %d: IdleTimeout is 0 -- idle connections are held forever", i)
				}
			}
		})
	}
}
