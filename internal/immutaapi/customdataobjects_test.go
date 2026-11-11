package immutaapi

import (
	"net/http"
	"reflect"
	"testing"
)

const customSecurableResponse = `{
	"id": "7d1d0c1e-5b0e-4a77-9a55-3b0f1c2d3e4f",
	"name": "Analyst Access",
	"type": "SECURABLE",
	"connectionKey": "snowflake-access",
	"objectPath": {
		"id": "f1", "name": "Production", "type": "FOLDER",
		"child": {"id": "7d1d0c1e-5b0e-4a77-9a55-3b0f1c2d3e4f", "name": "Analyst Access", "type": "SECURABLE"}
	}
}`

func TestCreateDataObject(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		parentID string
		want     map[string]any
	}{
		// The API rejects a null parentId, so it is left out at the top.
		{"at the top of the connection", "", map[string]any{"name": "Analyst Access", "type": "SECURABLE"}},
		{"in a folder", "f1", map[string]any{"name": "Analyst Access", "type": "SECURABLE", "parentId": "f1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			server, requests := recordingServer(t, http.StatusCreated, customSecurableResponse)

			object, err := newTestClient(t, server.URL).CreateDataObject(t.Context(), "snowflake-access", "Analyst Access", DataObjectTypeSecurable, tc.parentID)
			if err != nil {
				t.Fatalf("unexpected error: %s", err)
			}

			got := (*requests)[0]
			if got.method != http.MethodPost || got.path != "/api/connections/snowflake-access/dataObjects" {
				t.Errorf("request = %s %s", got.method, got.path)
			}
			if !reflect.DeepEqual(got.body, tc.want) {
				t.Errorf("body = %v, want %v", got.body, tc.want)
			}
			if object.ID != "7d1d0c1e-5b0e-4a77-9a55-3b0f1c2d3e4f" || object.Type != DataObjectTypeSecurable {
				t.Errorf("object = %+v", object)
			}
		})
	}
}

func TestCreateDataObjectRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, "https://tenant.invalid")
	for _, tc := range []struct{ name, key, objectName, objectType string }{
		{"no connection key", "", "a", DataObjectTypeFolder},
		{"blank name", "k", "  ", DataObjectTypeFolder},
		{"unsupported type", "k", "a", "TABLE"},
	} {
		if _, err := client.CreateDataObject(t.Context(), tc.key, tc.objectName, tc.objectType, ""); err == nil {
			t.Errorf("%s: expected an error", tc.name)
		}
	}
}

func TestRenameAndDeleteDataObject(t *testing.T) {
	t.Parallel()

	server, requests := recordingServer(t, http.StatusAccepted, `{"status": "accepted", "taskId": "00000000-0000-4000-8000-000000000001"}`)
	client := newTestClient(t, server.URL)

	if err := client.RenameDataObject(t.Context(), "snowflake-access", "s1", "Analysts"); err != nil {
		t.Fatalf("rename: %s", err)
	}
	if err := client.DeleteDataObject(t.Context(), "snowflake-access", "s1"); err != nil {
		t.Fatalf("delete: %s", err)
	}

	want := []recorded{
		{http.MethodPost, "/api/connections/snowflake-access/dataObjects/s1/_rename", "", map[string]any{"name": "Analysts"}},
		{http.MethodDelete, "/api/connections/snowflake-access/dataObjects/s1", "", nil},
	}
	if len(*requests) != len(want) {
		t.Fatalf("%d requests, want %d", len(*requests), len(want))
	}
	for i, got := range *requests {
		got.authorization = ""
		if !reflect.DeepEqual(got, want[i]) {
			t.Errorf("request %d = %+v, want %+v", i, got, want[i])
		}
	}
}

func TestAccessGroupRequests(t *testing.T) {
	t.Parallel()

	server, requests := recordingServer(t, http.StatusOK, `{"identityGroupId": "g1"}`)
	client := newTestClient(t, server.URL)

	if err := client.BindAccessGroup(t.Context(), "snowflake-access", "s1", "g1"); err != nil {
		t.Fatalf("bind: %s", err)
	}
	bound, err := client.GetAccessGroup(t.Context(), "snowflake-access", "s1")
	if err != nil || bound != "g1" {
		t.Fatalf("get = %q, %v", bound, err)
	}
	if err := client.UnbindAccessGroup(t.Context(), "snowflake-access", "s1", "g1"); err != nil {
		t.Fatalf("unbind: %s", err)
	}

	const endpoint = "/api/connections/snowflake-access/dataObjects/s1/accessGroup"
	want := []recorded{
		{http.MethodPut, endpoint, "", map[string]any{"action": "bind", "identityGroupId": "g1"}},
		{http.MethodGet, endpoint, "", nil},
		{http.MethodPut, endpoint, "", map[string]any{"action": "unbind", "identityGroupId": "g1"}},
	}
	if len(*requests) != len(want) {
		t.Fatalf("%d requests, want %d", len(*requests), len(want))
	}
	for i, got := range *requests {
		got.authorization = ""
		if !reflect.DeepEqual(got, want[i]) {
			t.Errorf("request %d = %+v, want %+v", i, got, want[i])
		}
	}

	if err := client.BindAccessGroup(t.Context(), "snowflake-access", "s1", " "); err == nil {
		t.Error("bind without a group: expected an error")
	}
}

func TestDecodeAccessGroup(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		body    string
		want    string
		wantErr bool
	}{
		{"empty body is unbound", "", "", false},
		{"null is unbound", "null", "", false},
		{"identityGroupId", `{"identityGroupId": "g1"}`, "g1", false},
		{"null identityGroupId is unbound", `{"identityGroupId": null}`, "", false},
		{"identityGroup object", `{"identityGroup": {"id": "g1", "name": "IMMUTA_ANALYSTS"}}`, "g1", false},
		{"null identityGroup is unbound", `{"identityGroup": null}`, "", false},
		{"identityGroup without id", `{"identityGroup": {}}`, "", true},
		// An unknown shape must not be read as unbound, which would make
		// Terraform forget the binding.
		{"unknown shape", `{"group": "g1"}`, "", true},
		{"not JSON", `<html>`, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := decodeAccessGroup([]byte(tc.body))
			if (err != nil) != tc.wantErr || got != tc.want {
				t.Errorf("decodeAccessGroup(%q) = %q, %v; want %q, error %t", tc.body, got, err, tc.want, tc.wantErr)
			}
		})
	}
}
