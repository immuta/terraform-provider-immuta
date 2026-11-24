package testutil

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// BIMServer is an in-memory implementation of the Immuta BIM endpoints used
// by the identity resources. It lets resource tests exercise the full
// Terraform lifecycle without a live Immuta tenant.
//
// It implements the documented endpoints, plus DELETE
// /bim/iam/{iamid}/user/{userid}, which the provider uses to destroy a user.
// That endpoint is undocumented; see Client.DeleteUser.
type BIMServer struct {
	server *httptest.Server

	mu          sync.Mutex
	users       map[int64]*StoredUser
	groups      map[int64]*StoredGroup
	memberships map[int64]*StoredMembership
	requests    []RecordedRequest
	nextID      int64
	clock       time.Time

	// defaultPermissions are granted to every new user on top of the ones
	// in the create request, the way an Immuta tenant with default
	// permissions configured behaves.
	defaultPermissions []string
}

// SetDefaultPermissions makes the fake API grant permissions to every new
// user, like a tenant with default permissions configured.
func (s *BIMServer) SetDefaultPermissions(permissions ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.defaultPermissions = append([]string(nil), permissions...)
}

// StoredUser is the fake API's record of a user.
type StoredUser struct {
	ID          int64
	IAMID       string
	UserID      string
	Name        string
	Permissions []string
	Profile     map[string]string
	Attributes  map[string][]string
	Disabled    bool
	CreatedAt   string
	UpdatedAt   string
}

// StoredGroup is the fake API's record of a group.
type StoredGroup struct {
	ID          int64
	IAMID       string
	Name        string
	Email       string
	Description string
	Attributes  map[string][]string
	CreatedAt   string
	UpdatedAt   string
}

// StoredMembership is the fake API's record of a user's membership of a group.
type StoredMembership struct {
	ID      int64
	GroupID int64
	UserID  int64
}

// profileFields are the profile attributes the fake API round-trips.
var profileFields = []string{"email", "phone", "sqlUser", "about", "location", "organization", "position"}

// NewBIMServer starts a fake Immuta BIM API.
func NewBIMServer() *BIMServer {
	s := &BIMServer{
		users:       map[int64]*StoredUser{},
		groups:      map[int64]*StoredGroup{},
		memberships: map[int64]*StoredMembership{},
		clock:       time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
	}
	s.server = httptest.NewServer(http.HandlerFunc(s.handle))

	return s
}

// URL is the base URL of the fake API.
func (s *BIMServer) URL() string { return s.server.URL }

// Close shuts the fake API down.
func (s *BIMServer) Close() { s.server.Close() }

// Requests returns the requests received so far.
func (s *BIMServer) Requests() []RecordedRequest {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]RecordedRequest(nil), s.requests...)
}

// UserCount returns the number of stored users.
func (s *BIMServer) UserCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.users)
}

// GroupCount returns the number of stored groups.
func (s *BIMServer) GroupCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.groups)
}

// MembershipCount returns the number of stored group memberships.
func (s *BIMServer) MembershipCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.memberships)
}

// User returns a copy of a stored user by username.
func (s *BIMServer) User(userID string) (StoredUser, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	user := s.findUserLocked(userID)
	if user == nil {
		return StoredUser{}, false
	}

	return *user, true
}

// AddUser stores a user out of band, the way Immuta holds users synchronised
// from an external identity manager, and returns its profile id.
func (s *BIMServer) AddUser(iamID, userID, name, email string) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.tick()
	user := &StoredUser{
		ID:         s.next(),
		IAMID:      iamID,
		UserID:     userID,
		Name:       name,
		Profile:    map[string]string{"email": email},
		Attributes: map[string][]string{},
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	s.users[user.ID] = user

	return user.ID
}

// Group returns a copy of a stored group by id.
func (s *BIMServer) Group(id int64) (StoredGroup, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	group, ok := s.groups[id]
	if !ok {
		return StoredGroup{}, false
	}

	return *group, true
}

