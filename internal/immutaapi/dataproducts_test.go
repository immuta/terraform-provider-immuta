package immutaapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

const testRequestAppToken = "test-request-app-pat"

const dataProductResponse = `{
	"id": "0b6c5f5e-6a43-4c0e-9b8f-6f2a4c1d2e3f",
	"name": "Customer 360",
	"description": "Everything about customers",
	"domains": ["d1"],
	"suspended": false,
	"createdBy": "5b0c1f0e-1111-2222-3333-444455556666",
	"createdAt": "2026-10-07T00:00:00Z",
	"updatedAt": "2026-10-07T00:00:00Z",
	"metadata": {"subjectMatterExpert": {"id": "99aeadb5-352e-4872-9983-e628d399c36e", "name": "Expert"}},
	"stats": {"dataSourceCount": 1}
}`

// recordingServer records every request and answers with a fixed response.
func recordingServer(t *testing.T, status int, response string) (*httptest.Server, *[]recorded) {
	t.Helper()

	var requests []recorded
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body any
		_ = json.Unmarshal(raw, &body)
		requests = append(requests, recorded{r.Method, r.URL.EscapedPath(), r.Header.Get("Authorization"), body})
		writeTestJSON(w, status, response)
	}))
	t.Cleanup(server.Close)

	return server, &requests
}

type recorded struct {
	method, path, authorization string
	body                        any
}

// newTestRequestAppClient returns a client whose Request app API is served
// by host under /marketplace, as the documented regional URLs are.
func newTestRequestAppClient(t *testing.T, host string) *Client {
	t.Helper()

	return newTestClient(t, "https://tenant.invalid", WithRequestApp(host+"/marketplace", testRequestAppToken))
}

func TestCreateDataProductUsesTheRequestAppURLAndToken(t *testing.T) {
	t.Parallel()

	server, requests := recordingServer(t, http.StatusOK, dataProductResponse)
	client := newTestRequestAppClient(t, server.URL)

	product, err := client.CreateDataProduct(t.Context(), CreateDataProductRequest{
		Name:        "Customer 360",
		Metadata:    DataProductMetadata{SubjectMatterExpert: DataProductSubjectMatterExpert{ID: "99aeadb5-352e-4872-9983-e628d399c36e"}},
		Domains:     []string{"d1"},
		DataSources: DataProductSources([]string{"7"}),
	})
	if err != nil {
		t.Fatalf("CreateDataProduct returned an unexpected error: %s", err)
	}

	got := (*requests)[0]
	if got.method != http.MethodPost || got.path != "/marketplace/api/data-product" {
		t.Errorf("request = %s %s, want POST /marketplace/api/data-product on the Request app URL", got.method, got.path)
	}
	if got.authorization != "Bearer "+testRequestAppToken {
		t.Errorf("authorization = %q, want the Request app token as a bearer token", got.authorization)
	}
	// No id or description: they are omitted, not sent empty.
	want := map[string]any{
		"name":        "Customer 360",
		"metadata":    map[string]any{"subjectMatterExpert": map[string]any{"id": "99aeadb5-352e-4872-9983-e628d399c36e"}},
		"domains":     []any{"d1"},
		"dataSources": []any{map[string]any{"sourceId": "7"}},
	}
	if !reflect.DeepEqual(got.body, want) {
		t.Errorf("request body = %v, want %v", got.body, want)
	}

	description := "Everything about customers"
	wantProduct := &DataProduct{
		ID:          "0b6c5f5e-6a43-4c0e-9b8f-6f2a4c1d2e3f",
		Name:        "Customer 360",
		Description: &description,
		Domains:     []string{"d1"},
		Metadata:    &DataProductMetadata{SubjectMatterExpert: DataProductSubjectMatterExpert{ID: "99aeadb5-352e-4872-9983-e628d399c36e"}},
	}
	if !reflect.DeepEqual(product, wantProduct) {
		t.Errorf("product = %+v, want %+v", product, wantProduct)
	}
	if got := product.SubjectMatterExpertID(); got != "99aeadb5-352e-4872-9983-e628d399c36e" {
		t.Errorf("subject matter expert = %q", got)
	}
}

