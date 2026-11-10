package immutaapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestUserDecodesDocumentedResponseShapes(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		body        string
		wantID      int64
		wantName    string
		wantEmail   string
		wantProfile bool
	}{
		"detailed user object carries the id on the profile": {
			body: `{"iamid":"bim","userid":"jane@immuta.com","permissions":["USER_ADMIN"],
				"profile":{"id":2,"name":"Jane Doe","email":"jane@immuta.com","phone":null}}`,
			wantID:      2,
			wantName:    "Jane Doe",
			wantEmail:   "jane@immuta.com",
			wantProfile: true,
		},
		"plain user object carries the id at the top level and the profile as a number": {
			body:        `{"id":7,"iamid":"bim","userid":"jane@immuta.com","profile":7}`,
			wantID:      7,
			wantProfile: true,
		},
		"a null profile is tolerated": {
			body:        `{"id":9,"iamid":"bim","userid":"jane@immuta.com","profile":null}`,
			wantID:      9,
			wantProfile: false,
		},
		"an omitted profile is tolerated": {
			body:        `{"id":11,"iamid":"bim","userid":"jane@immuta.com"}`,
			wantID:      11,
			wantProfile: false,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var user User
			if err := json.Unmarshal([]byte(test.body), &user); err != nil {
				t.Fatalf("decoding the user returned an unexpected error: %s", err)
			}

			if user.ID != test.wantID {
				t.Errorf("user id = %d, want %d", user.ID, test.wantID)
			}

			if (user.Profile != nil) != test.wantProfile {
				t.Fatalf("profile present = %t, want %t", user.Profile != nil, test.wantProfile)
			}

			if test.wantProfile {
				if user.Profile.Name != test.wantName {
					t.Errorf("profile name = %q, want %q", user.Profile.Name, test.wantName)
				}
				if user.Profile.Email != test.wantEmail {
					t.Errorf("profile email = %q, want %q", user.Profile.Email, test.wantEmail)
				}
			}
		})
	}
}

func TestAuthorizationsDecoding(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		body string
		want Authorizations
	}{
		"the documented map of names to values": {
			body: `{"Department":["Finance","Legal"]}`,
			want: Authorizations{"Department": {"Finance", "Legal"}},
		},
		"an empty object": {
			body: `{}`,
			want: Authorizations{},
		},
		"null": {
			body: `null`,
			want: nil,
		},
		"a bare string is read as a single value": {
			body: `{"Department":"Finance"}`,
			want: Authorizations{"Department": {"Finance"}},
		},
		"unexpected value shapes are skipped rather than failing the read": {
			body: `{"Department":["Finance"],"Nested":{"unexpected":true}}`,
			want: Authorizations{"Department": {"Finance"}},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var got Authorizations
			if err := json.Unmarshal([]byte(test.body), &got); err != nil {
				t.Fatalf("decoding authorizations returned an unexpected error: %s", err)
			}

			if !reflect.DeepEqual(got, test.want) {
				t.Errorf("authorizations = %v, want %v", got, test.want)
			}
		})
	}
}

func TestAuthorizationsHas(t *testing.T) {
	t.Parallel()

	authorizations := Authorizations{"Department": {"Finance"}}

	if !authorizations.Has("Department", "Finance") {
		t.Error("Has did not find an assigned value")
	}
	if authorizations.Has("Department", "Legal") {
		t.Error("Has found a value that is not assigned")
	}
	if authorizations.Has("Missing", "Finance") {
		t.Error("Has found a value under a name that is not assigned")
	}
	if (Authorizations)(nil).Has("Department", "Finance") {
		t.Error("Has found a value in a nil map")
	}
}