// SetUserProfileField changes a profile field out of band, simulating drift.
func (s *BIMServer) SetUserProfileField(userID, field, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if user := s.findUserLocked(userID); user != nil {
		user.Profile[field] = value
		user.UpdatedAt = s.tick()
	}
}

// SetGroupDescription changes a description out of band, simulating drift.
func (s *BIMServer) SetGroupDescription(id int64, description string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if group, ok := s.groups[id]; ok {
		group.Description = description
		group.UpdatedAt = s.tick()
	}
}

// RemoveUser deletes a user out of band, simulating remote deletion.
func (s *BIMServer) RemoveUser(userID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if user := s.findUserLocked(userID); user != nil {
		s.deleteUserLocked(user)
	}
}

// RemoveGroup deletes a group out of band, simulating remote deletion.
func (s *BIMServer) RemoveGroup(id int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.deleteGroupLocked(id)
}

// RemoveMembership deletes a membership out of band, simulating remote
// deletion.
func (s *BIMServer) RemoveMembership(id int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.memberships, id)
}

// UserAttributes returns a copy of a user's stored attributes.
func (s *BIMServer) UserAttributes(userID string) map[string][]string {
	s.mu.Lock()
	defer s.mu.Unlock()

	user := s.findUserLocked(userID)
	if user == nil {
		return nil
	}

	return copyAttributes(user.Attributes)
}

// GroupAttributes returns a copy of a group's stored attributes.
func (s *BIMServer) GroupAttributes(id int64) map[string][]string {
	s.mu.Lock()
	defer s.mu.Unlock()

	group, ok := s.groups[id]
	if !ok {
		return nil
	}

	return copyAttributes(group.Attributes)
}

// RemoveUserAttribute removes an attribute out of band, simulating drift.
func (s *BIMServer) RemoveUserAttribute(userID, name, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if user := s.findUserLocked(userID); user != nil {
		removeAttribute(user.Attributes, name, value)
	}
}

// tick must be called with the lock held.
func (s *BIMServer) tick() string {
	s.clock = s.clock.Add(time.Second)

	return s.clock.Format("2006-01-02T15:04:05.000Z")
}

// next must be called with the lock held.
func (s *BIMServer) next() int64 {
	s.nextID++

	return s.nextID
}

func (s *BIMServer) findUserLocked(userID string) *StoredUser {
	for _, user := range s.users {
		if user.UserID == userID {
			return user
		}
	}

	return nil
}

func (s *BIMServer) deleteUserLocked(user *StoredUser) {
	delete(s.users, user.ID)
	for id, membership := range s.memberships {
		if membership.UserID == user.ID {
			delete(s.memberships, id)
		}
	}
}

func (s *BIMServer) deleteGroupLocked(id int64) {
	delete(s.groups, id)
	for membershipID, membership := range s.memberships {
		if membership.GroupID == id {
			delete(s.memberships, membershipID)
		}
	}
}

func (s *BIMServer) handle(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.requests = append(s.requests, RecordedRequest{Method: r.Method, Path: r.URL.EscapedPath()})
	s.mu.Unlock()

	if r.Header.Get("Authorization") != TestAPIKey {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "invalid credentials"})

		return
	}

	escaped := splitPath(r.URL.EscapedPath())
	segments := make([]string, 0, len(escaped))
	for _, segment := range escaped {
		unescaped, err := url.PathUnescape(segment)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid path"})

			return
		}
		segments = append(segments, unescaped)
	}

	if len(segments) == 0 || segments[0] != "bim" {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "unknown endpoint"})

		return
	}

	switch segments[1] {
	case "iam":
		s.handleIAM(w, r, segments)
	case "group":
		s.handleGroup(w, r, segments)
	case "user":
		if len(segments) != 2 || r.Method != http.MethodGet {
			writeJSON(w, http.StatusNotFound, map[string]string{"message": "unknown endpoint"})

			return
		}
		s.searchUsers(w, r)
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "unknown endpoint"})
	}
}

