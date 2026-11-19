package immutaapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const testAPIKey = "super-secret-api-key"

func TestNormalizeHost(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		host    string
		want    string
		wantErr bool
	}{
		"bare hostname defaults to https": {host: "example.immuta.com", want: "https://example.immuta.com"},
		"full url is preserved":           {host: "https://example.immuta.com", want: "https://example.immuta.com"},
		"trailing slash is trimmed":       {host: "https://example.immuta.com/", want: "https://example.immuta.com"},
		"base path is preserved":          {host: "https://example.immuta.com/api/", want: "https://example.immuta.com/api"},
		"surrounding space is trimmed":    {host: "  example.immuta.com  ", want: "https://example.immuta.com"},
		"http is allowed":                 {host: "http://localhost:8080", want: "http://localhost:8080"},
		"credentials are dropped":         {host: "https://user:pass@example.immuta.com", want: "https://example.immuta.com"},
		"query is dropped":                {host: "https://example.immuta.com?token=abc", want: "https://example.immuta.com"},
		"empty host is rejected":          {host: "   ", wantErr: true},
		"unsupported scheme is rejected":  {host: "ftp://example.immuta.com", wantErr: true},
		"missing hostname is rejected":    {host: "https:///domain", wantErr: true},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := normalizeHost(test.host)
			if test.wantErr {
				if err == nil {
					t.Fatalf("normalizeHost(%q) = %q, want an error", test.host, got)
				}

				return
			}

			if err != nil {
				t.Fatalf("normalizeHost(%q) returned an unexpected error: %s", test.host, err)
			}

			if got.String() != test.want {
				t.Errorf("normalizeHost(%q) = %q, want %q", test.host, got.String(), test.want)
			}
		})
	}
}

func TestNewClientRequiresCredentials(t *testing.T) {
	t.Parallel()

	if _, err := NewClient("", testAPIKey); err == nil {
		t.Error("NewClient with an empty host should fail")
	}

	if _, err := NewClient("example.immuta.com", "  "); err == nil {
		t.Error("NewClient with an empty API key should fail")
	}
}

func TestNewClientClampsRetryCount(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		retries int
		want    int
	}{
		"negative is clamped to zero": {retries: -5, want: 0},
		"within bounds is kept":       {retries: 3, want: 3},
		"above the maximum is capped": {retries: 99, want: MaxRetryCount},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			client, err := NewClient("example.immuta.com", testAPIKey, WithRetryCount(test.retries))
			if err != nil {
				t.Fatalf("NewClient returned an unexpected error: %s", err)
			}

			if client.retryCount != test.want {
				t.Errorf("retryCount = %d, want %d", client.retryCount, test.want)
			}
		})
	}
}

func TestNewClientDoesNotMutateSuppliedHTTPClient(t *testing.T) {
	t.Parallel()

	supplied := &http.Client{}

	client, err := NewClient("example.immuta.com", testAPIKey, WithHTTPClient(supplied), WithRequestTimeout(5*time.Second))
	if err != nil {
		t.Fatalf("NewClient returned an unexpected error: %s", err)
	}

	if supplied.Timeout != 0 {
		t.Errorf("the supplied http.Client was mutated, Timeout = %s", supplied.Timeout)
	}

	if client.httpClient.Timeout != 5*time.Second {
		t.Errorf("client timeout = %s, want 5s", client.httpClient.Timeout)
	}
}

func TestClientSendsDocumentedHeaders(t *testing.T) {
	t.Parallel()

	var got http.Header

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		writeTestJSON(w, http.StatusOK, `{"id":"abc","name":"Research"}`)
	}))
	t.Cleanup(server.Close)

	client := newTestClient(t, server.URL, WithUserAgent("terraform-provider-immuta/1.2.3"))

	if _, err := client.GetDomain(t.Context(), "abc"); err != nil {
		t.Fatalf("GetDomain returned an unexpected error: %s", err)
	}

	if got.Get("Authorization") != testAPIKey {
		t.Errorf("Authorization header = %q, want the raw API key", got.Get("Authorization"))
	}

	if got.Get("Accept") != "application/json" {
		t.Errorf("Accept header = %q, want application/json", got.Get("Accept"))
	}

	if got.Get("User-Agent") != "terraform-provider-immuta/1.2.3" {
		t.Errorf("User-Agent header = %q", got.Get("User-Agent"))
	}

	if got.Get("Content-Type") != "" {
		t.Errorf("Content-Type header = %q, want it to be unset for a GET without a body", got.Get("Content-Type"))
	}
}

func TestClientSetsContentTypeForRequestsWithABody(t *testing.T) {
	t.Parallel()

	var contentType string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contentType = r.Header.Get("Content-Type")
		writeTestJSON(w, http.StatusOK, `{"id":"abc","name":"Research"}`)
	}))
	t.Cleanup(server.Close)

	client := newTestClient(t, server.URL)

	if _, err := client.CreateDomain(t.Context(), CreateDomainRequest{Name: "Research", Type: DomainTypeDomain}); err != nil {
		t.Fatalf("CreateDomain returned an unexpected error: %s", err)
	}

	if contentType != "application/json" {
		t.Errorf("Content-Type header = %q, want application/json", contentType)
	}
}