func TestCreateUserSendsTheDocumentedBodyAndUnwrapsNewUser(t *testing.T) {
	t.Parallel()

	var gotBody map[string]any
	var gotMethod, gotPath string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.EscapedPath()
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &gotBody); err != nil {
			t.Errorf("request body is not valid JSON: %s", err)
		}
		writeTestJSON(w, http.StatusOK, `{"newUser":{"id":4,"iamid":"bim","userid":"jane@immuta.com"},"newUserLink":"https://example.invalid/?token=secret"}`)
	}))
	t.Cleanup(server.Close)

	client := newTestClient(t, server.URL)

	user, err := client.CreateUser(t.Context(), CreateUserRequest{
		IAMID:       IAMIDBIM,
		UserID:      "jane@immuta.com",
		Profile:     &CreateUserProfile{Name: "Jane Doe", Email: "jane@immuta.com"},
		Permissions: []string{"CREATE_PROJECT"},
	})
	if err != nil {
		t.Fatalf("CreateUser returned an unexpected error: %s", err)
	}

	if user.ID != 4 {
		t.Errorf("user id = %d, want 4", user.ID)
	}

	if gotMethod != http.MethodPost || gotPath != "/bim/iam/bim/user" {
		t.Errorf("request = %s %s, want POST /bim/iam/bim/user", gotMethod, gotPath)
	}

	want := map[string]any{
		"iamid":       "bim",
		"userid":      "jane@immuta.com",
		"profile":     map[string]any{"name": "Jane Doe", "email": "jane@immuta.com"},
		"permissions": []any{"CREATE_PROJECT"},
	}
	if !reflect.DeepEqual(gotBody, want) {
		t.Errorf("request body = %v, want %v", gotBody, want)
	}
}

func TestCreateUserSendsAnEmptyPermissionsList(t *testing.T) {
	t.Parallel()

	var gotBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		writeTestJSON(w, http.StatusOK, `{"newUser":{"id":4,"userid":"jane@immuta.com"}}`)
	}))
	t.Cleanup(server.Close)

	client := newTestClient(t, server.URL)

	if _, err := client.CreateUser(t.Context(), CreateUserRequest{UserID: "jane@immuta.com"}); err != nil {
		t.Fatalf("CreateUser returned an unexpected error: %s", err)
	}

	permissions, ok := gotBody["permissions"].([]any)
	if !ok || len(permissions) != 0 {
		t.Errorf("permissions = %v, want an empty array", gotBody["permissions"])
	}

	// An unset IAM id falls back to Immuta's built-in identity manager.
	if gotBody["iamid"] != IAMIDBIM {
		t.Errorf("iamid = %v, want %q", gotBody["iamid"], IAMIDBIM)
	}
}

func TestGetUserUsesTheNumericID(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.EscapedPath()
		writeTestJSON(w, http.StatusOK, `{"iamid":"bim","userid":"jane@immuta.com","profile":{"id":18}}`)
	}))
	t.Cleanup(server.Close)

	client := newTestClient(t, server.URL)

	user, err := client.GetUser(t.Context(), IAMIDBIM, 18)
	if err != nil {
		t.Fatalf("GetUser returned an unexpected error: %s", err)
	}

	if user.ID != 18 {
		t.Errorf("user id = %d, want 18", user.ID)
	}

	if gotMethod != http.MethodGet || gotPath != "/bim/iam/bim/user/18" {
		t.Errorf("request = %s %s, want GET /bim/iam/bim/user/18", gotMethod, gotPath)
	}
}

func TestGetUserReturnsNotFound(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeTestJSON(w, http.StatusNotFound, `{"message":"user not found"}`)
	}))
	t.Cleanup(server.Close)

	client := newTestClient(t, server.URL)

	_, err := client.GetUser(t.Context(), IAMIDBIM, 18)
	if !IsNotFound(err) {
		t.Fatalf("GetUser error = %v, want a not-found error", err)
	}
}

func TestUpdateUserProfileSendsOnlyTheSuppliedFields(t *testing.T) {
	t.Parallel()

	phone := "555-0100"
	empty := ""

	tests := map[string]struct {
		request UpdateUserProfileRequest
		want    map[string]any
	}{
		"a single field": {
			request: UpdateUserProfileRequest{Phone: &phone},
			want:    map[string]any{"phone": "555-0100"},
		},
		"an explicitly cleared field": {
			request: UpdateUserProfileRequest{Location: &empty},
			want:    map[string]any{"location": ""},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var gotBody map[string]any
			var gotMethod, gotPath string

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod, gotPath = r.Method, r.URL.EscapedPath()
				raw, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(raw, &gotBody)
				w.WriteHeader(http.StatusNoContent)
			}))
			t.Cleanup(server.Close)

			client := newTestClient(t, server.URL)

			if err := client.UpdateUserProfile(t.Context(), IAMIDBIM, "jane@immuta.com", test.request); err != nil {
				t.Fatalf("UpdateUserProfile returned an unexpected error: %s", err)
			}

			if gotMethod != http.MethodPut || gotPath != "/bim/iam/bim/user/jane@immuta.com/profile" {
				t.Errorf("request = %s %s, want PUT /bim/iam/bim/user/jane@immuta.com/profile", gotMethod, gotPath)
			}

			if !reflect.DeepEqual(gotBody, test.want) {
				t.Errorf("request body = %v, want %v", gotBody, test.want)
			}
		})
	}
}

