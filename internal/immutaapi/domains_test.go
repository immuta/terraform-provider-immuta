package immutaapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateDomainSendsTheDocumentedBody(t *testing.T) {
	t.Parallel()

	emptyDescription := ""
	description := "Research domain"

	tests := map[string]struct {
		request CreateDomainRequest
		want    map[string]any
	}{
		"without a description": {
			request: CreateDomainRequest{Name: "Research", Type: DomainTypeDomain},
			want:    map[string]any{"name": "Research", "type": "domain"},
		},
		"with a description": {
			request: CreateDomainRequest{Name: "Research", Type: DomainTypeDomain, Description: &description},
			want:    map[string]any{"name": "Research", "type": "domain", "description": "Research domain"},
		},
		"with an explicitly empty description": {
			request: CreateDomainRequest{Name: "Research", Type: DomainTypeDomain, Description: &emptyDescription},
			want:    map[string]any{"name": "Research", "type": "domain", "description": ""},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var gotBody map[string]any
			var gotMethod, gotPath string

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod, gotPath = r.Method, r.URL.EscapedPath()
				raw, _ := io.ReadAll(r.Body)
				if err := json.Unmarshal(raw, &gotBody); err != nil {
					t.Errorf("request body is not valid JSON: %s", err)
				}
				writeTestJSON(w, http.StatusOK, `{"id":"abc","name":"Research","type":"domain"}`)
			}))
			t.Cleanup(server.Close)

			client := newTestClient(t, server.URL)

			domain, err := client.CreateDomain(t.Context(), test.request)
			if err != nil {
				t.Fatalf("CreateDomain returned an unexpected error: %s", err)
			}

			if domain.ID != "abc" {
				t.Errorf("domain id = %q, want abc", domain.ID)
			}

			if gotMethod != http.MethodPost || gotPath != "/domain" {
				t.Errorf("request = %s %s, want POST /domain", gotMethod, gotPath)
			}

			if len(gotBody) != len(test.want) {
				t.Errorf("request body = %v, want exactly the keys of %v", gotBody, test.want)
			}

			for key, want := range test.want {
				if got, ok := gotBody[key]; !ok || got != want {
					t.Errorf("request body[%q] = %v, want %v", key, got, want)
				}
			}
		})
	}
}

func TestGetDomainDecodesDocumentedResponseShapes(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		body        string
		wantName    string
		wantErr     bool
		wantMissing bool
	}{
		"bare object": {
			body:     `{"id":"abc","name":"Research","type":"domain"}`,
			wantName: "Research",
		},
		"data object envelope": {
			body:     `{"data":{"id":"abc","name":"Research","type":"domain"}}`,
			wantName: "Research",
		},
		"data array envelope": {
			body:     `{"data":[{"id":"abc","name":"Research","type":"domain"}],"count":1}`,
			wantName: "Research",
		},
		"empty data array is not found": {
			body:        `{"data":[],"count":0}`,
			wantMissing: true,
		},
		"response without an id is rejected": {
			body:    `{"name":"Research"}`,
			wantErr: true,
		},
		"invalid json is rejected": {
			body:    `not json`,
			wantErr: true,
		},
		"empty body is rejected": {
			body:    ``,
			wantErr: true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				writeTestJSON(w, http.StatusOK, test.body)
			}))
			t.Cleanup(server.Close)

			client := newTestClient(t, server.URL)

			domain, err := client.GetDomain(t.Context(), "abc")

			switch {
			case test.wantMissing:
				if !IsNotFound(err) {
					t.Fatalf("error = %v, want a not found error", err)
				}
			case test.wantErr:
				if err == nil {
					t.Fatalf("GetDomain returned %+v, want an error", domain)
				}
			default:
				if err != nil {
					t.Fatalf("GetDomain returned an unexpected error: %s", err)
				}
				if domain.Name != test.wantName {
					t.Errorf("domain name = %q, want %q", domain.Name, test.wantName)
				}
			}
		})
	}
}

func TestGetDomainDecodesSettingsAndMetadata(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeTestJSON(w, http.StatusOK, `{
			"id":"abc",
			"name":"Research",
			"description":"Research domain",
			"type":"domain",
			"assignmentType":"manual",
			"settings":{"autoscanning":true},
			"createdBy":2,
			"createdAt":"2024-01-01T00:00:00.000Z",
			"updatedAt":"2024-01-02T00:00:00.000Z"
		}`)
	}))
	t.Cleanup(server.Close)

	client := newTestClient(t, server.URL)

	domain, err := client.GetDomain(t.Context(), "abc")
	if err != nil {
		t.Fatalf("GetDomain returned an unexpected error: %s", err)
	}

	autoscanning, ok := domain.Autoscanning()
	if !ok || !autoscanning {
		t.Errorf("Autoscanning() = (%t, %t), want (true, true)", autoscanning, ok)
	}

	if domain.AssignmentType != "manual" {
		t.Errorf("assignment type = %q, want manual", domain.AssignmentType)
	}

	if domain.CreatedBy == nil || *domain.CreatedBy != 2 {
		t.Errorf("created by = %v, want 2", domain.CreatedBy)
	}

	if domain.UpdatedAt != "2024-01-02T00:00:00.000Z" {
		t.Errorf("updated at = %q", domain.UpdatedAt)
	}
}

