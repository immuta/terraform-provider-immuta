package immutaapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPreviewDomainDelete(t *testing.T) {
	t.Parallel()

	var method, path, query string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path, query = r.Method, r.URL.Path, r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"dataSourceCount": 1, "globalPolicyCount": 2, "identifierCount": 3, "dataProductCount": 4, "other": true}`))
	}))
	t.Cleanup(server.Close)

	impact, err := newTestClient(t, server.URL).PreviewDomainDelete(t.Context(), "d1")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	// The dry run must never be sent without dryRun=true, or it would
	// delete the domain.
	if method != http.MethodDelete || path != "/domain/d1" || query != "dryRun=true" {
		t.Errorf("request = %s %s?%s, want DELETE /domain/d1?dryRun=true", method, path, query)
	}

	want := DomainDeleteImpact{DataSourceCount: 1, GlobalPolicyCount: 2, IdentifierCount: 3, DataProductCount: 4}
	if impact != want {
		t.Errorf("impact = %+v, want %+v", impact, want)
	}
}

func TestPreviewDomainDeleteRejectsInvalidResponses(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"dataSourceCount": "many"}`))
	}))
	t.Cleanup(server.Close)

	if _, err := newTestClient(t, server.URL).PreviewDomainDelete(t.Context(), "d1"); err == nil {
		t.Error("expected an error for a count that is not a number")
	}
	if _, err := newTestClient(t, server.URL).PreviewDomainDelete(t.Context(), " "); err == nil {
		t.Error("expected an error without a domain id")
	}
}
