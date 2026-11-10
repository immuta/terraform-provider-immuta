package immutaapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

const dataObjectResponse = `{
	"id": "e6cb340a-4a44-42a5-bed3-0ffe1c491926",
	"name": "orders",
	"type": "TABLE",
	"connectionKey": "acme_dev_databricks",
	"objectPath": {
		"id": "c1", "name": "acme_dev", "type": "CATALOG",
		"child": {
			"id": "s1", "name": "sales", "type": "SCHEMA",
			"child": {"id": "e6cb340a-4a44-42a5-bed3-0ffe1c491926", "name": "orders", "type": "TABLE"}
		}
	},
	"settings": {
		"syncPolicy": {"value": true, "inheritedFrom": {"id": "c1", "type": "DATA_OBJECT"}},
		"owners": []
	}
}`

func TestGetDataObject(t *testing.T) {
	t.Parallel()

	server, requests := recordingServer(t, http.StatusOK, dataObjectResponse)

	object, err := newTestClient(t, server.URL).GetDataObject(t.Context(), "acme/dev", "e6cb340a-4a44-42a5-bed3-0ffe1c491926")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	if got := (*requests)[0]; got.method != http.MethodGet || got.path != "/api/connections/acme%2Fdev/dataObjects/e6cb340a-4a44-42a5-bed3-0ffe1c491926" {
		t.Errorf("request = %s %s", got.method, got.path)
	}
	enabled := true
	want := &DataObject{
		ID:            "e6cb340a-4a44-42a5-bed3-0ffe1c491926",
		Name:          "orders",
		Type:          "TABLE",
		ConnectionKey: "acme_dev_databricks",
		Path: []DataObjectPathSegment{
			{ID: "c1", Name: "acme_dev", Type: "CATALOG"},
			{ID: "s1", Name: "sales", Type: "SCHEMA"},
			{ID: "e6cb340a-4a44-42a5-bed3-0ffe1c491926", Name: "orders", Type: "TABLE"},
		},
		SyncPolicy:          &enabled,
		SyncPolicyInherited: true,
	}
	if !reflect.DeepEqual(object, want) {
		t.Errorf("object = %+v, want %+v", object, want)
	}
	if got := object.PathNames(); !reflect.DeepEqual(got, []string{"acme_dev", "sales", "orders"}) {
		t.Errorf("path names = %v", got)
	}

	// Set directly on the object: no inheritedFrom.
	direct, err := decodeDataObject([]byte(`{"id": "x", "settings": {"syncPolicy": {"value": false}}}`))
	if err != nil || direct.SyncPolicy == nil || *direct.SyncPolicy || direct.SyncPolicyInherited {
		t.Errorf("direct = %+v, err = %v", direct, err)
	}
}

func TestSetDataObjectSyncPolicy(t *testing.T) {
	t.Parallel()

	server, requests := recordingServer(t, http.StatusOK, dataObjectResponse)
	client := newTestClient(t, server.URL)

	enabled := true
	if err := client.SetDataObjectSyncPolicy(t.Context(), "acme_dev_databricks", "d1", &enabled); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if err := client.SetDataObjectSyncPolicy(t.Context(), "acme_dev_databricks", "d1", nil); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	want := []recorded{
		// Only syncPolicy is sent: owners are left as they are.
		{http.MethodPatch, "/api/connections/acme_dev_databricks/dataObjects/d1", testAPIKey, map[string]any{"settings": map[string]any{"syncPolicy": true}}},
		// null clears it, so that it is inherited again.
		{http.MethodPatch, "/api/connections/acme_dev_databricks/dataObjects/d1", testAPIKey, map[string]any{"settings": map[string]any{"syncPolicy": nil}}},
	}
	if !reflect.DeepEqual(*requests, want) {
		t.Errorf("requests = %+v, want %+v", *requests, want)
	}
}

func TestSearchDataObjectsFollowsTheCursor(t *testing.T) {
	t.Parallel()

	var bodies []any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/connections/dataObjects/_search" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		bodies = append(bodies, body)

		if len(bodies) == 1 {
			writeTestJSON(w, http.StatusOK, `{"data": [`+dataObjectResponse+`], "meta": {"page": {"limit": 100, "cursor": "next"}}}`)

			return
		}
		writeTestJSON(w, http.StatusOK, `{"data": [{"id": "other", "name": "orders"}], "meta": {"page": {"limit": 100}}}`)
	}))
	t.Cleanup(server.Close)

	objects, err := newTestClient(t, server.URL).SearchDataObjects(t.Context(), "acme_dev_databricks", "orders")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if len(objects) != 2 || objects[1].ID != "other" {
		t.Fatalf("objects = %+v", objects)
	}

	filter := map[string]any{
		"connection": map[string]any{"connectionKey": "acme_dev_databricks"},
		"dataObject": map[string]any{"name": "orders"},
	}
	want := []any{
		map[string]any{"filter": filter, "meta": map[string]any{"page": map[string]any{"limit": float64(100)}}},
		map[string]any{"filter": filter, "meta": map[string]any{"page": map[string]any{"limit": float64(100), "cursor": "next"}}},
	}
	if !reflect.DeepEqual(bodies, want) {
		t.Errorf("bodies = %v, want %v", bodies, want)
	}
}

