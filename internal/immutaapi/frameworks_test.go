package immutaapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

// frameworkResponse is the documented POST /frameworks response.
const frameworkResponse = `{
	"id": "9a6bf3b1-823c-4b2e-aef9-570dac6793cc",
	"version": "4e823e0b-8e38-455d-b23c-1f03200d203a",
	"shortName": "HR Information",
	"name": "HR Information Framework",
	"description": "This framework finds internal employee information and tags it for HR.",
	"createdBy": 2,
	"createdAt": "2023-10-19T16:14:39.109Z",
	"tags": [{"name": "HR Framework.Internal Employee Data", "source": "curated", "sensitivities": []}],
	"rules": [{
		"name": "HR Rule 1",
		"classificationTag": {"name": "HR Framework.Internal Employee Data", "source": "curated"},
		"columnTags": [],
		"neighborColumnTags": [{"name": "Employee Name", "source": "curated"}],
		"tableTags": []
	}],
	"active": true
}`

func testFrameworkRequest() SaveFrameworkRequest {
	return SaveFrameworkRequest{
		ShortName:   "HR Information",
		Name:        "HR Information Framework",
		Description: "This framework finds internal employee information and tags it for HR.",
		Tags: []FrameworkTag{
			{Name: "HR Framework.Internal Employee Data", Source: "curated"},
		},
		Rules: []FrameworkRule{{
			Name:               "HR Rule 1",
			ClassificationTag:  FrameworkTagRef{Name: "HR Framework.Internal Employee Data", Source: "curated"},
			NeighborColumnTags: []FrameworkTagRef{{Name: "Employee Name", Source: "curated"}},
		}},
		Active: true,
	}
}

// documentedFrameworkBody is the body of the documented POST /frameworks
// example, decoded generically.
func documentedFrameworkBody(active bool) map[string]any {
	return map[string]any{
		"active":      active,
		"shortName":   "HR Information",
		"name":        "HR Information Framework",
		"description": "This framework finds internal employee information and tags it for HR.",
		"tags": []any{map[string]any{
			"name":          "HR Framework.Internal Employee Data",
			"source":        "curated",
			"sensitivities": []any{},
		}},
		"rules": []any{map[string]any{
			"name":              "HR Rule 1",
			"classificationTag": map[string]any{"name": "HR Framework.Internal Employee Data", "source": "curated"},
			"columnTags":        []any{},
			"neighborColumnTags": []any{
				map[string]any{"name": "Employee Name", "source": "curated"},
			},
			"tableTags": []any{},
		}},
	}
}

func TestCreateFrameworkSendsTheDocumentedBody(t *testing.T) {
	t.Parallel()

	var gotBody map[string]any
	var gotMethod, gotPath string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.EscapedPath()
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &gotBody); err != nil {
			t.Errorf("request body is not valid JSON: %s", err)
		}
		writeTestJSON(w, http.StatusOK, frameworkResponse)
	}))
	t.Cleanup(server.Close)

	framework, err := newTestClient(t, server.URL).CreateFramework(t.Context(), testFrameworkRequest())
	if err != nil {
		t.Fatalf("CreateFramework returned an unexpected error: %s", err)
	}

	if gotMethod != http.MethodPost || gotPath != "/frameworks" {
		t.Errorf("request = %s %s, want POST /frameworks", gotMethod, gotPath)
	}
	// Empty lists are sent as [], never null, matching the documented example.
	if want := documentedFrameworkBody(true); !reflect.DeepEqual(gotBody, want) {
		t.Errorf("request body = %v, want %v", gotBody, want)
	}

	if framework.ID != "9a6bf3b1-823c-4b2e-aef9-570dac6793cc" {
		t.Errorf("framework id = %q, want the id from the response", framework.ID)
	}
	if framework.Version != "4e823e0b-8e38-455d-b23c-1f03200d203a" {
		t.Errorf("version = %q, want the version from the response", framework.Version)
	}
	if framework.CreatedBy == nil || *framework.CreatedBy != 2 {
		t.Errorf("createdBy = %v, want 2", framework.CreatedBy)
	}
	if len(framework.Rules) != 1 || framework.Rules[0].NeighborColumnTags[0].Name != "Employee Name" {
		t.Errorf("rules = %+v, want the rule from the response", framework.Rules)
	}
	if !framework.Active {
		t.Error("active = false, want true")
	}
}

