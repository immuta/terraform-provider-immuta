package immutaapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// IAMIDBIM is the id of the identity manager built into Immuta. It is the
// only IAM the documented create endpoints write to.
const IAMIDBIM = "bim"

// Documented model types for the attribute (authorizations) endpoints.
const (
	ModelTypeUser  = "user"
	ModelTypeGroup = "group"
)

// Authorizations is the documented attribute map held against a user or a
// group: attribute name to the values assigned for that name.
//
// Immuta returns this field as an object, as null, or omits it entirely, so
// decoding is deliberately tolerant. Values that are not strings or arrays of
// strings are skipped rather than failing the whole read, which would turn an
// unexpected attribute somewhere else in the tenant into a broken refresh.
type Authorizations map[string][]string

// UnmarshalJSON implements json.Unmarshaler.
func (a *Authorizations) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		*a = nil

		return nil
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &raw); err != nil {
		return fmt.Errorf("decoding authorizations: %w", err)
	}

	decoded := make(Authorizations, len(raw))
	for name, value := range raw {
		var values []string
		if err := json.Unmarshal(value, &values); err == nil {
			decoded[name] = values

			continue
		}

		var single string
		if err := json.Unmarshal(value, &single); err == nil {
			decoded[name] = []string{single}
		}
	}

	*a = decoded

	return nil
}

// Has reports whether the named attribute holds the given value.
func (a Authorizations) Has(name, value string) bool {
	for _, candidate := range a[name] {
		if candidate == value {
			return true
		}
	}

	return false
}

// UserProfile is the documented user profile object.
type UserProfile struct {
	ID           *int64 `json:"id,omitempty"`
	Name         string `json:"name,omitempty"`
	Email        string `json:"email,omitempty"`
	Phone        string `json:"phone,omitempty"`
	About        string `json:"about,omitempty"`
	Location     string `json:"location,omitempty"`
	Organization string `json:"organization,omitempty"`
	Position     string `json:"position,omitempty"`
	SQLUser      string `json:"sqlUser,omitempty"`
}