func TestUpdateUserProfileRequestIsEmpty(t *testing.T) {
	t.Parallel()

	value := "x"

	if !(UpdateUserProfileRequest{}).IsEmpty() {
		t.Error("an unset request is not reported as empty")
	}
	if (UpdateUserProfileRequest{Position: &value}).IsEmpty() {
		t.Error("a request with a field set is reported as empty")
	}
}

func TestUpdateUserPermissionsSendsABareArray(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		permissions []string
		want        []any
	}{
		"a list of permissions":      {permissions: []string{"USER_ADMIN"}, want: []any{"USER_ADMIN"}},
		"no permissions clears them": {permissions: nil, want: []any{}},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var gotBody []any
			var gotMethod, gotPath string

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod, gotPath = r.Method, r.URL.EscapedPath()
				raw, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(raw, &gotBody)
				w.WriteHeader(http.StatusNoContent)
			}))
			t.Cleanup(server.Close)

			client := newTestClient(t, server.URL)

			if err := client.UpdateUserPermissions(t.Context(), IAMIDBIM, "jane@immuta.com", test.permissions); err != nil {
				t.Fatalf("UpdateUserPermissions returned an unexpected error: %s", err)
			}

			if gotMethod != http.MethodPut || gotPath != "/bim/iam/bim/user/jane@immuta.com/permissions" {
				t.Errorf("request = %s %s, want PUT /bim/iam/bim/user/jane@immuta.com/permissions", gotMethod, gotPath)
			}

			if !reflect.DeepEqual(gotBody, test.want) {
				t.Errorf("request body = %v, want %v", gotBody, test.want)
			}
		})
	}
}

func TestSetUserDisabledPutsTheFlagInThePath(t *testing.T) {
	t.Parallel()

	for _, disabled := range []bool{true, false} {
		var gotMethod, gotPath string

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotMethod, gotPath = r.Method, r.URL.EscapedPath()
			writeTestJSON(w, http.StatusOK, `{"userid":"jane@immuta.com","disabled":true}`)
		}))

		client := newTestClient(t, server.URL)

		if err := client.SetUserDisabled(t.Context(), IAMIDBIM, "jane@immuta.com", disabled); err != nil {
			t.Fatalf("SetUserDisabled returned an unexpected error: %s", err)
		}

		wantPath := "/bim/iam/bim/user/jane@immuta.com/disable/false"
		if disabled {
			wantPath = "/bim/iam/bim/user/jane@immuta.com/disable/true"
		}

		if gotMethod != http.MethodPut || gotPath != wantPath {
			t.Errorf("request = %s %s, want PUT %s", gotMethod, gotPath, wantPath)
		}

		server.Close()
	}
}

func TestDeleteUserUsesTheUsername(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.EscapedPath()
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	client := newTestClient(t, server.URL)

	if err := client.DeleteUser(t.Context(), IAMIDBIM, "test@test.com"); err != nil {
		t.Fatalf("DeleteUser returned an unexpected error: %s", err)
	}

	if gotMethod != http.MethodDelete || gotPath != "/bim/iam/bim/user/test@test.com" {
		t.Errorf("request = %s %s, want DELETE /bim/iam/bim/user/test@test.com", gotMethod, gotPath)
	}
}