func TestCreateFrameworkSendsSensitivities(t *testing.T) {
	t.Parallel()

	var gotBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		writeTestJSON(w, http.StatusOK, frameworkResponse)
	}))
	t.Cleanup(server.Close)

	req := testFrameworkRequest()
	req.Tags[0].Sensitivities = []FrameworkSensitivity{{Dimension: SensitivityDimensionConfidentiality, Sensitivity: SensitivityHighlySensitive}}

	if _, err := newTestClient(t, server.URL).CreateFramework(t.Context(), req); err != nil {
		t.Fatalf("CreateFramework returned an unexpected error: %s", err)
	}

	tags := gotBody["tags"].([]any)
	want := []any{map[string]any{"dimension": "confidentiality", "sensitivity": float64(2)}}
	if got := tags[0].(map[string]any)["sensitivities"]; !reflect.DeepEqual(got, want) {
		t.Errorf("sensitivities = %v, want %v", got, want)
	}
}

func TestCreateFrameworkRejectsInvalidRequestsWithoutCallingTheAPI(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		writeTestJSON(w, http.StatusOK, frameworkResponse)
	}))
	t.Cleanup(server.Close)

	client := newTestClient(t, server.URL)

	tooManyRules := testFrameworkRequest()
	for i := range FrameworkMaxRules {
		rule := tooManyRules.Rules[0]
		rule.Name = fmt.Sprintf("Rule %d", i)
		tooManyRules.Rules = append(tooManyRules.Rules, rule)
	}

	cases := map[string]struct {
		mutate func(*SaveFrameworkRequest)
		want   string
	}{
		"missing short name":  {func(r *SaveFrameworkRequest) { r.ShortName = " " }, "short name is required"},
		"missing name":        {func(r *SaveFrameworkRequest) { r.Name = "" }, "framework name is required"},
		"missing description": {func(r *SaveFrameworkRequest) { r.Description = "" }, "description is required"},
		"too many rules":      {func(r *SaveFrameworkRequest) { *r = tooManyRules }, "at most 50 rules, got 51"},
		"tag without source":  {func(r *SaveFrameworkRequest) { r.Tags[0].Source = "" }, "a source is required"},
		"rule without name":   {func(r *SaveFrameworkRequest) { r.Rules[0].Name = "" }, "rule name is required"},
		"classification tag without name": {
			func(r *SaveFrameworkRequest) { r.Rules[0].ClassificationTag.Name = "" },
			"classification tag",
		},
		"criteria tag without source": {
			func(r *SaveFrameworkRequest) { r.Rules[0].NeighborColumnTags[0].Source = "" },
			"criteria tag",
		},
		"sensitivity without dimension": {
			func(r *SaveFrameworkRequest) { r.Tags[0].Sensitivities = []FrameworkSensitivity{{Sensitivity: 1}} },
			"sensitivity dimension is required",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			req := testFrameworkRequest()
			tc.mutate(&req)

			_, err := client.CreateFramework(t.Context(), req)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %v, want it to mention %q", err, tc.want)
			}
		})
	}

	if got := calls.Load(); got != 0 {
		t.Errorf("the API was called %d times, want 0", got)
	}
}