// User is the documented user object.
type User struct {
	ID          int64        `json:"id,omitempty"`
	IAMID       string       `json:"iamid,omitempty"`
	UserID      string       `json:"userid,omitempty"`
	Permissions []string     `json:"permissions,omitempty"`
	Profile     *UserProfile `json:"profile,omitempty"`
	Disabled    bool         `json:"disabled"`

	// Authorizations is the user's combined attribute view. BIMAuthorizations
	// holds the attributes assigned to the user's BIM profile, which is what
	// the documented attribute endpoints write to.
	Authorizations    Authorizations `json:"authorizations,omitempty"`
	BIMAuthorizations Authorizations `json:"bimAuthorizations,omitempty"`

	CreatedAt string `json:"createdAt,omitempty"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

// UnmarshalJSON implements json.Unmarshaler.
//
// Two documented shapes have to be handled. The detailed user object nests a
// profile object and carries the numeric id on the profile, while the plain
// user object returns the profile as its numeric id. Both are reduced to the
// same Go value.
func (u *User) UnmarshalJSON(data []byte) error {
	type userAlias User

	var decoded struct {
		userAlias
		Profile json.RawMessage `json:"profile"`
	}

	if err := json.Unmarshal(data, &decoded); err != nil {
		return fmt.Errorf("decoding user: %w", err)
	}

	*u = User(decoded.userAlias)
	u.Profile = nil

	profile := bytes.TrimSpace(decoded.Profile)
	switch {
	case len(profile) == 0 || bytes.Equal(profile, []byte("null")):
	case bytes.HasPrefix(profile, []byte("{")):
		var parsed UserProfile
		if err := json.Unmarshal(profile, &parsed); err != nil {
			return fmt.Errorf("decoding user profile: %w", err)
		}
		u.Profile = &parsed
	default:
		var profileID int64
		if err := json.Unmarshal(profile, &profileID); err != nil {
			return fmt.Errorf("decoding user profile: %w", err)
		}
		u.Profile = &UserProfile{ID: &profileID}
	}

	// GET /bim/iam/{iamid}/user/{id} returns the numeric id on the profile
	// rather than at the top level.
	if u.ID == 0 && u.Profile != nil && u.Profile.ID != nil {
		u.ID = *u.Profile.ID
	}

	return nil
}

// CreateUserProfile is the documented profile payload accepted by
// POST /bim/iam/bim/user. Only name and email are documented there; the other
// profile fields are set with UpdateUserProfile.
type CreateUserProfile struct {
	Name  string `json:"name,omitempty"`
	Email string `json:"email,omitempty"`
}

// CreateUserRequest is the documented POST /bim/iam/bim/user body.
type CreateUserRequest struct {
	IAMID       string             `json:"iamid"`
	UserID      string             `json:"userid"`
	Profile     *CreateUserProfile `json:"profile,omitempty"`
	Permissions []string           `json:"permissions"`
}

// UpdateUserProfileRequest is the documented PUT
// /bim/iam/{iamid}/user/{userid}/profile body. Every field is optional so
// that only changed values are sent.
type UpdateUserProfileRequest struct {
	Email        *string `json:"email,omitempty"`
	Phone        *string `json:"phone,omitempty"`
	SQLUser      *string `json:"sqlUser,omitempty"`
	About        *string `json:"about,omitempty"`
	Location     *string `json:"location,omitempty"`
	Organization *string `json:"organization,omitempty"`
	Position     *string `json:"position,omitempty"`
}

// IsEmpty reports whether the request would not change anything.
func (r UpdateUserProfileRequest) IsEmpty() bool {
	return r.Email == nil && r.Phone == nil && r.SQLUser == nil && r.About == nil &&
		r.Location == nil && r.Organization == nil && r.Position == nil
}

// CreateUser creates a BIM user with POST /bim/iam/bim/user.
func (c *Client) CreateUser(ctx context.Context, req CreateUserRequest) (*User, error) {
	if strings.TrimSpace(req.UserID) == "" {
		return nil, errors.New("a username is required")
	}
	if strings.TrimSpace(req.IAMID) == "" {
		req.IAMID = IAMIDBIM
	}
	if req.Permissions == nil {
		req.Permissions = []string{}
	}

	body, err := c.do(ctx, request{
		method:   http.MethodPost,
		segments: []string{"bim", "iam", IAMIDBIM, "user"},
		body:     req,
	})
	if err != nil {
		return nil, err
	}

	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil, errEmptyBody
	}

	// The documented response wraps the created user in newUser. Only that
	// field is read: newUserLink contains a single use login token and must
	// never reach Terraform state or logs.
	var envelope struct {
		NewUser json.RawMessage `json:"newUser"`
	}
	if err := json.Unmarshal(trimmed, &envelope); err != nil {
		return nil, fmt.Errorf("decoding create user response: %w", err)
	}

	payload := bytes.TrimSpace(envelope.NewUser)
	if len(payload) == 0 {
		payload = trimmed
	}

	return decodeUser(payload)
}

// GetUser reads a user with GET /bim/iam/{iamid}/user/{id}. The documented
// path parameter is the numeric user id, not the username.
func (c *Client) GetUser(ctx context.Context, iamID string, id int64) (*User, error) {
	if err := validateIAMID(iamID); err != nil {
		return nil, err
	}
	if id <= 0 {
		return nil, errors.New("a user id is required")
	}

	body, err := c.do(ctx, request{
		method:   http.MethodGet,
		segments: []string{"bim", "iam", iamID, "user", strconv.FormatInt(id, 10)},
	})
	if err != nil {
		return nil, err
	}

	return decodeUser(body)
}

// UpdateUserProfile updates a user profile with PUT
// /bim/iam/{iamid}/user/{userid}/profile.
func (c *Client) UpdateUserProfile(ctx context.Context, iamID, userID string, req UpdateUserProfileRequest) error {
	if err := validateUserRef(iamID, userID); err != nil {
		return err
	}

	_, err := c.do(ctx, request{
		method:   http.MethodPut,
		segments: []string{"bim", "iam", iamID, "user", userID, "profile"},
		body:     req,
	})

	return err
}

// UpdateUserPermissions replaces a user's permissions with PUT
// /bim/iam/{iamid}/user/{userid}/permissions. The documented payload is a
// comprehensive list: anything omitted is removed.
func (c *Client) UpdateUserPermissions(ctx context.Context, iamID, userID string, permissions []string) error {
	if err := validateUserRef(iamID, userID); err != nil {
		return err
	}
	if permissions == nil {
		permissions = []string{}
	}

	_, err := c.do(ctx, request{
		method:   http.MethodPut,
		segments: []string{"bim", "iam", iamID, "user", userID, "permissions"},
		body:     permissions,
	})

	return err
}

// SetUserDisabled disables or enables a user with PUT
// /bim/iam/{iamid}/user/{userid}/disable/{disable}.
func (c *Client) SetUserDisabled(ctx context.Context, iamID, userID string, disabled bool) error {
	if err := validateUserRef(iamID, userID); err != nil {
		return err
	}

	_, err := c.do(ctx, request{
		method:   http.MethodPut,
		segments: []string{"bim", "iam", iamID, "user", userID, "disable", strconv.FormatBool(disabled)},
	})

	return err
}

// DeleteUser permanently deletes a user with DELETE
// /bim/iam/{iamid}/user/{userid}.
//
// This endpoint is NOT part of the published Immuta API reference. It is the
// request the Immuta UI issues for "permanently delete user", which the
// documentation describes only as a UI action. Every other call in this
// package is documented; this one is the single deliberate exception, made so
// that destroying an immuta_user actually removes the user instead of only
// disabling it. See docs/resources/user.md.
//
// Because it is undocumented it carries no compatibility guarantee. Do not
// add further undocumented calls without the same explicit decision.
func (c *Client) DeleteUser(ctx context.Context, iamID, userID string) error {
	if err := validateUserRef(iamID, userID); err != nil {
		return err
	}

	_, err := c.do(ctx, request{
		method:   http.MethodDelete,
		segments: []string{"bim", "iam", iamID, "user", userID},
	})

	return err
}

// Group is the documented group object.
type Group struct {
	ID             int64          `json:"id"`
	IAMID          string         `json:"iamid,omitempty"`
	Name           string         `json:"name"`
	Email          string         `json:"email,omitempty"`
	Description    string         `json:"description,omitempty"`
	Authorizations Authorizations `json:"authorizations,omitempty"`
	CreatedAt      string         `json:"createdAt,omitempty"`
	UpdatedAt      string         `json:"updatedAt,omitempty"`
}

// CreateGroupRequest is the documented POST /bim/group body.
type CreateGroupRequest struct {
	IAMID       string  `json:"iamid"`
	Name        string  `json:"name"`
	Email       *string `json:"email,omitempty"`
	Description *string `json:"description,omitempty"`
}

// UpdateGroupRequest is the documented PUT /bim/group/{groupId} body.
type UpdateGroupRequest struct {
	Name        *string `json:"name,omitempty"`
	Email       *string `json:"email,omitempty"`
	Description *string `json:"description,omitempty"`
}

// IsEmpty reports whether the request would not change anything.
func (r UpdateGroupRequest) IsEmpty() bool {
	return r.Name == nil && r.Email == nil && r.Description == nil
}

// CreateGroup creates a group with POST /bim/group.
func (c *Client) CreateGroup(ctx context.Context, req CreateGroupRequest) (*Group, error) {
	if strings.TrimSpace(req.Name) == "" {
		return nil, errors.New("a group name is required")
	}
	if strings.TrimSpace(req.IAMID) == "" {
		req.IAMID = IAMIDBIM
	}

	body, err := c.do(ctx, request{
		method:   http.MethodPost,
		segments: []string{"bim", "group"},
		body:     req,
	})
	if err != nil {
		return nil, err
	}

	return decodeGroup(body)
}

// GetGroup reads a group with GET /bim/group/{groupId}.
func (c *Client) GetGroup(ctx context.Context, groupID int64) (*Group, error) {
	if groupID <= 0 {
		return nil, errors.New("a group id is required")
	}

	body, err := c.do(ctx, request{
		method:   http.MethodGet,
		segments: []string{"bim", "group", strconv.FormatInt(groupID, 10)},
	})
	if err != nil {
		return nil, err
	}

	return decodeGroup(body)
}

// UpdateGroup updates a group with PUT /bim/group/{groupId}.
func (c *Client) UpdateGroup(ctx context.Context, groupID int64, req UpdateGroupRequest) error {
	if groupID <= 0 {
		return errors.New("a group id is required")
	}

	_, err := c.do(ctx, request{
		method:   http.MethodPut,
		segments: []string{"bim", "group", strconv.FormatInt(groupID, 10)},
		body:     req,
	})

	return err
}

// DeleteGroup deletes a group with DELETE /bim/group/{groupId}.
func (c *Client) DeleteGroup(ctx context.Context, groupID int64) error {
	if groupID <= 0 {
		return errors.New("a group id is required")
	}

	_, err := c.do(ctx, request{
		method:   http.MethodDelete,
		segments: []string{"bim", "group", strconv.FormatInt(groupID, 10)},
	})

	return err
}

// GroupMembership is the documented response of POST /bim/group/{groupId}/user.
type GroupMembership struct {
	// ID is the unique membership id, the groupuserid used to remove the user
	// from the group again.
	ID int64 `json:"id"`
	// Group is the group id, Profile the numeric user id.
	Group   int64 `json:"group"`
	Profile int64 `json:"profile"`

	CreatedAt string `json:"createdAt,omitempty"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

// UserGroup is an entry of GET /bim/iam/{iamid}/user/{userid}/groups.
type UserGroup struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	IAMID string `json:"iamid"`
	// GroupUser is the unique membership id for this user in this group.
	GroupUser int64 `json:"groupUser"`
}