// handleIAM routes everything under /bim/iam/{iamid}.
func (s *BIMServer) handleIAM(w http.ResponseWriter, r *http.Request, segments []string) {
	if len(segments) < 4 {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "unknown endpoint"})

		return
	}

	iamID := segments[2]
	rest := segments[3:]

	// /bim/iam/{iamid}/{modelType}/{modelId}/authorizations/{name}/{value}
	if len(rest) == 5 && rest[2] == "authorizations" {
		s.handleAttribute(w, r, iamID, rest[0], rest[1], rest[3], rest[4])

		return
	}

	if rest[0] != "user" {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "unknown endpoint"})

		return
	}

	switch {
	case len(rest) == 1 && r.Method == http.MethodPost:
		s.createUser(w, r, iamID)
	case len(rest) == 2 && r.Method == http.MethodGet:
		s.getUser(w, rest[1])
	case len(rest) == 2 && r.Method == http.MethodDelete:
		s.deleteUser(w, rest[1])
	case len(rest) == 3 && rest[2] == "profile" && r.Method == http.MethodPut:
		s.updateProfile(w, r, rest[1])
	case len(rest) == 3 && rest[2] == "permissions" && r.Method == http.MethodPut:
		s.updatePermissions(w, r, rest[1])
	case len(rest) == 3 && rest[2] == "groups" && r.Method == http.MethodGet:
		s.listUserGroups(w, rest[1])
	case len(rest) == 4 && rest[2] == "disable" && r.Method == http.MethodPut:
		s.setDisabled(w, rest[1], rest[3])
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "unknown endpoint"})
	}
}

// handleGroup routes everything under /bim/group.
func (s *BIMServer) handleGroup(w http.ResponseWriter, r *http.Request, segments []string) {
	rest := segments[2:]

	switch {
	case len(rest) == 0 && r.Method == http.MethodPost:
		s.createGroup(w, r)
	case len(rest) == 1 && r.Method == http.MethodGet:
		s.getGroup(w, rest[0])
	case len(rest) == 1 && r.Method == http.MethodPut:
		s.updateGroup(w, r, rest[0])
	case len(rest) == 1 && r.Method == http.MethodDelete:
		s.deleteGroup(w, rest[0])
	case len(rest) == 2 && rest[1] == "user" && r.Method == http.MethodPost:
		s.addGroupUser(w, r, rest[0])
	case len(rest) == 3 && rest[1] == "user" && r.Method == http.MethodDelete:
		s.removeGroupUser(w, rest[0], rest[2])
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "unknown endpoint"})
	}
}

// searchUsers implements GET /bim/user: partial, case-insensitive email and
// userid matches, an iamid filter, enabled users only, sorted by name, and
// offset and size paging, in the {"count", "hits"} envelope.
func (s *BIMServer) searchUsers(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	email := strings.ToLower(query.Get("email"))
	userID := strings.ToLower(query.Get("userid"))
	iamID := query.Get("iamid")

	s.mu.Lock()
	defer s.mu.Unlock()

	var matching []*StoredUser
	for _, user := range s.users {
		if user.Disabled || (iamID != "" && user.IAMID != iamID) ||
			!strings.Contains(strings.ToLower(user.Profile["email"]), email) ||
			!strings.Contains(strings.ToLower(user.UserID), userID) {
			continue
		}
		matching = append(matching, user)
	}
	sort.Slice(matching, func(i, j int) bool {
		if matching[i].Name != matching[j].Name {
			return matching[i].Name < matching[j].Name
		}

		return matching[i].ID < matching[j].ID
	})

	offset, _ := strconv.Atoi(query.Get("offset"))
	size, err := strconv.Atoi(query.Get("size"))
	if err != nil || size < 1 {
		size = 25
	}

	hits := []map[string]any{}
	for _, user := range matching[min(offset, len(matching)):min(offset+size, len(matching))] {
		hits = append(hits, s.userPayloadLocked(user, false))
	}

	writeJSON(w, http.StatusOK, map[string]any{"count": len(matching), "hits": hits})
}