func TestFrameworkAllowsExactlyTheRuleLimit(t *testing.T) {
	t.Parallel()

	req := testFrameworkRequest()
	for i := 1; i < FrameworkMaxRules; i++ {
		rule := req.Rules[0]
		rule.Name = fmt.Sprintf("Rule %d", i)
		req.Rules = append(req.Rules, rule)
	}

	if err := req.validate(); err != nil {
		t.Errorf("a framework with %d rules should be valid: %s", FrameworkMaxRules, err)
	}
}

func TestGetFrameworkEscapesTheIDAndAcceptsANumericID(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.EscapedPath()
		writeTestJSON(w, http.StatusOK, strings.Replace(frameworkResponse, `"9a6bf3b1-823c-4b2e-aef9-570dac6793cc"`, `123456`, 1))
	}))
	t.Cleanup(server.Close)

	framework, err := newTestClient(t, server.URL).GetFramework(t.Context(), "a/b")
	if err != nil {
		t.Fatalf("GetFramework returned an unexpected error: %s", err)
	}

	if gotMethod != http.MethodGet || gotPath != "/frameworks/a%2Fb" {
		t.Errorf("request = %s %s, want GET /frameworks/a%%2Fb", gotMethod, gotPath)
	}
	if framework.ID != "123456" {
		t.Errorf("framework id = %q, want 123456", framework.ID)
	}
}

func TestGetFrameworkReportsNotFound(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeTestJSON(w, http.StatusNotFound, `{"message": "framework not found"}`)
	}))
	t.Cleanup(server.Close)

	_, err := newTestClient(t, server.URL).GetFramework(t.Context(), "missing")
	if !IsNotFound(err) {
		t.Errorf("error = %v, want a not-found error", err)
	}
}

func TestFrameworkMethodsRequireAnID(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, "https://example.invalid")

	if _, err := client.GetFramework(t.Context(), " "); err == nil {
		t.Error("GetFramework accepted an empty id")
	}
	if _, err := client.UpdateFramework(t.Context(), "", testFrameworkRequest()); err == nil {
		t.Error("UpdateFramework accepted an empty id")
	}
	if err := client.DeleteFramework(t.Context(), ""); err == nil {
		t.Error("DeleteFramework accepted an empty id")
	}
}

func TestUpdateFrameworkSendsTheCompleteFramework(t *testing.T) {
	t.Parallel()

	var gotBody map[string]any
	var gotMethod, gotPath string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.EscapedPath()
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		writeTestJSON(w, http.StatusOK, frameworkResponse)
	}))
	t.Cleanup(server.Close)

	req := testFrameworkRequest()
	req.Active = false

	if _, err := newTestClient(t, server.URL).UpdateFramework(t.Context(), "framework-1", req); err != nil {
		t.Fatalf("UpdateFramework returned an unexpected error: %s", err)
	}

	if gotMethod != http.MethodPut || gotPath != "/frameworks/framework-1" {
		t.Errorf("request = %s %s, want PUT /frameworks/framework-1", gotMethod, gotPath)
	}
	// active=false must be sent explicitly, not omitted.
	if want := documentedFrameworkBody(false); !reflect.DeepEqual(gotBody, want) {
		t.Errorf("request body = %v, want %v", gotBody, want)
	}
}

func TestUpdateFrameworkReadsBackAfterAnEmptyResponse(t *testing.T) {
	t.Parallel()

	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.EscapedPath())
		if r.Method == http.MethodPut {
			w.WriteHeader(http.StatusOK)

			return
		}
		writeTestJSON(w, http.StatusOK, frameworkResponse)
	}))
	t.Cleanup(server.Close)

	framework, err := newTestClient(t, server.URL).UpdateFramework(t.Context(), "framework-1", testFrameworkRequest())
	if err != nil {
		t.Fatalf("UpdateFramework returned an unexpected error: %s", err)
	}
	if framework == nil || framework.ID == "" {
		t.Fatal("UpdateFramework should return the framework read back after the update")
	}

	want := []string{"PUT /frameworks/framework-1", "GET /frameworks/framework-1"}
	if !reflect.DeepEqual(requests, want) {
		t.Errorf("requests = %v, want %v", requests, want)
	}
}