// AddGroupUser adds a user to a group with POST /bim/group/{groupId}/user.
func (c *Client) AddGroupUser(ctx context.Context, groupID int64, iamID, userID string) (*GroupMembership, error) {
	if groupID <= 0 {
		return nil, errors.New("a group id is required")
	}
	if err := validateUserRef(iamID, userID); err != nil {
		return nil, err
	}

	body, err := c.do(ctx, request{
		method:   http.MethodPost,
		segments: []string{"bim", "group", strconv.FormatInt(groupID, 10), "user"},
		body: map[string]string{
			"userid": userID,
			"iamid":  iamID,
		},
	})
	if err != nil {
		return nil, err
	}

	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil, errEmptyBody
	}

	var membership GroupMembership
	if err := json.Unmarshal(trimmed, &membership); err != nil {
		return nil, fmt.Errorf("decoding add group user response: %w", err)
	}

	if membership.ID == 0 {
		return nil, errors.New("decoding add group user response: the API response did not contain a membership id")
	}

	return &membership, nil
}

// ListUserGroups lists a user's groups with GET
// /bim/iam/{iamid}/user/{userid}/groups. It is the only documented way to
// recover the membership id of an existing membership.
func (c *Client) ListUserGroups(ctx context.Context, iamID, userID string) ([]UserGroup, error) {
	if err := validateUserRef(iamID, userID); err != nil {
		return nil, err
	}

	body, err := c.do(ctx, request{
		method:   http.MethodGet,
		segments: []string{"bim", "iam", iamID, "user", userID, "groups"},
	})
	if err != nil {
		return nil, err
	}

	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil, errEmptyBody
	}

	var groups []UserGroup
	if err := json.Unmarshal(trimmed, &groups); err != nil {
		return nil, fmt.Errorf("decoding user groups response: %w", err)
	}

	return groups, nil
}