func (s *BIMServer) createUser(w http.ResponseWriter, r *http.Request, iamID string) {
	var body struct {
		IAMID   string `json:"iamid"`
		UserID  string `json:"userid"`
		Profile *struct {
			Name  string `json:"name"`
			Email string `json:"email"`
		} `json:"profile"`
		Permissions []string `json:"permissions"`
	}

	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid request body"})

		return
	}

	if body.UserID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "userid is required"})

		return
	}
	if body.IAMID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "iamid is required"})

		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.findUserLocked(body.UserID) != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"message": "a user with that username already exists"})

		return
	}

	now := s.tick()
	user := &StoredUser{
		ID:          s.next(),
		IAMID:       iamID,
		UserID:      body.UserID,
		Permissions: mergePermissions(body.Permissions, s.defaultPermissions),
		Profile:     map[string]string{},
		Attributes:  map[string][]string{},
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if body.Profile != nil {
		user.Name = body.Profile.Name
		user.Profile["email"] = body.Profile.Email
	}

	s.users[user.ID] = user

	// The documented create response wraps the user in newUser and returns the
	// numeric id at the top level.
	writeJSON(w, http.StatusOK, map[string]any{
		"newUser":     s.userPayloadLocked(user, true),
		"newUserLink": "https://example.invalid/login?token=redacted",
		"emailSent":   false,
		"emailFailed": false,
	})
}

func (s *BIMServer) getUser(w http.ResponseWriter, rawID string) {
	id, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "the user id must be numeric"})

		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	user, ok := s.users[id]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "user not found"})

		return
	}

	// The documented read response carries the numeric id on the profile only.
	writeJSON(w, http.StatusOK, s.userPayloadLocked(user, false))
}

func (s *BIMServer) deleteUser(w http.ResponseWriter, userID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	user := s.findUserLocked(userID)
	if user == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "user not found"})

		return
	}

	s.deleteUserLocked(user)
	w.WriteHeader(http.StatusNoContent)
}

func (s *BIMServer) updateProfile(w http.ResponseWriter, r *http.Request, userID string) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid request body"})

		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	user := s.findUserLocked(userID)
	if user == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "user not found"})

		return
	}

	for _, field := range profileFields {
		value, ok := body[field]
		if !ok {
			continue
		}

		if text, isString := value.(string); isString {
			user.Profile[field] = text
		}
	}
	user.UpdatedAt = s.tick()

	writeJSON(w, http.StatusOK, s.userPayloadLocked(user, true))
}

func (s *BIMServer) updatePermissions(w http.ResponseWriter, r *http.Request, userID string) {
	var permissions []string
	if err := json.NewDecoder(r.Body).Decode(&permissions); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "the payload must be an array of permissions"})

		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	user := s.findUserLocked(userID)
	if user == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "user not found"})

		return
	}

	user.Permissions = append([]string(nil), permissions...)
	user.UpdatedAt = s.tick()

	writeJSON(w, http.StatusOK, s.userPayloadLocked(user, true))
}

func (s *BIMServer) setDisabled(w http.ResponseWriter, userID, rawDisabled string) {
	disabled, err := strconv.ParseBool(rawDisabled)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "disable must be true or false"})

		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	user := s.findUserLocked(userID)
	if user == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "user not found"})

		return
	}

	user.Disabled = disabled
	user.UpdatedAt = s.tick()

	writeJSON(w, http.StatusOK, map[string]any{"userid": user.UserID, "disabled": user.Disabled})
}

