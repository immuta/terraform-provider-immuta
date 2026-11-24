package immutaapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

const identifierResponse = `{
	"id": "a32c69df-3707-4b03-8144-a8d11d1f52a4",
	"name": "Primary colors",
	"description": "Columns that hold a primary color",
	"type": "regex",
	"config": {"regex": "(red|green|blue)"},
	"tags": [{"id": 42, "name": "Discovered.Primary color", "displayName": "Primary color"}],
	"domain": {"id": "domain-1", "name": "Finance"},
	"createdAt": "2024-01-01T00:00:00.000Z"
}`

func TestCreateIdentifierSendsTheDocumentedBody(t *testing.T) {
	t.Parallel()

	var gotBody map[string]any
	var gotMethod, gotPath string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.EscapedPath()
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &gotBody); err != nil {
			t.Errorf("request body is not valid JSON: %s", err)
		}
		writeTestJSON(w, http.StatusOK, identifierResponse)
	}))
	t.Cleanup(server.Close)

	client := newTestClient(t, server.URL)

	request := NewSaveIdentifierRequest(
		"Primary colors",
		"Columns that hold a primary color",
		IdentifierTypeRegex,
		IdentifierConfig{Regex: "(red|green|blue)"},
		[]string{"Discovered.Primary color"},
	)
	domainID := "domain-1"
	request.DomainID = &domainID

	identifier, err := client.CreateIdentifier(t.Context(), request)
	if err != nil {
		t.Fatalf("CreateIdentifier returned an unexpected error: %s", err)
	}

	if identifier.ID != "a32c69df-3707-4b03-8144-a8d11d1f52a4" {
		t.Errorf("identifier id = %q, want the id from the response", identifier.ID)
	}
	if identifier.DomainID() != "domain-1" {
		t.Errorf("domain id = %q, want domain-1", identifier.DomainID())
	}
	if got := identifier.TagNames(); !reflect.DeepEqual(got, []string{"Discovered.Primary color"}) {
		t.Errorf("tag names = %v, want [Discovered.Primary color]", got)
	}

	if gotMethod != http.MethodPost || gotPath != "/sdd/identifier" {
		t.Errorf("request = %s %s, want POST /sdd/identifier", gotMethod, gotPath)
	}

	want := map[string]any{
		"name":        "Primary colors",
		"description": "Columns that hold a primary color",
		"domainId":    "domain-1",
		"type":        "regex",
		"config":      map[string]any{"regex": "(red|green|blue)"},
		"tags":        []any{map[string]any{"name": "Discovered.Primary color"}},
	}
	if !reflect.DeepEqual(gotBody, want) {
		t.Errorf("request body = %v, want %v", gotBody, want)
	}
}

func TestCreateIdentifierOmitsTheDomainWhenUnset(t *testing.T) {
	t.Parallel()

	var gotBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		writeTestJSON(w, http.StatusOK, identifierResponse)
	}))
	t.Cleanup(server.Close)

	client := newTestClient(t, server.URL)

	_, err := client.CreateIdentifier(t.Context(), NewSaveIdentifierRequest(
		"Primary colors", "A description", IdentifierTypeRegex,
		IdentifierConfig{Regex: "x"}, []string{"Discovered.Primary color"},
	))
	if err != nil {
		t.Fatalf("CreateIdentifier returned an unexpected error: %s", err)
	}

	if _, present := gotBody["domainId"]; present {
		t.Error("the request sent domainId for an identifier that is not in a domain")
	}
}

func TestIdentifierRequestsUseTheDocumentedPaths(t *testing.T) {
	t.Parallel()

	validRequest := NewSaveIdentifierRequest(
		"Primary colors", "A description", IdentifierTypeRegex,
		IdentifierConfig{Regex: "x"}, []string{"Discovered.Primary color"},
	)

	tests := map[string]struct {
		call       func(*Client) error
		wantMethod string
		wantPath   string
	}{
		"read": {
			call: func(c *Client) error {
				_, err := c.GetIdentifier(t.Context(), "abc-123")

				return err
			},
			wantMethod: http.MethodGet,
			wantPath:   "/sdd/identifier/abc-123",
		},
		"update": {
			call: func(c *Client) error {
				_, err := c.UpdateIdentifier(t.Context(), "abc-123", validRequest)

				return err
			},
			wantMethod: http.MethodPut,
			wantPath:   "/sdd/identifier/abc-123",
		},
		"delete": {
			call: func(c *Client) error {
				return c.DeleteIdentifier(t.Context(), "abc-123")
			},
			wantMethod: http.MethodDelete,
			wantPath:   "/sdd/identifier/abc-123",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var gotMethod, gotPath string

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod, gotPath = r.Method, r.URL.EscapedPath()
				writeTestJSON(w, http.StatusOK, identifierResponse)
			}))
			t.Cleanup(server.Close)

			client := newTestClient(t, server.URL)

			if err := test.call(client); err != nil {
				t.Fatalf("the request returned an unexpected error: %s", err)
			}

			if gotMethod != test.wantMethod || gotPath != test.wantPath {
				t.Errorf("request = %s %s, want %s %s", gotMethod, gotPath, test.wantMethod, test.wantPath)
			}
		})
	}
}