func TestDataProductRequests(t *testing.T) {
	t.Parallel()

	server, requests := recordingServer(t, http.StatusOK, dataProductResponse)
	client := newTestRequestAppClient(t, server.URL)

	if _, err := client.GetDataProduct(t.Context(), "p/1"); err != nil {
		t.Fatal(err)
	}
	if err := client.UpdateDataProduct(t.Context(), "p1", UpdateDataProductRequest{
		Name:     "Renamed",
		Metadata: DataProductMetadata{SubjectMatterExpert: DataProductSubjectMatterExpert{ID: "u1"}},
		Domains:  []string{"d1"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := client.SetDataProductDataSources(t.Context(), "p1", []string{"7", "8"}); err != nil {
		t.Fatal(err)
	}
	if err := client.DeleteDataProduct(t.Context(), "p1"); err != nil {
		t.Fatal(err)
	}

	want := []recorded{
		{http.MethodGet, "/marketplace/api/data-product/p%2F1", "", nil},
		// Every managed field is sent; a null description clears it.
		{http.MethodPut, "/marketplace/api/data-product/p1", "", map[string]any{
			"name":        "Renamed",
			"description": nil,
			"metadata":    map[string]any{"subjectMatterExpert": map[string]any{"id": "u1"}},
			"domains":     []any{"d1"},
			"suspended":   false,
		}},
		{http.MethodPut, "/marketplace/api/data-product/p1/datasources", "", map[string]any{"dataSources": []any{
			map[string]any{"sourceId": "7"}, map[string]any{"sourceId": "8"},
		}}},
		{http.MethodDelete, "/marketplace/api/data-product/p1", "", nil},
	}
	for i := range want {
		got := (*requests)[i]
		got.authorization = ""
		if !reflect.DeepEqual(got, want[i]) {
			t.Errorf("request %d = %+v, want %+v", i, got, want[i])
		}
	}
}

func TestGetDataProductDataSources(t *testing.T) {
	t.Parallel()

	// Documented as strings; a number is accepted too.
	server, requests := recordingServer(t, http.StatusOK, `{"dataSources": [{"sourceId": "7", "name": "customers"}, {"sourceId": 8}]}`)

	sourceIDs, err := newTestRequestAppClient(t, server.URL).GetDataProductDataSources(t.Context(), "p1")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if !reflect.DeepEqual(sourceIDs, []string{"7", "8"}) {
		t.Errorf("source ids = %v, want [7 8]", sourceIDs)
	}
	if got := (*requests)[0].path; got != "/marketplace/api/data-product/p1/datasources/all" {
		t.Errorf("path = %s, want /marketplace/api/data-product/p1/datasources/all", got)
	}
}

func TestDataProductErrors(t *testing.T) {
	t.Parallel()

	server, _ := recordingServer(t, http.StatusNotFound, `{"message": "Data product not found"}`)
	client := newTestRequestAppClient(t, server.URL)

	if _, err := client.GetDataProduct(t.Context(), "missing"); !IsNotFound(err) {
		t.Errorf("error = %v, want not found", err)
	}
	if _, err := client.GetDataProduct(t.Context(), " "); err == nil {
		t.Error("GetDataProduct accepted an empty id")
	}
	if _, err := client.CreateDataProduct(t.Context(), CreateDataProductRequest{Domains: []string{"d1"}}); err == nil {
		t.Error("CreateDataProduct accepted a body without a name")
	}
	if _, err := decodeDataProduct([]byte(`{"name": "no id"}`)); err == nil {
		t.Error("decodeDataProduct accepted a response without an id")
	}
}

func TestDataProductRequestsNeedTheRequestApp(t *testing.T) {
	t.Parallel()

	server, requests := recordingServer(t, http.StatusOK, dataProductResponse)
	client := newTestClient(t, server.URL)

	if client.HasRequestApp() || client.RequestAppURL() != "" {
		t.Fatal("a client built without WithRequestApp reports a Request app")
	}
	if _, err := client.GetDataProduct(t.Context(), "p1"); !errors.Is(err, ErrRequestAppNotConfigured) {
		t.Errorf("error = %v, want ErrRequestAppNotConfigured", err)
	}
	if err := client.DeleteDataProduct(t.Context(), "p1"); !errors.Is(err, ErrRequestAppNotConfigured) {
		t.Errorf("error = %v, want ErrRequestAppNotConfigured", err)
	}
	// Nothing is sent to the tenant host with the Govern API key instead.
	if len(*requests) != 0 {
		t.Errorf("requests = %+v, want none", *requests)
	}
}

func TestRequestAppKeepsTheGovernAPIOnTheTenantHost(t *testing.T) {
	t.Parallel()

	tenant, tenantRequests := recordingServer(t, http.StatusOK, `{"id": "d1", "name": "Sales"}`)
	requestApp, requestAppRequests := recordingServer(t, http.StatusOK, dataProductResponse)
	client := newTestClient(t, tenant.URL, WithRequestApp(requestApp.URL+"/marketplace/", testRequestAppToken))

	if got, want := client.RequestAppURL(), requestApp.URL+"/marketplace"; got != want {
		t.Errorf("RequestAppURL = %q, want %q", got, want)
	}
	if _, err := client.GetDomain(t.Context(), "d1"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetDataProduct(t.Context(), "p1"); err != nil {
		t.Fatal(err)
	}

	if len(*tenantRequests) != 1 || (*tenantRequests)[0].authorization != testAPIKey {
		t.Errorf("tenant requests = %+v, want one with the Govern API key", *tenantRequests)
	}
	if len(*requestAppRequests) != 1 || (*requestAppRequests)[0].authorization != "Bearer "+testRequestAppToken {
		t.Errorf("Request app requests = %+v, want one with the bearer token", *requestAppRequests)
	}
}

func TestWithRequestAppNeedsBothURLAndToken(t *testing.T) {
	t.Parallel()

	for name, option := range map[string]Option{
		"no token":    WithRequestApp("https://na.api.immutacloud.com/marketplace", " "),
		"no url":      WithRequestApp("", testRequestAppToken),
		"invalid url": WithRequestApp("ftp://na.api.immutacloud.com/marketplace", testRequestAppToken),
	} {
		_, err := NewClient("https://tenant.invalid", testAPIKey, option)
		if err == nil {
			t.Errorf("%s: NewClient succeeded, want an error", name)
		} else if strings.Contains(err.Error(), testRequestAppToken) {
			t.Errorf("%s: the error leaked the token: %s", name, err)
		}
	}

	// Neither is fine: the Request app is simply not configured.
	if _, err := NewClient("https://tenant.invalid", testAPIKey, WithRequestApp("", "")); err != nil {
		t.Errorf("NewClient without a Request app returned %s", err)
	}
}

func TestRequestAppErrorsNeverLeakTheToken(t *testing.T) {
	t.Parallel()

	server, _ := recordingServer(t, http.StatusUnauthorized, `{"message": "token `+testRequestAppToken+` is not authenticated"}`)

	_, err := newTestRequestAppClient(t, server.URL).GetDataProduct(t.Context(), "p1")
	if err == nil {
		t.Fatal("GetDataProduct should fail on 401")
	}
	if strings.Contains(err.Error(), testRequestAppToken) || !strings.Contains(err.Error(), "<redacted>") {
		t.Errorf("error = %q, want the token redacted", err)
	}
}