func (s *BIMServer) listUserGroups(w http.ResponseWriter, userID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	user := s.findUserLocked(userID)
	if user == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "user not found"})

		return
	}

	groups := []map[string]any{}
	for _, membership := range s.memberships {
		if membership.UserID != user.ID {
			continue
		}

		group, ok := s.groups[membership.GroupID]
		if !ok {
			continue
		}

		groups = append(groups, map[string]any{
			"id":        group.ID,
			"name":      group.Name,
			"iamid":     group.IAMID,
			"groupUser": membership.ID,
		})
	}

	writeJSON(w, http.StatusOK, groups)
}

func (s *BIMServer) createGroup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IAMID       string  `json:"iamid"`
		Name        string  `json:"name"`
		Email       *string `json:"email"`
		Description *string `json:"description"`
	}

	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid request body"})

		return
	}

	if body.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "name is required"})

		return
	}
	if body.IAMID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "iamid is required"})

		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, existing := range s.groups {
		if existing.Name == body.Name {
			writeJSON(w, http.StatusConflict, map[string]string{"message": "a group with that name already exists"})

			return
		}
	}

	now := s.tick()
	group := &StoredGroup{
		ID:         s.next(),
		IAMID:      body.IAMID,
		Name:       body.Name,
		Attributes: map[string][]string{},
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if body.Email != nil {
		group.Email = *body.Email
	}
	if body.Description != nil {
		group.Description = *body.Description
	}

	s.groups[group.ID] = group

	writeJSON(w, http.StatusOK, groupPayload(group))
}

func (s *BIMServer) getGroup(w http.ResponseWriter, rawID string) {
	id, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "the group id must be numeric"})

		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	group, ok := s.groups[id]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "group not found"})

		return
	}

	writeJSON(w, http.StatusOK, groupPayload(group))
}

func (s *BIMServer) updateGroup(w http.ResponseWriter, r *http.Request, rawID string) {
	id, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "the group id must be numeric"})

		return
	}

	var body struct {
		Name        *string `json:"name"`
		Email       *string `json:"email"`
		Description *string `json:"description"`
	}

	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid request body"})

		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	group, ok := s.groups[id]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "group not found"})

		return
	}

	if body.Name != nil {
		group.Name = *body.Name
	}
	if body.Email != nil {
		group.Email = *body.Email
	}
	if body.Description != nil {
		group.Description = *body.Description
	}
	group.UpdatedAt = s.tick()

	writeJSON(w, http.StatusOK, groupPayload(group))
}

func (s *BIMServer) deleteGroup(w http.ResponseWriter, rawID string) {
	id, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "the group id must be numeric"})

		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.groups[id]; !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "group not found"})

		return
	}

	s.deleteGroupLocked(id)
	w.WriteHeader(http.StatusNoContent)
}

func (s *BIMServer) addGroupUser(w http.ResponseWriter, r *http.Request, rawGroupID string) {
	groupID, err := strconv.ParseInt(rawGroupID, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "the group id must be numeric"})

		return
	}

	var body struct {
		UserID string `json:"userid"`
		IAMID  string `json:"iamid"`
	}

	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid request body"})

		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	group, ok := s.groups[groupID]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "group not found"})

		return
	}

	user := s.findUserLocked(body.UserID)
	if user == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "user not found"})

		return
	}

	for _, membership := range s.memberships {
		if membership.GroupID == group.ID && membership.UserID == user.ID {
			writeJSON(w, http.StatusConflict, map[string]string{"message": "the user is already a member of that group"})

			return
		}
	}

	now := s.tick()
	membership := &StoredMembership{ID: s.next(), GroupID: group.ID, UserID: user.ID}
	s.memberships[membership.ID] = membership

	writeJSON(w, http.StatusOK, map[string]any{
		"id":        membership.ID,
		"group":     membership.GroupID,
		"profile":   membership.UserID,
		"createdAt": now,
		"updatedAt": now,
	})
}

