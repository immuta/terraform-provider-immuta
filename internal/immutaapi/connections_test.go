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

const trinoConnectionResponse = `{
	"connectionKey": "trino",
	"technology": "Trino",
	"state": "ACTIVATING",
	"clientConfiguration": {"host": "trino.example.com", "port": 443, "ssl": true, "credential": {"authenticationType": "UsernamePassword", "username": "svc"}},
	"activeTasks": []
}`

func TestCreateConnectionPostsThePayload(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath string
	var gotBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.EscapedPath()
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		writeTestJSON(w, http.StatusCreated, trinoConnectionResponse)
	}))
	t.Cleanup(server.Close)

	body := map[string]any{
		"technology":    "Trino",
		"connectionKey": "trino",
		"clientConfiguration": map[string]any{
			"host":       "trino.example.com",
			"credential": map[string]any{"authenticationType": "UsernamePassword", "username": "svc", "password": "secret"},
		},
	}

	connection, err := newTestClient(t, server.URL).CreateConnection(t.Context(), body)
	if err != nil {
		t.Fatalf("CreateConnection returned an unexpected error: %s", err)
	}

	if gotMethod != http.MethodPost || gotPath != "/api/connections" {
		t.Errorf("request = %s %s, want POST /api/connections", gotMethod, gotPath)
	}
	if !reflect.DeepEqual(gotBody, body) {
		t.Errorf("request body = %v, want the payload unchanged", gotBody)
	}
	if connection.ConnectionKey != "trino" || connection.Technology != "Trino" || connection.State != ConnectionStateActivating {
		t.Errorf("connection = %+v, want the decoded response", connection)
	}
	if host := connection.Raw["clientConfiguration"].(map[string]any)["host"]; host != "trino.example.com" {
		t.Errorf("raw host = %v, want the whole response kept", host)
	}
}

func TestCreateConnectionRequiresAKeyAndTechnology(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, "https://example.invalid")

	if _, err := client.CreateConnection(t.Context(), map[string]any{"technology": "Trino"}); err == nil {
		t.Error("CreateConnection accepted a payload without a connectionKey")
	}
	if _, err := client.CreateConnection(t.Context(), map[string]any{"connectionKey": "k"}); err == nil {
		t.Error("CreateConnection accepted a payload without a technology")
	}
}

func TestCreateConnectionReportsTheDocumentedErrors(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeTestJSON(w, http.StatusConflict, `{"errors": [{"code": "CONN002", "message": "A connection with this key already exists"}]}`)
	}))
	t.Cleanup(server.Close)

	_, err := newTestClient(t, server.URL).CreateConnection(t.Context(), map[string]any{"connectionKey": "k", "technology": "Trino"})

	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusConflict {
		t.Fatalf("error = %v, want a 409 API error", err)
	}
	if !strings.Contains(err.Error(), "CONN002: A connection with this key already exists") {
		t.Errorf("error %q should carry Immuta's reason", err)
	}
}

func TestGetConnection(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() == "/api/connections/trino" {
			writeTestJSON(w, http.StatusOK, trinoConnectionResponse)

			return
		}
		writeTestJSON(w, http.StatusNotFound, `{"errors": [{"code": "CONN004", "message": "not found"}]}`)
	}))
	t.Cleanup(server.Close)

	client := newTestClient(t, server.URL)

	connection, err := client.GetConnection(t.Context(), "trino")
	if err != nil || connection.State != ConnectionStateActivating {
		t.Fatalf("connection = %+v, err = %v", connection, err)
	}
	if _, err := client.GetConnection(t.Context(), "missing"); !IsNotFound(err) {
		t.Errorf("error = %v, want not found", err)
	}
	if _, err := client.GetConnection(t.Context(), " "); err == nil {
		t.Error("GetConnection accepted an empty key")
	}
}

func TestUpdateConnectionPatchesOnlyTheGivenFields(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath string
	var gotBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.EscapedPath()
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		writeTestJSON(w, http.StatusOK, trinoConnectionResponse)
	}))
	t.Cleanup(server.Close)

	patch := map[string]any{"dataObjectSync": map[string]any{"schedule": "EVERY_6_HOURS"}}
	if _, err := newTestClient(t, server.URL).UpdateConnection(t.Context(), "trino", patch); err != nil {
		t.Fatalf("UpdateConnection returned an unexpected error: %s", err)
	}

	if gotMethod != http.MethodPatch || gotPath != "/api/connections/trino" {
		t.Errorf("request = %s %s, want PATCH /api/connections/trino", gotMethod, gotPath)
	}
	if !reflect.DeepEqual(gotBody, patch) {
		t.Errorf("request body = %v, want %v", gotBody, patch)
	}
}

func TestDeleteConnectionDecodesTheTask(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.EscapedPath()
		writeTestJSON(w, http.StatusAccepted, `{"taskId": "task-1", "teardown": {"script": "DROP ROLE IMMUTA;"}}`)
	}))
	t.Cleanup(server.Close)

	deletion, err := newTestClient(t, server.URL).DeleteConnection(t.Context(), "trino")
	if err != nil {
		t.Fatalf("DeleteConnection returned an unexpected error: %s", err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/api/connections/trino" {
		t.Errorf("request = %s %s, want DELETE /api/connections/trino", gotMethod, gotPath)
	}
	if deletion.TaskID != "task-1" || !strings.Contains(string(deletion.Teardown), "DROP ROLE") {
		t.Errorf("deletion = %+v, want the task id and teardown", deletion)
	}
}