// FindUserGroup returns the user's membership of a single group. It returns
// an error matching ErrNotFound when the user is not a member.
func (c *Client) FindUserGroup(ctx context.Context, iamID, userID string, groupID int64) (*UserGroup, error) {
	groups, err := c.ListUserGroups(ctx, iamID, userID)
	if err != nil {
		return nil, err
	}

	for _, group := range groups {
		if group.ID == groupID {
			return &group, nil
		}
	}

	return nil, ErrNotFound
}

// RemoveGroupUser removes a user from a group with DELETE
// /bim/group/{groupId}/user/{groupuserid}.
func (c *Client) RemoveGroupUser(ctx context.Context, groupID, membershipID int64) error {
	if groupID <= 0 {
		return errors.New("a group id is required")
	}
	if membershipID <= 0 {
		return errors.New("a group membership id is required")
	}

	_, err := c.do(ctx, request{
		method:   http.MethodDelete,
		segments: []string{"bim", "group", strconv.FormatInt(groupID, 10), "user", strconv.FormatInt(membershipID, 10)},
	})

	return err
}

// SetAttribute adds an attribute to a user or a group with PUT
// /bim/iam/{iamid}/{modelType}/{modelId}/authorizations/{name}/{value}.
//
// modelID is the username for a user and the numeric group id for a group.
func (c *Client) SetAttribute(ctx context.Context, iamID, modelType, modelID, name, value string) error {
	if err := validateAttributeRef(iamID, modelType, modelID, name, value); err != nil {
		return err
	}

	_, err := c.do(ctx, request{
		method:   http.MethodPut,
		segments: []string{"bim", "iam", iamID, modelType, modelID, "authorizations", name, value},
	})

	return err
}