func TestSaveIdentifierRequestValidation(t *testing.T) {
	t.Parallel()

	tests := map[string]SaveIdentifierRequest{
		"no name":                                NewSaveIdentifierRequest("", "d", IdentifierTypeRegex, IdentifierConfig{Regex: "x"}, []string{"t"}),
		"no description":                         NewSaveIdentifierRequest("n", "", IdentifierTypeRegex, IdentifierConfig{Regex: "x"}, []string{"t"}),
		"no tags":                                NewSaveIdentifierRequest("n", "d", IdentifierTypeRegex, IdentifierConfig{Regex: "x"}, nil),
		"a regex type with no regex":             NewSaveIdentifierRequest("n", "d", IdentifierTypeRegex, IdentifierConfig{}, []string{"t"}),
		"a column name regex type with no regex": NewSaveIdentifierRequest("n", "d", IdentifierTypeColumnNameRegex, IdentifierConfig{}, []string{"t"}),
		"a dictionary type with no values":       NewSaveIdentifierRequest("n", "d", IdentifierTypeDictionary, IdentifierConfig{}, []string{"t"}),
		"an unknown type":                        NewSaveIdentifierRequest("n", "d", "fuzzy", IdentifierConfig{Regex: "x"}, []string{"t"}),
	}

	// No request should reach the API when it is invalid.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request %s %s", r.Method, r.URL.EscapedPath())
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	client := newTestClient(t, server.URL)

	for name, request := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, err := client.CreateIdentifier(t.Context(), request); err == nil {
				t.Error("the request was accepted, want a validation error")
			}
		})
	}
}

func TestTagRequestsUseTheDocumentedPaths(t *testing.T) {
	t.Parallel()

	t.Run("create", func(t *testing.T) {
		t.Parallel()

		var gotBody map[string]any
		var gotMethod, gotPath string

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotMethod, gotPath = r.Method, r.URL.EscapedPath()
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &gotBody)
			writeTestJSON(w, http.StatusOK, `[{"id":1,"name":"Discovered"},{"id":2,"name":"Discovered.Primary color"}]`)
		}))
		t.Cleanup(server.Close)

		client := newTestClient(t, server.URL)

		tags, err := client.CreateTags(t.Context(), []string{"Discovered.Primary color"}, "")
		if err != nil {
			t.Fatalf("CreateTags returned an unexpected error: %s", err)
		}

		// Immuta returns the parent tag it created as well.
		if len(tags) != 2 {
			t.Errorf("tags = %v, want the tag and its parent", tags)
		}

		if gotMethod != http.MethodPost || gotPath != "/tag" {
			t.Errorf("request = %s %s, want POST /tag", gotMethod, gotPath)
		}

		want := map[string]any{"tags": []any{map[string]any{"name": "Discovered.Primary color"}}}
		if !reflect.DeepEqual(gotBody, want) {
			t.Errorf("request body = %v, want %v", gotBody, want)
		}
	})

	t.Run("create under an existing root sends rootTag without deleting the hierarchy", func(t *testing.T) {
		t.Parallel()

		var gotBody map[string]any

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &gotBody)
			writeTestJSON(w, http.StatusOK, `[{"id":2,"name":"Discovered.Entity.Primary color"}]`)
		}))
		t.Cleanup(server.Close)

		client := newTestClient(t, server.URL)

		if _, err := client.CreateTags(t.Context(), []string{"Discovered.Entity.Primary color"}, "Discovered"); err != nil {
			t.Fatalf("CreateTags returned an unexpected error: %s", err)
		}

		// The same shape the Immuta UI sends. deleteHierarchy must be present
		// and false: true would delete every existing child of the root.
		want := map[string]any{
			"rootTag": map[string]any{"name": "Discovered", "deleteHierarchy": false},
			"tags":    []any{map[string]any{"name": "Discovered.Entity.Primary color"}},
		}
		if !reflect.DeepEqual(gotBody, want) {
			t.Errorf("request body = %v, want %v", gotBody, want)
		}
	})

	t.Run("create under a root rejects a tag outside it", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.EscapedPath())
		}))
		t.Cleanup(server.Close)

		client := newTestClient(t, server.URL)

		if _, err := client.CreateTags(t.Context(), []string{"Other.Primary color"}, "Discovered"); err == nil {
			t.Fatal("CreateTags accepted a tag that is not under the root tag")
		}
	})

	t.Run("delete sends deleteHierarchy", func(t *testing.T) {
		t.Parallel()

		var gotMethod, gotPath, gotQuery string

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotMethod, gotPath, gotQuery = r.Method, r.URL.EscapedPath(), r.URL.RawQuery
			w.WriteHeader(http.StatusNoContent)
		}))
		t.Cleanup(server.Close)

		client := newTestClient(t, server.URL)

		if err := client.DeleteTag(t.Context(), "Discovered.Primary color", true); err != nil {
			t.Fatalf("DeleteTag returned an unexpected error: %s", err)
		}

		if gotMethod != http.MethodDelete || gotPath != "/tag/Discovered.Primary%20color" {
			t.Errorf("request = %s %s, want DELETE /tag/Discovered.Primary%%20color", gotMethod, gotPath)
		}
		if gotQuery != "deleteHierarchy=true" {
			t.Errorf("query = %q, want deleteHierarchy=true", gotQuery)
		}
	})
}