func TestGroupRequestsUseTheDocumentedPaths(t *testing.T) {
	t.Parallel()

	description := "Engineering group"

	tests := map[string]struct {
		call       func(*Client) error
		wantMethod string
		wantPath   string
		wantBody   map[string]any
	}{
		"create": {
			call: func(c *Client) error {
				_, err := c.CreateGroup(t.Context(), CreateGroupRequest{Name: "Engineering", Description: &description})

				return err
			},
			wantMethod: http.MethodPost,
			wantPath:   "/bim/group",
			wantBody:   map[string]any{"iamid": "bim", "name": "Engineering", "description": "Engineering group"},
		},
		"read": {
			call: func(c *Client) error {
				_, err := c.GetGroup(t.Context(), 12)

				return err
			},
			wantMethod: http.MethodGet,
			wantPath:   "/bim/group/12",
		},
		"update": {
			call: func(c *Client) error {
				return c.UpdateGroup(t.Context(), 12, UpdateGroupRequest{Description: &description})
			},
			wantMethod: http.MethodPut,
			wantPath:   "/bim/group/12",
			wantBody:   map[string]any{"description": "Engineering group"},
		},
		"delete": {
			call: func(c *Client) error {
				return c.DeleteGroup(t.Context(), 12)
			},
			wantMethod: http.MethodDelete,
			wantPath:   "/bim/group/12",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var gotMethod, gotPath string
			var gotBody map[string]any

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod, gotPath = r.Method, r.URL.EscapedPath()
				raw, _ := io.ReadAll(r.Body)
				if len(raw) > 0 {
					_ = json.Unmarshal(raw, &gotBody)
				}
				writeTestJSON(w, http.StatusOK, `{"id":12,"iamid":"bim","name":"Engineering"}`)
			}))
			t.Cleanup(server.Close)

			client := newTestClient(t, server.URL)

			if err := test.call(client); err != nil {
				t.Fatalf("the request returned an unexpected error: %s", err)
			}

			if gotMethod != test.wantMethod || gotPath != test.wantPath {
				t.Errorf("request = %s %s, want %s %s", gotMethod, gotPath, test.wantMethod, test.wantPath)
			}

			if test.wantBody != nil && !reflect.DeepEqual(gotBody, test.wantBody) {
				t.Errorf("request body = %v, want %v", gotBody, test.wantBody)
			}
		})
	}
}

func TestUpdateGroupRequestIsEmpty(t *testing.T) {
	t.Parallel()

	name := "Engineering"

	if !(UpdateGroupRequest{}).IsEmpty() {
		t.Error("an unset request is not reported as empty")
	}
	if (UpdateGroupRequest{Name: &name}).IsEmpty() {
		t.Error("a request with a field set is reported as empty")
	}
}

func TestAddGroupUserReturnsTheMembershipID(t *testing.T) {
	t.Parallel()

	var gotBody map[string]any
	var gotMethod, gotPath string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.EscapedPath()
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		writeTestJSON(w, http.StatusOK, `{"id":6,"group":2,"profile":2}`)
	}))
	t.Cleanup(server.Close)

	client := newTestClient(t, server.URL)

	membership, err := client.AddGroupUser(t.Context(), 2, IAMIDBIM, "jane@immuta.com")
	if err != nil {
		t.Fatalf("AddGroupUser returned an unexpected error: %s", err)
	}

	if membership.ID != 6 {
		t.Errorf("membership id = %d, want 6", membership.ID)
	}

	if gotMethod != http.MethodPost || gotPath != "/bim/group/2/user" {
		t.Errorf("request = %s %s, want POST /bim/group/2/user", gotMethod, gotPath)
	}

	want := map[string]any{"userid": "jane@immuta.com", "iamid": "bim"}
	if !reflect.DeepEqual(gotBody, want) {
		t.Errorf("request body = %v, want %v", gotBody, want)
	}
}

func TestAddGroupUserRejectsAResponseWithoutAMembershipID(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeTestJSON(w, http.StatusOK, `{"group":2,"profile":2}`)
	}))
	t.Cleanup(server.Close)

	client := newTestClient(t, server.URL)

	if _, err := client.AddGroupUser(t.Context(), 2, IAMIDBIM, "jane@immuta.com"); err == nil {
		t.Fatal("AddGroupUser accepted a response without a membership id")
	}
}