func TestAutoscanningIsUnsetWhenTheAPIOmitsSettings(t *testing.T) {
	t.Parallel()

	domain := &Domain{ID: "abc"}

	if _, ok := domain.Autoscanning(); ok {
		t.Error("Autoscanning() should report that the value is unset")
	}

	var nilDomain *Domain
	if _, ok := nilDomain.Autoscanning(); ok {
		t.Error("Autoscanning() on a nil domain should report that the value is unset")
	}
}

func TestGetDomainReturnsNotFound(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeTestJSON(w, http.StatusNotFound, `{"message":"domain not found"}`)
	}))
	t.Cleanup(server.Close)

	client := newTestClient(t, server.URL)

	_, err := client.GetDomain(t.Context(), "abc")
	if !IsNotFound(err) {
		t.Fatalf("error = %v, want a not found error", err)
	}
}

func TestDomainRequestsRequireAnIdentifier(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("the client should not send a request without an identifier")
		writeTestJSON(w, http.StatusOK, `{}`)
	}))
	t.Cleanup(server.Close)

	client := newTestClient(t, server.URL)

	if _, err := client.GetDomain(t.Context(), "  "); err == nil {
		t.Error("GetDomain with an empty id should fail")
	}

	if _, err := client.UpdateDomain(t.Context(), "", UpdateDomainRequest{}); err == nil {
		t.Error("UpdateDomain with an empty id should fail")
	}

	if err := client.DeleteDomain(t.Context(), ""); err == nil {
		t.Error("DeleteDomain with an empty id should fail")
	}

	if _, err := client.GetDomainByName(t.Context(), " "); err == nil {
		t.Error("GetDomainByName with an empty name should fail")
	}
}

func TestUpdateDomainSendsOnlyTheSuppliedFields(t *testing.T) {
	t.Parallel()

	name := "Renamed"
	description := "Updated description"
	cleared := ""

	tests := map[string]struct {
		request UpdateDomainRequest
		want    map[string]any
	}{
		"name only": {
			request: UpdateDomainRequest{Name: &name},
			want:    map[string]any{"name": "Renamed"},
		},
		"description only": {
			request: UpdateDomainRequest{Description: &description},
			want:    map[string]any{"description": "Updated description"},
		},
		"clearing the description sends an empty string": {
			request: UpdateDomainRequest{Description: &cleared},
			want:    map[string]any{"description": ""},
		},
		"both attributes": {
			request: UpdateDomainRequest{Name: &name, Description: &description},
			want:    map[string]any{"name": "Renamed", "description": "Updated description"},
		},
	}

	for label, test := range tests {
		t.Run(label, func(t *testing.T) {
			t.Parallel()

			var gotBody map[string]any
			var gotMethod, gotPath string

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod, gotPath = r.Method, r.URL.EscapedPath()
				raw, _ := io.ReadAll(r.Body)
				if err := json.Unmarshal(raw, &gotBody); err != nil {
					t.Errorf("request body is not valid JSON: %s", err)
				}
				writeTestJSON(w, http.StatusOK, `{"id":"abc","name":"Renamed","type":"domain"}`)
			}))
			t.Cleanup(server.Close)

			client := newTestClient(t, server.URL)

			if _, err := client.UpdateDomain(t.Context(), "abc", test.request); err != nil {
				t.Fatalf("UpdateDomain returned an unexpected error: %s", err)
			}

			if gotMethod != http.MethodPut || gotPath != "/domain/abc" {
				t.Errorf("request = %s %s, want PUT /domain/abc", gotMethod, gotPath)
			}

			if len(gotBody) != len(test.want) {
				t.Errorf("request body = %v, want exactly the keys of %v", gotBody, test.want)
			}

			for key, want := range test.want {
				if got, ok := gotBody[key]; !ok || got != want {
					t.Errorf("request body[%q] = %v, want %v", key, got, want)
				}
			}
		})
	}
}

func TestUpdateDomainToleratesAnEmptyResponseBody(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	client := newTestClient(t, server.URL)

	name := "Renamed"

	domain, err := client.UpdateDomain(t.Context(), "abc", UpdateDomainRequest{Name: &name})
	if err != nil {
		t.Fatalf("UpdateDomain returned an unexpected error: %s", err)
	}

	if domain != nil {
		t.Errorf("domain = %+v, want nil when the API returns no body", domain)
	}
}

func TestUpdateDomainRequestIsEmpty(t *testing.T) {
	t.Parallel()

	value := "value"

	if !(UpdateDomainRequest{}).IsEmpty() {
		t.Error("an update request without fields should be empty")
	}

	if (UpdateDomainRequest{Name: &value}).IsEmpty() {
		t.Error("an update request with a name should not be empty")
	}

	if (UpdateDomainRequest{Description: &value}).IsEmpty() {
		t.Error("an update request with a description should not be empty")
	}
}

