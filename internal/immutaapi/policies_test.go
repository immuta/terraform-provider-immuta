package immutaapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// hashingPolicy is the documented simple masking policy.
func hashingPolicy() map[string]any {
	return map[string]any{
		"name":      "Hashing",
		"policyKey": "data mask hashing",
		"type":      "data",
		"actions": []any{map[string]any{
			"rules": []any{map[string]any{
				"type": "Masking",
				"config": map[string]any{
					"fields":        []any{map[string]any{"type": "noTags"}},
					"maskingConfig": map[string]any{"type": "Hash"},
				},
			}},
		}},
		"circumstances": []any{map[string]any{"type": "noTags"}},
	}
}

func TestSaveGlobalPolicyPostsTheDocument(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath string
	var gotBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.EscapedPath()
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		writeTestJSON(w, http.StatusOK, `{"id": 7}`)
	}))
	t.Cleanup(server.Close)

	body, err := newTestClient(t, server.URL).SaveGlobalPolicy(t.Context(), hashingPolicy())
	if err != nil {
		t.Fatalf("SaveGlobalPolicy returned an unexpected error: %s", err)
	}

	if gotMethod != http.MethodPost || gotPath != "/api/v2/policy" {
		t.Errorf("request = %s %s, want POST /api/v2/policy", gotMethod, gotPath)
	}
	if !reflect.DeepEqual(gotBody, hashingPolicy()) {
		t.Errorf("request body = %v, want the document unchanged", gotBody)
	}
	if string(body) != `{"id": 7}` {
		t.Errorf("response = %s, want the raw response for id resolution", body)
	}
}

func TestSaveGlobalPolicyRequiresAPolicyKey(t *testing.T) {
	t.Parallel()

	document := hashingPolicy()
	delete(document, "policyKey")

	if _, err := newTestClient(t, "https://example.invalid").SaveGlobalPolicy(t.Context(), document); err == nil {
		t.Error("SaveGlobalPolicy accepted a document without a policyKey")
	}
}

func TestGetGlobalPolicy(t *testing.T) {
	t.Parallel()

	responses := map[string]struct {
		status int
		body   string
	}{
		// The documented response, trimmed.
		"/policy/global/1": {http.StatusOK, `{"id": 1, "policyKey": "New Column Added", "name": "New Column Added",
			"type": "data", "template": false, "deleted": false, "actions": [{"type": "masking"}]}`},
		"/policy/global/2": {http.StatusOK, `{"id": 2, "policyKey": "gone", "deleted": true}`},
		"/policy/global/3": {http.StatusNotFound, `{"message": "not found"}`},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := responses[r.URL.EscapedPath()]
		writeTestJSON(w, response.status, response.body)
	}))
	t.Cleanup(server.Close)

	client := newTestClient(t, server.URL)

	policy, err := client.GetGlobalPolicy(t.Context(), "1")
	if err != nil {
		t.Fatalf("GetGlobalPolicy returned an unexpected error: %s", err)
	}
	if policy.Fingerprint == "" {
		t.Error("policy.Fingerprint is empty, want a hash of the policy content")
	}
	got := *policy
	got.Fingerprint = "" // Covered by TestGlobalPolicyFingerprint.
	want := GlobalPolicy{ID: "1", PolicyKey: "New Column Added", Name: "New Column Added", Type: "data"}
	if got != want {
		t.Errorf("policy = %+v, want %+v", got, want)
	}

	if _, err := client.GetGlobalPolicy(t.Context(), "2"); !IsNotFound(err) {
		t.Errorf("a policy flagged as deleted should be not found, got %v", err)
	}
	if _, err := client.GetGlobalPolicy(t.Context(), "3"); !IsNotFound(err) {
		t.Errorf("a 404 should be not found, got %v", err)
	}
	if _, err := client.GetGlobalPolicy(t.Context(), ""); err == nil {
		t.Error("GetGlobalPolicy accepted an empty id")
	}
}

func TestDeleteGlobalPolicy(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.EscapedPath()
		writeTestJSON(w, http.StatusOK, `{"id": 6}`)
	}))
	t.Cleanup(server.Close)

	if err := newTestClient(t, server.URL).DeleteGlobalPolicy(t.Context(), "6"); err != nil {
		t.Fatalf("DeleteGlobalPolicy returned an unexpected error: %s", err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/policy/global/6" {
		t.Errorf("request = %s %s, want DELETE /policy/global/6", gotMethod, gotPath)
	}
}

// policyLookupServer serves GET /policy/global and GET /policy/global/{id}
// for a fixed set of policies, and records the requests.
type policyLookupServer struct {
	mu       sync.Mutex
	requests []string
}

func newPolicyLookupServer(t *testing.T, listBody string, policies map[string]string) (*httptest.Server, *policyLookupServer) {
	t.Helper()

	recorder := &policyLookupServer{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder.mu.Lock()
		recorder.requests = append(recorder.requests, r.Method+" "+r.URL.EscapedPath()+"?"+r.URL.RawQuery)
		recorder.mu.Unlock()

		if r.URL.EscapedPath() == "/policy/global" {
			writeTestJSON(w, http.StatusOK, listBody)

			return
		}

		id := strings.TrimPrefix(r.URL.EscapedPath(), "/policy/global/")
		key, ok := policies[id]
		if !ok {
			writeTestJSON(w, http.StatusNotFound, `{"message": "not found"}`)

			return
		}
		writeTestJSON(w, http.StatusOK, fmt.Sprintf(`{"id": %s, "policyKey": %q, "name": "Hashing", "type": "data"}`, id, key))
	}))
	t.Cleanup(server.Close)

	return server, recorder
}