func TestFindTagMatchesExactlyAndSkipsDeleted(t *testing.T) {
	t.Parallel()

	var gotQuery string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		// The documented search is a wildcard match, so it also returns a
		// longer name and a deleted tag.
		writeTestJSON(w, http.StatusOK, `[
			{"id":1,"name":"Discovered.Primary color scheme"},
			{"id":2,"name":"Discovered.Primary color","deleted":true},
			{"id":3,"name":"Discovered.Primary color"}
		]`)
	}))
	t.Cleanup(server.Close)

	client := newTestClient(t, server.URL)

	tag, err := client.FindTag(t.Context(), "Discovered.Primary color")
	if err != nil {
		t.Fatalf("FindTag returned an unexpected error: %s", err)
	}

	if tag.ID != 3 {
		t.Errorf("tag id = %d, want 3, the exact match that is not deleted", tag.ID)
	}

	if gotQuery != "searchText=Discovered.Primary+color" {
		t.Errorf("query = %q, want the tag name as searchText", gotQuery)
	}
}

func TestFindTagReturnsNotFound(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeTestJSON(w, http.StatusOK, `[{"id":1,"name":"Something else"}]`)
	}))
	t.Cleanup(server.Close)

	client := newTestClient(t, server.URL)

	if _, err := client.FindTag(t.Context(), "Discovered.Primary color"); !IsNotFound(err) {
		t.Fatalf("FindTag error = %v, want a not-found error", err)
	}
}

func TestRootTagName(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"Discovered.Primary color":        "Discovered",
		"Discovered.Entity.Primary color": "Discovered",
		"Retired":                         "",
	}

	for name, want := range tests {
		if got := RootTagName(name); got != want {
			t.Errorf("RootTagName(%q) = %q, want %q", name, got, want)
		}
	}
}

// bulkCopyServer answers the bulk copy with response and serves the copy with
// GET /sdd/identifier/copy-1, recording the requests it receives.
func bulkCopyServer(t *testing.T, response string) (*httptest.Server, *[]string, *[]any) {
	t.Helper()

	var requests []string
	var copyBody []any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.EscapedPath())

		switch {
		case r.Method == http.MethodPost && r.URL.EscapedPath() == "/sdd/identifier/bulk-copy/domain-1":
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &copyBody)
			writeTestJSON(w, http.StatusOK, response)
		case r.Method == http.MethodGet && r.URL.EscapedPath() == "/sdd/identifier/copy-1":
			writeTestJSON(w, http.StatusOK, `{"id":"copy-1","name":"Credit card","type":"regex","config":{"regex":"x"},"tags":[],"domain":{"id":"domain-1"}}`)
		default:
			writeTestJSON(w, http.StatusNotFound, `{"message":"not found"}`)
		}
	}))
	t.Cleanup(server.Close)

	return server, &requests, &copyBody
}