func TestDeleteDomain(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		status      int
		body        string
		wantMissing bool
		wantErr     bool
	}{
		"no content":        {status: http.StatusNoContent},
		"ok with a payload": {status: http.StatusOK, body: `{"id":"abc"}`},
		"not found":         {status: http.StatusNotFound, body: `{"message":"domain not found"}`, wantMissing: true},
		"conflict":          {status: http.StatusConflict, body: `{"message":"domain is not empty"}`, wantErr: true},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var gotMethod, gotPath string

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod, gotPath = r.Method, r.URL.EscapedPath()
				writeTestJSON(w, test.status, test.body)
			}))
			t.Cleanup(server.Close)

			client := newTestClient(t, server.URL)

			err := client.DeleteDomain(t.Context(), "abc")

			switch {
			case test.wantMissing:
				if !IsNotFound(err) {
					t.Fatalf("error = %v, want a not found error", err)
				}
			case test.wantErr:
				if err == nil {
					t.Fatal("DeleteDomain should fail")
				}
				if IsNotFound(err) {
					t.Fatal("a conflict must not be reported as a missing domain")
				}
			default:
				if err != nil {
					t.Fatalf("DeleteDomain returned an unexpected error: %s", err)
				}
			}

			if gotMethod != http.MethodDelete || gotPath != "/domain/abc" {
				t.Errorf("request = %s %s, want DELETE /domain/abc", gotMethod, gotPath)
			}
		})
	}
}

func TestListDomains(t *testing.T) {
	t.Parallel()

	var gotQuery string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		writeTestJSON(w, http.StatusOK, `{"data":[{"id":"abc","name":"Research"},{"id":"def","name":"Finance"}],"count":2}`)
	}))
	t.Cleanup(server.Close)

	client := newTestClient(t, server.URL)

	offset, size, exact := 0, 10, true

	domains, err := client.ListDomains(t.Context(), ListDomainsOptions{
		Offset:       &offset,
		Size:         &size,
		SearchText:   "Research domain",
		IsExactMatch: &exact,
	})
	if err != nil {
		t.Fatalf("ListDomains returned an unexpected error: %s", err)
	}

	if len(domains) != 2 {
		t.Fatalf("got %d domains, want 2", len(domains))
	}

	want := "isExactMatch=true&offset=0&searchText=Research+domain&size=10"
	if gotQuery != want {
		t.Errorf("query = %q, want %q", gotQuery, want)
	}
}

func TestListDomainsOmitsUnsetOptions(t *testing.T) {
	t.Parallel()

	var gotQuery string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		writeTestJSON(w, http.StatusOK, `{"data":[],"count":0}`)
	}))
	t.Cleanup(server.Close)

	client := newTestClient(t, server.URL)

	domains, err := client.ListDomains(t.Context(), ListDomainsOptions{})
	if err != nil {
		t.Fatalf("ListDomains returned an unexpected error: %s", err)
	}

	if len(domains) != 0 {
		t.Errorf("got %d domains, want none", len(domains))
	}

	if gotQuery != "" {
		t.Errorf("query = %q, want it to be empty", gotQuery)
	}
}

func TestSetDomainAutoscanningSendsTheDocumentedBody(t *testing.T) {
	t.Parallel()

	for _, autoscanning := range []bool{true, false} {
		t.Run(fmt.Sprint(autoscanning), func(t *testing.T) {
			t.Parallel()

			var gotBody map[string]any
			var gotMethod, gotPath string

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod, gotPath = r.Method, r.URL.EscapedPath()
				raw, _ := io.ReadAll(r.Body)
				if err := json.Unmarshal(raw, &gotBody); err != nil {
					t.Errorf("request body is not valid JSON: %s", err)
				}
				writeTestJSON(w, http.StatusOK, fmt.Sprintf(`{"autoscanning":%t}`, autoscanning))
			}))
			t.Cleanup(server.Close)

			client := newTestClient(t, server.URL)

			if err := client.SetDomainAutoscanning(t.Context(), "abc", autoscanning); err != nil {
				t.Fatalf("SetDomainAutoscanning returned an unexpected error: %s", err)
			}

			if gotMethod != http.MethodPut || gotPath != "/domain/abc/settings" {
				t.Errorf("request = %s %s, want PUT /domain/abc/settings", gotMethod, gotPath)
			}
			// false is sent explicitly, not omitted.
			if len(gotBody) != 1 || gotBody["autoscanning"] != autoscanning {
				t.Errorf("request body = %v, want exactly {\"autoscanning\": %t}", gotBody, autoscanning)
			}
		})
	}
}

func TestSetDomainAutoscanningRequiresAnIdentifier(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, "http://127.0.0.1:1")

	if err := client.SetDomainAutoscanning(t.Context(), " ", true); err == nil {
		t.Error("SetDomainAutoscanning accepted an empty domain ID")
	}
}