func TestResolveGlobalPolicyIDUsesTheSaveResponse(t *testing.T) {
	t.Parallel()

	server, recorder := newPolicyLookupServer(t, `[]`, map[string]string{"7": "data mask hashing"})

	id, err := newTestClient(t, server.URL).ResolveGlobalPolicyID(t.Context(), []byte(`{"id": 7, "name": "Hashing"}`),
		"data mask hashing", "Hashing", "data")
	if err != nil || id != "7" {
		t.Fatalf("id = %q, err = %v, want 7", id, err)
	}

	// The id is confirmed, and no search is needed.
	if want := []string{"GET /policy/global/7?"}; !reflect.DeepEqual(recorder.requests, want) {
		t.Errorf("requests = %v, want %v", recorder.requests, want)
	}
}

func TestResolveGlobalPolicyIDSearchesByNameAndConfirmsThePolicyKey(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		saveResponse string
		listBody     string
	}{
		"empty save response":         {``, `[{"name": "Hashing", "id": 3, "type": "data"}, {"name": "Hashing v2", "id": 9, "type": "data"}]`},
		"save response without an id": {`{"success": true}`, `[{"id": 3}, {"id": 9}]`},
		// An id belonging to another policy must not be trusted.
		"save response id with another key": {`{"id": 3}`, `[{"id": 3}, {"id": 9}]`},
		"list wrapped in hits":              {`[]`, `{"count": 2, "hits": [{"id": 3}, {"id": 9}]}`},
		"policy nested in the response":     {`{"policy": {"id": 9}}`, `[]`},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Policy 3 has a similar name but another policyKey.
			server, recorder := newPolicyLookupServer(t, tc.listBody, map[string]string{
				"3": "some other policy",
				"9": "data mask hashing",
			})

			id, err := newTestClient(t, server.URL).ResolveGlobalPolicyID(t.Context(), []byte(tc.saveResponse),
				"data mask hashing", "Hashing", "data")
			if err != nil || id != "9" {
				t.Fatalf("id = %q, err = %v, want 9; requests %v", id, err, recorder.requests)
			}

			for _, request := range recorder.requests {
				if strings.HasPrefix(request, "GET /policy/global?") &&
					(!strings.Contains(request, "searchText=Hashing") || !strings.Contains(request, "type=data")) {
					t.Errorf("search %q should filter by name and type", request)
				}
			}
		})
	}
}

func TestResolveGlobalPolicyIDReportsAMissingPolicy(t *testing.T) {
	t.Parallel()

	server, _ := newPolicyLookupServer(t, `[{"id": 3}]`, map[string]string{"3": "some other policy"})

	_, err := newTestClient(t, server.URL).ResolveGlobalPolicyID(t.Context(), nil, "data mask hashing", "Hashing", "data")
	if err == nil || !strings.Contains(err.Error(), `"data mask hashing"`) {
		t.Errorf("error = %v, want it to name the policyKey", err)
	}
}

func TestGlobalPolicyFingerprint(t *testing.T) {
	t.Parallel()

	fingerprint := func(t *testing.T, body string) string {
		t.Helper()

		var policy GlobalPolicy
		if err := json.Unmarshal([]byte(body), &policy); err != nil {
			t.Fatalf("decoding %s: %s", body, err)
		}

		return policy.Fingerprint
	}

	base := `{"id": 1, "name": "Mask", "staged": false,
		"actions": [{"type": "masking", "rules": [{"type": "masking", "config": {"fields": [{"name": "PII", "source": "curated"}]}}]}],
		"circumstances": [{"type": "columnTags", "columnTag": "PII"}],
		"certification": {"text": "certify", "tags": ["PII"]}}`

	same := map[string]string{
		"key order and whitespace": `{"certification": {"tags": ["PII"], "text": "certify"},
			"circumstances": [{"columnTag": "PII", "type": "columnTags"}], "staged": false, "name": "Mask", "id": 1,
			"actions": [{"rules": [{"config": {"fields": [{"source": "curated", "name": "PII"}]}, "type": "masking"}], "type": "masking"}]}`,
		"fields outside the content": `{"id": 9, "name": "Renamed", "policyKey": "other", "createdAt": "2026-10-28", "staged": false,
			"actions": [{"type": "masking", "rules": [{"type": "masking", "config": {"fields": [{"name": "PII", "source": "curated"}]}}]}],
			"circumstances": [{"type": "columnTags", "columnTag": "PII"}],
			"certification": {"text": "certify", "tags": ["PII"]}}`,
	}
	for name, body := range same {
		if got, want := fingerprint(t, body), fingerprint(t, base); got != want {
			t.Errorf("%s: fingerprint changed, want it unchanged", name)
		}
	}

	changed := map[string]string{
		"actions":       strings.Replace(base, `"name": "PII"`, `"name": "PHI"`, 1),
		"circumstances": strings.Replace(base, `"columnTag": "PII"`, `"columnTag": "PHI"`, 1),
		"staged":        strings.Replace(base, `"staged": false`, `"staged": true`, 1),
		"certification": strings.Replace(base, `"text": "certify"`, `"text": "changed"`, 1),
	}
	for name, body := range changed {
		if body == base {
			t.Fatalf("%s: test body was not changed", name)
		}
		if fingerprint(t, body) == fingerprint(t, base) {
			t.Errorf("a change to %s kept the fingerprint, want it changed", name)
		}
	}
}