func TestDataObjectErrors(t *testing.T) {
	t.Parallel()

	server, requests := recordingServer(t, http.StatusNotFound, `{"errors": [{"code": "NOT_FOUND", "message": "Data object not found"}]}`)
	client := newTestClient(t, server.URL)

	if _, err := client.GetDataObject(t.Context(), "acme", "gone"); !IsNotFound(err) {
		t.Errorf("error = %v, want not found", err)
	}

	before := len(*requests)
	if _, err := client.SearchDataObjects(t.Context(), "acme", " "); err == nil {
		t.Error("SearchDataObjects accepted an empty name")
	}
	if err := client.SetDataObjectSyncPolicy(t.Context(), "acme", "", nil); err == nil {
		t.Error("SetDataObjectSyncPolicy accepted an empty id")
	}
	if _, err := client.GetDataObject(t.Context(), "", "d1"); err == nil {
		t.Error("GetDataObject accepted an empty connection key")
	}
	if len(*requests) != before {
		t.Error("invalid arguments reached the API")
	}
	if _, err := decodeDataObject([]byte(`{"name": "no id"}`)); err == nil {
		t.Error("decodeDataObject accepted a data object without an id")
	}
}

func TestPatchDataObjectTags(t *testing.T) {
	t.Parallel()

	server, requests := recordingServer(t, http.StatusOK, dataObjectResponse)
	client := newTestClient(t, server.URL)

	if err := client.PatchDataObjectTags(t.Context(), "acme_dev_databricks", "d1", []int64{192}, nil); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if err := client.PatchDataObjectTags(t.Context(), "acme_dev_databricks", "d1", nil, []int64{192, 7}); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	path := "/api/connections/acme_dev_databricks/dataObjects/d1"
	want := []recorded{
		// Only tags are sent, never settings, and an empty list is [] not null.
		{http.MethodPatch, path, testAPIKey, map[string]any{"tags": map[string]any{
			"add":    []any{map[string]any{"id": float64(192)}},
			"remove": []any{},
		}}},
		{http.MethodPatch, path, testAPIKey, map[string]any{"tags": map[string]any{
			"add":    []any{},
			"remove": []any{map[string]any{"id": float64(192)}, map[string]any{"id": float64(7)}},
		}}},
	}
	if !reflect.DeepEqual(*requests, want) {
		t.Errorf("requests = %+v, want %+v", *requests, want)
	}

	if err := client.PatchDataObjectTags(t.Context(), "acme_dev_databricks", "d1", nil, nil); err == nil {
		t.Error("a patch with no tags should be rejected before it is sent")
	}
	if err := client.PatchDataObjectTags(t.Context(), "acme_dev_databricks", " ", []int64{1}, nil); err == nil {
		t.Error("a patch without a data object id should be rejected")
	}
	if len(*requests) != 2 {
		t.Errorf("invalid patches were sent: %d requests", len(*requests))
	}
}

func TestDecodeDataObjectTags(t *testing.T) {
	t.Parallel()

	object, err := decodeDataObject([]byte(`{
		"id": "x",
		"tags": [
			{"id": 192, "name": "Access.Layer.Gold", "displayName": "Gold", "source": "curated",
			 "application": {"appliedAt": "2026-10-08T00:00:00Z", "appliedBy": null}},
			{"id": 7, "name": "Discovered.PII", "displayName": "PII", "inheritedFrom": {"id": "s1", "type": "DATA_OBJECT"}},
			{"id": 8, "name": "Explicit null", "displayName": "Explicit null", "inheritedFrom": null}
		]
	}`))
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	want := []DataObjectTag{
		{ID: 192, Name: "Access.Layer.Gold"},
		{ID: 7, Name: "Discovered.PII", Inherited: true},
		{ID: 8, Name: "Explicit null"},
	}
	if !reflect.DeepEqual(object.Tags, want) {
		t.Errorf("tags = %+v, want %+v", object.Tags, want)
	}

	if tag, ok := object.DirectTag(192); !ok || tag.Name != "Access.Layer.Gold" {
		t.Errorf("DirectTag(192) = %+v, %t", tag, ok)
	}
	// Inherited assignments are not applied to the data object itself.
	if _, ok := object.DirectTag(7); ok {
		t.Error("DirectTag(7) should ignore an inherited tag")
	}
	if _, ok := object.DirectTag(999); ok {
		t.Error("DirectTag(999) should not find a missing tag")
	}
}