func TestClientEscapesPathSegments(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		name string
		want string
	}{
		"plain name":           {name: "Research", want: "/domain/name/Research"},
		"name with a space":    {name: "HR Research", want: "/domain/name/HR%20Research"},
		"name with a slash":    {name: "HR/Research", want: "/domain/name/HR%2FResearch"},
		"name with a percent":  {name: "100% data", want: "/domain/name/100%25%20data"},
		"name with a question": {name: "what?", want: "/domain/name/what%3F"},
	}

	for label, test := range tests {
		t.Run(label, func(t *testing.T) {
			t.Parallel()

			var gotPath string

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.EscapedPath()
				writeTestJSON(w, http.StatusOK, `{"id":"abc","name":"Research"}`)
			}))
			t.Cleanup(server.Close)

			client := newTestClient(t, server.URL)

			if _, err := client.GetDomainByName(t.Context(), test.name); err != nil {
				t.Fatalf("GetDomainByName returned an unexpected error: %s", err)
			}

			if gotPath != test.want {
				t.Errorf("request path = %q, want %q", gotPath, test.want)
			}
		})
	}
}

func TestClientPreservesBasePath(t *testing.T) {
	t.Parallel()

	var gotPath string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		writeTestJSON(w, http.StatusOK, `{"id":"abc","name":"Research"}`)
	}))
	t.Cleanup(server.Close)

	client := newTestClient(t, server.URL+"/gateway/")

	if _, err := client.GetDomain(t.Context(), "abc"); err != nil {
		t.Fatalf("GetDomain returned an unexpected error: %s", err)
	}

	if gotPath != "/gateway/domain/abc" {
		t.Errorf("request path = %q, want /gateway/domain/abc", gotPath)
	}
}

func TestClientRetriesTransientFailuresForIdempotentRequests(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) < 3 {
			writeTestJSON(w, http.StatusServiceUnavailable, `{"message":"try again"}`)

			return
		}

		writeTestJSON(w, http.StatusOK, `{"id":"abc","name":"Research"}`)
	}))
	t.Cleanup(server.Close)

	client := newTestClient(t, server.URL, WithRetryCount(2))

	domain, err := client.GetDomain(t.Context(), "abc")
	if err != nil {
		t.Fatalf("GetDomain returned an unexpected error: %s", err)
	}

	if domain.ID != "abc" {
		t.Errorf("domain id = %q, want abc", domain.ID)
	}

	if got := calls.Load(); got != 3 {
		t.Errorf("server received %d requests, want 3", got)
	}
}

func TestClientDoesNotRetryNonIdempotentRequests(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		writeTestJSON(w, http.StatusServiceUnavailable, `{"message":"try again"}`)
	}))
	t.Cleanup(server.Close)

	client := newTestClient(t, server.URL, WithRetryCount(3))

	if _, err := client.CreateDomain(t.Context(), CreateDomainRequest{Name: "Research", Type: DomainTypeDomain}); err == nil {
		t.Fatal("CreateDomain should fail when the API keeps returning 503")
	}

	if got := calls.Load(); got != 1 {
		t.Errorf("server received %d requests, want exactly 1 so that a domain is never created twice", got)
	}
}

func TestClientDoesNotRetryPermanentFailures(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		writeTestJSON(w, http.StatusForbidden, `{"message":"forbidden"}`)
	}))
	t.Cleanup(server.Close)

	client := newTestClient(t, server.URL, WithRetryCount(3))

	if _, err := client.GetDomain(t.Context(), "abc"); err == nil {
		t.Fatal("GetDomain should fail on 403")
	}

	if got := calls.Load(); got != 1 {
		t.Errorf("server received %d requests, want exactly 1", got)
	}
}

func TestClientHonoursContextCancellation(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() {
		close(release)
		server.Close()
	})

	client := newTestClient(t, server.URL)

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	_, err := client.GetDomain(ctx, "abc")
	if err == nil {
		t.Fatal("GetDomain should fail when the context deadline passes")
	}

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("error = %v, want it to wrap context.DeadlineExceeded", err)
	}
}

func TestClientHonoursRequestTimeout(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() {
		close(release)
		server.Close()
	})

	client := newTestClient(t, server.URL, WithRequestTimeout(50*time.Millisecond), WithRetryCount(0))

	if _, err := client.GetDomain(t.Context(), "abc"); err == nil {
		t.Fatal("GetDomain should fail when the request timeout passes")
	}
}

func TestClientErrorsNeverLeakTheAPIKey(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A hostile or careless API echoing the credential back.
		writeTestJSON(w, http.StatusForbidden, `{"message":"token `+testAPIKey+` is not authorized"}`)
	}))
	t.Cleanup(server.Close)

	client := newTestClient(t, server.URL)

	_, err := client.GetDomain(t.Context(), "abc")
	if err == nil {
		t.Fatal("GetDomain should fail on 403")
	}

	if strings.Contains(err.Error(), testAPIKey) {
		t.Fatal("the error message leaked the API key")
	}

	if !strings.Contains(err.Error(), "<redacted>") {
		t.Errorf("error = %q, want the credential to be redacted", err)
	}
}

func newTestClient(t *testing.T, host string, opts ...Option) *Client {
	t.Helper()

	opts = append([]Option{WithRetryWait(0), WithRetryCount(0)}, opts...)

	client, err := NewClient(host, testAPIKey, opts...)
	if err != nil {
		t.Fatalf("NewClient returned an unexpected error: %s", err)
	}

	return client
}

func writeTestJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}