func (s *BIMServer) removeGroupUser(w http.ResponseWriter, rawGroupID, rawMembershipID string) {
	groupID, groupErr := strconv.ParseInt(rawGroupID, 10, 64)
	membershipID, membershipErr := strconv.ParseInt(rawMembershipID, 10, 64)
	if groupErr != nil || membershipErr != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "the group and membership ids must be numeric"})

		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	membership, ok := s.memberships[membershipID]
	if !ok || membership.GroupID != groupID {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "group membership not found"})

		return
	}

	delete(s.memberships, membershipID)
	w.WriteHeader(http.StatusNoContent)
}

func (s *BIMServer) handleAttribute(w http.ResponseWriter, r *http.Request, iamID, modelType, modelID, name, value string) {
	if r.Method != http.MethodPut && r.Method != http.MethodDelete {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "unknown endpoint"})

		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var attributes map[string][]string

	switch modelType {
	case "user":
		user := s.findUserLocked(modelID)
		if user == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"message": "user not found"})

			return
		}
		attributes = user.Attributes
	case "group":
		groupID, err := strconv.ParseInt(modelID, 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": "the group id must be numeric"})

			return
		}

		group, ok := s.groups[groupID]
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{"message": "group not found"})

			return
		}
		attributes = group.Attributes
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "the model type must be user or group"})

		return
	}

	if r.Method == http.MethodPut {
		if !containsString(attributes[name], value) {
			attributes[name] = append(attributes[name], value)
		}
	} else {
		removeAttribute(attributes, name, value)
	}

	writeJSON(w, http.StatusOK, map[string]any{"iamid": iamID, "authorizations": attributes})
}

// userPayloadLocked renders the documented user object. When withTopLevelID
// is false the numeric id is carried on the profile only, which is what the
// documented GET response looks like.
func (s *BIMServer) userPayloadLocked(user *StoredUser, withTopLevelID bool) map[string]any {
	profile := map[string]any{
		"id":        user.ID,
		"name":      user.Name,
		"createdAt": user.CreatedAt,
		"updatedAt": user.UpdatedAt,
	}
	for _, field := range profileFields {
		if value, ok := user.Profile[field]; ok && value != "" {
			profile[field] = value
		} else {
			profile[field] = nil
		}
	}

	permissions := user.Permissions
	if permissions == nil {
		permissions = []string{}
	}

	payload := map[string]any{
		"iamid":             user.IAMID,
		"userid":            user.UserID,
		"permissions":       permissions,
		"profile":           profile,
		"disabled":          user.Disabled,
		"authorizations":    copyAttributes(user.Attributes),
		"bimAuthorizations": copyAttributes(user.Attributes),
		"createdAt":         user.CreatedAt,
		"updatedAt":         user.UpdatedAt,
	}
	if withTopLevelID {
		payload["id"] = user.ID
	}

	return payload
}

func groupPayload(group *StoredGroup) map[string]any {
	return map[string]any{
		"id":             group.ID,
		"iamid":          group.IAMID,
		"name":           group.Name,
		"email":          emptyToNil(group.Email),
		"description":    emptyToNil(group.Description),
		"authorizations": copyAttributes(group.Attributes),
		"createdAt":      group.CreatedAt,
		"updatedAt":      group.UpdatedAt,
	}
}

func copyAttributes(attributes map[string][]string) map[string][]string {
	copied := make(map[string][]string, len(attributes))
	for name, values := range attributes {
		copied[name] = append([]string(nil), values...)
	}

	return copied
}

func removeAttribute(attributes map[string][]string, name, value string) {
	remaining := attributes[name][:0:0]
	for _, candidate := range attributes[name] {
		if candidate != value {
			remaining = append(remaining, candidate)
		}
	}

	if len(remaining) == 0 {
		delete(attributes, name)

		return
	}

	attributes[name] = remaining
}

func containsString(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}

	return false
}

// mergePermissions returns the requested permissions plus the defaults,
// without duplicates.
func mergePermissions(requested, defaults []string) []string {
	merged := append([]string(nil), requested...)
	for _, permission := range defaults {
		if !containsString(merged, permission) {
			merged = append(merged, permission)
		}
	}

	return merged
}