func TestFindUserGroup(t *testing.T) {
	t.Parallel()

	var gotPath string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		writeTestJSON(w, http.StatusOK, `[{"id":2,"name":"Engineering","iamid":"bim","groupUser":6}]`)
	}))
	t.Cleanup(server.Close)

	client := newTestClient(t, server.URL)

	membership, err := client.FindUserGroup(t.Context(), IAMIDBIM, "jane@immuta.com", 2)
	if err != nil {
		t.Fatalf("FindUserGroup returned an unexpected error: %s", err)
	}

	if membership.GroupUser != 6 {
		t.Errorf("membership id = %d, want 6", membership.GroupUser)
	}

	if gotPath != "/bim/iam/bim/user/jane@immuta.com/groups" {
		t.Errorf("path = %s, want /bim/iam/bim/user/jane@immuta.com/groups", gotPath)
	}

	if _, err := client.FindUserGroup(t.Context(), IAMIDBIM, "jane@immuta.com", 99); !IsNotFound(err) {
		t.Errorf("FindUserGroup error for a group the user is not in = %v, want a not-found error", err)
	}
}

func TestRemoveGroupUserUsesTheMembershipID(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.EscapedPath()
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	client := newTestClient(t, server.URL)

	if err := client.RemoveGroupUser(t.Context(), 2, 6); err != nil {
		t.Fatalf("RemoveGroupUser returned an unexpected error: %s", err)
	}

	if gotMethod != http.MethodDelete || gotPath != "/bim/group/2/user/6" {
		t.Errorf("request = %s %s, want DELETE /bim/group/2/user/6", gotMethod, gotPath)
	}
}

func TestAttributeRequestsEscapeThePathSegments(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		call       func(*Client) error
		wantMethod string
		wantPath   string
	}{
		"assigning a user attribute": {
			call: func(c *Client) error {
				return c.SetAttribute(t.Context(), IAMIDBIM, ModelTypeUser, "jane@immuta.com", "Department", "Finance Team")
			},
			wantMethod: http.MethodPut,
			wantPath:   "/bim/iam/bim/user/jane@immuta.com/authorizations/Department/Finance%20Team",
		},
		"removing a group attribute": {
			call: func(c *Client) error {
				return c.DeleteAttribute(t.Context(), IAMIDBIM, ModelTypeGroup, "12", "Department", "Finance")
			},
			wantMethod: http.MethodDelete,
			wantPath:   "/bim/iam/bim/group/12/authorizations/Department/Finance",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var gotMethod, gotPath string

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod, gotPath = r.Method, r.URL.EscapedPath()
				writeTestJSON(w, http.StatusOK, `{}`)
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

func TestBIMRequestsValidateTheirArguments(t *testing.T) {
	t.Parallel()

	// No request should reach the API when an argument is missing.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request %s %s", r.Method, r.URL.EscapedPath())
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	client := newTestClient(t, server.URL)

	tests := map[string]func() error{
		"create a user without a username": func() error {
			_, err := client.CreateUser(t.Context(), CreateUserRequest{})

			return err
		},
		"read a user without an id": func() error {
			_, err := client.GetUser(t.Context(), IAMIDBIM, 0)

			return err
		},
		"read a user without an IAM id": func() error {
			_, err := client.GetUser(t.Context(), "", 1)

			return err
		},
		"update a profile without a username": func() error {
			return client.UpdateUserProfile(t.Context(), IAMIDBIM, "", UpdateUserProfileRequest{})
		},
		"disable a user without a username": func() error {
			return client.SetUserDisabled(t.Context(), IAMIDBIM, "", true)
		},
		"delete a user without a username": func() error {
			return client.DeleteUser(t.Context(), IAMIDBIM, "")
		},
		"create a group without a name": func() error {
			_, err := client.CreateGroup(t.Context(), CreateGroupRequest{})

			return err
		},
		"read a group without an id": func() error {
			_, err := client.GetGroup(t.Context(), 0)

			return err
		},
		"delete a group without an id": func() error {
			return client.DeleteGroup(t.Context(), 0)
		},
		"add a group user without a group id": func() error {
			_, err := client.AddGroupUser(t.Context(), 0, IAMIDBIM, "jane@immuta.com")

			return err
		},
		"remove a group user without a membership id": func() error {
			return client.RemoveGroupUser(t.Context(), 2, 0)
		},
		"set an attribute with an unknown model type": func() error {
			return client.SetAttribute(t.Context(), IAMIDBIM, "project", "12", "Department", "Finance")
		},
		"set an attribute without a value": func() error {
			return client.SetAttribute(t.Context(), IAMIDBIM, ModelTypeUser, "jane@immuta.com", "Department", "")
		},
	}

	for name, call := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if err := call(); err == nil {
				t.Error("the request was accepted, want a validation error")
			}
		})
	}
}
