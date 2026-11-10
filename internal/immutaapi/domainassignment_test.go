package immutaapi

import (
	"net/http"
	"reflect"
	"testing"
)

func TestSetDomainAssignment(t *testing.T) {
	t.Parallel()

	server, requests := recordingServer(t, http.StatusOK, `{"assignmentType": "dynamic", "dataSourcesAdded": 3, "dataSourcesKept": 1, "dataSourcesRemoved": 2}`)
	client := newTestClient(t, server.URL)

	result, err := client.SetDomainAssignment(t.Context(), "d/1", DomainAssignmentDynamic, []string{"Discovered.Country.Denmark", "Sales"})
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if want := (&DomainAssignmentResult{AssignmentType: "dynamic", DataSourcesAdded: 3, DataSourcesKept: 1, DataSourcesRemoved: 2}); !reflect.DeepEqual(result, want) {
		t.Errorf("result = %+v, want %+v", result, want)
	}

	if _, err := client.SetDomainAssignment(t.Context(), "d/1", DomainAssignmentManual, nil); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	want := []recorded{
		{http.MethodPost, "/domain/d%2F1/assignment", testAPIKey, map[string]any{
			"assignmentType": "dynamic",
			"tags":           []any{map[string]any{"name": "Discovered.Country.Denmark"}, map[string]any{"name": "Sales"}},
		}},
		// Manual sends no tags.
		{http.MethodPost, "/domain/d%2F1/assignment", testAPIKey, map[string]any{"assignmentType": "manual"}},
	}
	if !reflect.DeepEqual(*requests, want) {
		t.Errorf("requests = %+v, want %+v", *requests, want)
	}
}

func TestSetDomainAssignmentErrors(t *testing.T) {
	t.Parallel()

	server, requests := recordingServer(t, http.StatusNotFound, `{"message": "Domain not found"}`)
	client := newTestClient(t, server.URL)

	if _, err := client.SetDomainAssignment(t.Context(), "missing", DomainAssignmentManual, nil); !IsNotFound(err) {
		t.Errorf("error = %v, want not found", err)
	}

	before := len(*requests)
	for name, call := range map[string]func() error{
		"dynamic without tags": func() error {
			_, err := client.SetDomainAssignment(t.Context(), "d1", DomainAssignmentDynamic, nil)
			return err
		},
		"manual with tags": func() error {
			_, err := client.SetDomainAssignment(t.Context(), "d1", DomainAssignmentManual, []string{"Sales"})
			return err
		},
		"unknown type": func() error {
			_, err := client.SetDomainAssignment(t.Context(), "d1", "automatic", nil)
			return err
		},
		"no domain": func() error {
			_, err := client.SetDomainAssignment(t.Context(), " ", DomainAssignmentManual, nil)
			return err
		},
	} {
		if call() == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if len(*requests) != before {
		t.Error("invalid arguments reached the API")
	}
}