func TestCopyIdentifiersToDomainReadsTheObservedResponse(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"created entries as ids":     `{"count":1,"createdIdentifiers":["copy-1"],"errors":[]}`,
		"created entries as objects": `{"count":1,"createdIdentifiers":[{"id":"copy-1","name":"Credit card"}],"errors":[]}`,
		"errors omitted":             `{"count":1,"createdIdentifiers":["copy-1"]}`,
	}

	for name, response := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			server, requests, copyBody := bulkCopyServer(t, response)

			copies, err := newTestClient(t, server.URL).CopyIdentifiersToDomain(t.Context(), "domain-1", []string{"source-1"})
			if err != nil {
				t.Fatalf("CopyIdentifiersToDomain returned an unexpected error: %s", err)
			}

			if len(copies) != 1 || copies[0].ID != "copy-1" || copies[0].Name != "Credit card" {
				t.Errorf("copies = %v, want the copy read back in full", copies)
			}

			// The request body is the bare array of ids the Immuta UI sends.
			if !reflect.DeepEqual(*copyBody, []any{"source-1"}) {
				t.Errorf("request body = %v, want [source-1]", *copyBody)
			}

			want := []string{"POST /sdd/identifier/bulk-copy/domain-1", "GET /sdd/identifier/copy-1"}
			if !reflect.DeepEqual(*requests, want) {
				t.Errorf("requests = %v, want %v", *requests, want)
			}
		})
	}
}

func TestCopyIdentifiersToDomainSurfacesReportedErrors(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		errors string
		want   string
	}{
		"an object with a message": {
			errors: `[{"identifierId":"source-1","message":"an identifier named Credit card already exists"}]`,
			want:   "an identifier named Credit card already exists",
		},
		"a plain string": {
			errors: `["name already taken"]`,
			want:   "name already taken",
		},
		"an object without a message": {
			errors: `[{"code":409,"identifierId":"source-1"}]`,
			want:   "an object with the keys [code, identifierId]",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			server, requests, _ := bulkCopyServer(t, `{"count":0,"createdIdentifiers":[],"errors":`+test.errors+`}`)

			_, err := newTestClient(t, server.URL).CopyIdentifiersToDomain(t.Context(), "domain-1", []string{"source-1"})
			if err == nil {
				t.Fatal("CopyIdentifiersToDomain succeeded although Immuta reported an error")
			}

			if !strings.Contains(err.Error(), test.want) {
				t.Errorf("error %q does not contain %q", err, test.want)
			}

			if len(*requests) != 1 {
				t.Errorf("requests = %v, want only the bulk copy", *requests)
			}
		})
	}
}

func TestCopyIdentifiersToDomainRejectsUnexpectedResponses(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		response string
		want     string
	}{
		"no createdIdentifiers": {
			response: `{"workflowId":"secret-looking-value"}`,
			want:     "an object with the keys [workflowId]",
		},
		"a bare array": {
			response: `["copy-1"]`,
			want:     "unexpected bulk copy response",
		},
		"fewer copies than requested": {
			response: `{"count":0,"createdIdentifiers":[],"errors":[]}`,
			want:     "copied 1 identifiers but Immuta reported 0 created",
		},
		"an entry without an id": {
			response: `{"count":1,"createdIdentifiers":[{"name":"secret-looking-value"}],"errors":[]}`,
			want:     "unexpected entry in createdIdentifiers: an object with the keys [name]",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			server, _, _ := bulkCopyServer(t, test.response)

			_, err := newTestClient(t, server.URL).CopyIdentifiersToDomain(t.Context(), "domain-1", []string{"source-1"})
			if err == nil {
				t.Fatal("CopyIdentifiersToDomain accepted an unexpected response")
			}

			if !strings.Contains(err.Error(), test.want) {
				t.Errorf("error %q does not contain %q", err, test.want)
			}

			// The shape is described, but no value from the body leaks.
			if strings.Contains(err.Error(), "secret-looking-value") {
				t.Errorf("error %q leaks a value from the response body", err)
			}
		})
	}
}
