package immutaapi

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestSearchUsersFollowsPages(t *testing.T) {
	t.Parallel()

	const total = userSearchPageSize + 1

	var queries []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/bim/user" {
			t.Errorf("request = %s %s, want GET /bim/user", r.Method, r.URL.Path)
		}
		queries = append(queries, r.URL.RawQuery)

		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		size, _ := strconv.Atoi(r.URL.Query().Get("size"))
		var hits []string
		for i := offset; i < min(offset+size, total); i++ {
			hits = append(hits, fmt.Sprintf(`{"iamid": "okta", "userid": "user%d", "profile": {"id": %d, "name": "User %d", "email": "user%d@example.com"}}`, i, i+1, i, i))
		}
		writeTestJSON(w, http.StatusOK, fmt.Sprintf(`{"count": %d, "hits": [%s]}`, total, strings.Join(hits, ",")))
	}))
	t.Cleanup(server.Close)

	users, err := newTestClient(t, server.URL).SearchUsers(t.Context(), SearchUsersOptions{Email: "@example.com", IAMID: "okta"})
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	if len(users) != total {
		t.Fatalf("got %d users, want %d", len(users), total)
	}
	last := users[total-1]
	if last.ID != total || last.IAMID != "okta" || last.UserID != fmt.Sprintf("user%d", total-1) || last.Profile.Email != fmt.Sprintf("user%d@example.com", total-1) {
		t.Errorf("last user = %+v, profile %+v", last, last.Profile)
	}
	wantQueries := []string{
		"email=%40example.com&iamid=okta&offset=0&size=100",
		"email=%40example.com&iamid=okta&offset=100&size=100",
	}
	if !reflect.DeepEqual(queries, wantQueries) {
		t.Errorf("queries = %v, want %v", queries, wantQueries)
	}
}

func TestDecodeUserList(t *testing.T) {
	t.Parallel()

	user := `{"iamid": "bim", "userid": "jane", "profile": {"id": 7, "email": "jane@example.com"}}`
	for name, body := range map[string]string{
		"bare array": "[" + user + "]",
		"envelope":   `{"count": 1, "hits": [` + user + `]}`,
	} {
		users, err := decodeUserList([]byte(body))
		if err != nil || len(users) != 1 || users[0].ID != 7 || users[0].UserID != "jane" {
			t.Errorf("%s: users = %+v, err = %v", name, users, err)
		}
	}

	if _, err := decodeUserList([]byte(`{"count": 0}`)); err == nil {
		t.Error("decodeUserList accepted a response without users")
	}
}

func TestSearchUsersErrors(t *testing.T) {
	t.Parallel()

	server, _ := recordingServer(t, http.StatusForbidden, `{"message": "USER_ADMIN required"}`)
	client := newTestClient(t, server.URL)

	if _, err := client.SearchUsers(t.Context(), SearchUsersOptions{Email: "jane@example.com"}); err == nil || !strings.Contains(err.Error(), "403") {
		t.Errorf("error = %v, want the 403", err)
	}
	if _, err := client.SearchUsers(t.Context(), SearchUsersOptions{IAMID: "bim"}); err == nil {
		t.Error("SearchUsers searched without an email or user id")
	}
}