func TestDeleteFrameworkSendsDelete(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.EscapedPath()
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	if err := newTestClient(t, server.URL).DeleteFramework(t.Context(), "framework-1"); err != nil {
		t.Fatalf("DeleteFramework returned an unexpected error: %s", err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/frameworks/framework-1" {
		t.Errorf("request = %s %s, want DELETE /frameworks/framework-1", gotMethod, gotPath)
	}
}

func TestDecodeFrameworkRequiresAnID(t *testing.T) {
	t.Parallel()

	if _, err := decodeFramework([]byte(`{"name": "No id"}`)); err == nil {
		t.Error("decodeFramework accepted a response without an id")
	}
	if _, err := decodeFramework([]byte(`{"id": true}`)); err == nil {
		t.Error("decodeFramework accepted a boolean id")
	}
}

// TestCreateFrameworkAcceptsCreatedByAsAnObject reproduces a real tenant,
// which returns createdBy as an object although the reference documents an
// integer. Failing to decode it after a successful create would leave the
// framework in Immuta but not in Terraform state.
func TestCreateFrameworkAcceptsCreatedByAsAnObject(t *testing.T) {
	t.Parallel()

	response := strings.Replace(frameworkResponse, `"createdBy": 2,`,
		`"createdBy": {"id": 2, "name": "Stu Johnstone", "email": "stu@example.com"},`, 1)
	if response == frameworkResponse {
		t.Fatal("the test response was not changed")
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeTestJSON(w, http.StatusOK, response)
	}))
	t.Cleanup(server.Close)

	framework, err := newTestClient(t, server.URL).CreateFramework(t.Context(), testFrameworkRequest())
	if err != nil {
		t.Fatalf("CreateFramework returned an unexpected error: %s", err)
	}
	if framework.ID != "9a6bf3b1-823c-4b2e-aef9-570dac6793cc" {
		t.Errorf("framework id = %q, want the id from the response", framework.ID)
	}
	if framework.CreatedBy == nil || *framework.CreatedBy != 2 {
		t.Errorf("createdBy = %v, want 2 from the createdBy object", framework.CreatedBy)
	}
}

func TestFrameworkCreatedByShapes(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		createdBy string
		want      *int64
	}{
		"documented integer":    {`2`, ptr(int64(2))},
		"numeric string":        {`"2"`, ptr(int64(2))},
		"object with id":        {`{"id": 2, "name": "Stu"}`, ptr(int64(2))},
		"object with string id": {`{"id": "2"}`, ptr(int64(2))},
		// Anything not understood is ignored, never an error.
		"object without id":        {`{"name": "Stu"}`, nil},
		"object with non-numeric":  {`{"id": "a1b2"}`, nil},
		"null":                     {`null`, nil},
		"non-numeric string":       {`"stu@example.com"`, nil},
		"unexpected type":          {`[1]`, nil},
		"object with nested shape": {`{"id": {"value": 2}}`, nil},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			framework, err := decodeFramework([]byte(`{"id": "f1", "createdBy": ` + tc.createdBy + `}`))
			if err != nil {
				t.Fatalf("decodeFramework returned an unexpected error: %s", err)
			}

			switch {
			case tc.want == nil && framework.CreatedBy != nil:
				t.Errorf("createdBy = %d, want nil", *framework.CreatedBy)
			case tc.want != nil && (framework.CreatedBy == nil || *framework.CreatedBy != *tc.want):
				t.Errorf("createdBy = %v, want %d", framework.CreatedBy, *tc.want)
			}
		})
	}

	framework, err := decodeFramework([]byte(`{"id": "f1"}`))
	if err != nil || framework.CreatedBy != nil {
		t.Errorf("a missing createdBy should decode to nil without error, got %v, %v", framework, err)
	}
}

func ptr[T any](value T) *T { return &value }