// DeleteAttribute removes an attribute from a user or a group with DELETE
// /bim/iam/{iamid}/{modelType}/{modelId}/authorizations/{key}/{value}.
func (c *Client) DeleteAttribute(ctx context.Context, iamID, modelType, modelID, name, value string) error {
	if err := validateAttributeRef(iamID, modelType, modelID, name, value); err != nil {
		return err
	}

	_, err := c.do(ctx, request{
		method:   http.MethodDelete,
		segments: []string{"bim", "iam", iamID, modelType, modelID, "authorizations", name, value},
	})

	return err
}

func validateIAMID(iamID string) error {
	if strings.TrimSpace(iamID) == "" {
		return errors.New("an IAM id is required")
	}

	return nil
}

func validateUserRef(iamID, userID string) error {
	if err := validateIAMID(iamID); err != nil {
		return err
	}
	if strings.TrimSpace(userID) == "" {
		return errors.New("a username is required")
	}

	return nil
}

func validateAttributeRef(iamID, modelType, modelID, name, value string) error {
	if err := validateIAMID(iamID); err != nil {
		return err
	}

	switch modelType {
	case ModelTypeUser, ModelTypeGroup:
	default:
		return fmt.Errorf("model type must be %q or %q, got %q", ModelTypeUser, ModelTypeGroup, modelType)
	}

	if strings.TrimSpace(modelID) == "" {
		return errors.New("a user or group id is required")
	}
	if strings.TrimSpace(name) == "" {
		return errors.New("an attribute name is required")
	}
	if strings.TrimSpace(value) == "" {
		return errors.New("an attribute value is required")
	}

	return nil
}

func decodeUser(body []byte) (*User, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil, errEmptyBody
	}

	var user User
	if err := json.Unmarshal(trimmed, &user); err != nil {
		return nil, fmt.Errorf("decoding user response: %w", err)
	}

	if user.ID == 0 {
		return nil, errors.New("decoding user response: the API response did not contain a user id")
	}

	return &user, nil
}

func decodeGroup(body []byte) (*Group, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil, errEmptyBody
	}

	var group Group
	if err := json.Unmarshal(trimmed, &group); err != nil {
		return nil, fmt.Errorf("decoding group response: %w", err)
	}

	if group.ID == 0 {
		return nil, errors.New("decoding group response: the API response did not contain a group id")
	}

	return &group, nil
}
