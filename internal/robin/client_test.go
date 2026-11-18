package robin

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
)

func TestConfirmEvent(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != http.MethodPut || r.URL.Path != "/v1.0/events/123/confirmation" {
			t.Errorf("confirmation request = %s %s, want PUT /v1.0/events/123/confirmation", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(server.Close)
	serverURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}

	client := New().WithSession(&Session{AccessToken: "test-token"})
	client.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		req := r.Clone(r.Context())
		req.URL.Scheme = serverURL.Scheme
		req.URL.Host = serverURL.Host
		return server.Client().Transport.RoundTrip(req)
	})

	if err := client.ConfirmEvent("123"); err != nil {
		t.Fatalf("ConfirmEvent with HTTP 201: %v", err)
	}
	if got := requests.Load(); got != 1 {
		t.Errorf("confirmation requests = %d, want 1", got)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
