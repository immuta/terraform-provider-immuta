package immutaapi

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestSearchRequestAppUsers(t *testing.T) {
	t.Parallel()

	server, requests := recordingServer(t, http.StatusOK, `{"users": [
		{"id": 7, "iamId": "okta", "globalUserId": "99aeadb5-352e-4872-9983-e628d399c36e", "trackingId": "t",
		 "username": "jane", "name": "Jane Doe", "email": null, "authorizations": {"Department": ["Sales"]}}
	]}`)

	users, truncated, err := newTestRequestAppClient(t, server.URL).SearchRequestAppUsers(t.Context(), "jane doe")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	want := []RequestAppUser{{IAMID: "okta", GlobalUserID: "99aeadb5-352e-4872-9983-e628d399c36e", Username: "jane", Name: "Jane Doe"}}
	if !reflect.DeepEqual(users, want) || truncated {
		t.Errorf("users = %+v, truncated = %t, want %+v, false", users, truncated, want)
	}

	got := (*requests)[0]
	if got.method != http.MethodGet || got.path != "/marketplace/api/metadata/users" || got.authorization != "Bearer "+testRequestAppToken {
		t.Errorf("request = %+v, want GET /marketplace/api/metadata/users with the bearer token", got)
	}
}

func TestSearchRequestAppUsersQuery(t *testing.T) {
	t.Parallel()

	var query string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		writeTestJSON(w, http.StatusOK, `{"users": []}`)
	}))
	t.Cleanup(server.Close)

	users, _, err := newTestRequestAppClient(t, server.URL).SearchRequestAppUsers(t.Context(), "jane&doe")
	if err != nil || len(users) != 0 {
		t.Fatalf("users = %v, err = %v, want none", users, err)
	}
	if query != "filter=jane%26doe&limit=100" {
		t.Errorf("query = %q, want the filter escaped and the limit set", query)
	}
}

func TestSearchRequestAppUsersTruncated(t *testing.T) {
	t.Parallel()

	users := make([]string, RequestAppUserSearchLimit)
	for i := range users {
		users[i] = fmt.Sprintf(`{"iamId": "bim", "globalUserId": "g%d", "username": "u%d", "name": "U"}`, i, i)
	}
	server, _ := recordingServer(t, http.StatusOK, `{"users": [`+strings.Join(users, ",")+`]}`)

	got, truncated, err := newTestRequestAppClient(t, server.URL).SearchRequestAppUsers(t.Context(), "u")
	if err != nil || len(got) != RequestAppUserSearchLimit || !truncated {
		t.Errorf("len = %d, truncated = %t, err = %v, want a full, truncated page", len(got), truncated, err)
	}
}

func TestSearchRequestAppUsersErrors(t *testing.T) {
	t.Parallel()

	server, requests := recordingServer(t, http.StatusOK, `{"count": 0}`)
	client := newTestRequestAppClient(t, server.URL)

	if _, _, err := client.SearchRequestAppUsers(t.Context(), " "); err == nil {
		t.Error("an empty filter was accepted")
	}
	if len(*requests) != 0 {
		t.Error("an empty filter was sent")
	}
	if _, _, err := client.SearchRequestAppUsers(t.Context(), "jane"); err == nil {
		t.Error("a response without a users list was accepted")
	}
	if _, _, err := newTestClient(t, server.URL).SearchRequestAppUsers(t.Context(), "jane"); !errors.Is(err, ErrRequestAppNotConfigured) {
		t.Errorf("error = %v, want ErrRequestAppNotConfigured", err)
	}
}
