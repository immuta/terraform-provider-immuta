package immutaapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// The access group endpoint binds an identity group, crawled by an Entra or
// Okta connection, to a Custom securable:
//
//	PUT    /api/connections/{connectionKey}/dataObjects/{securableId}/accessGroup
//	GET    /api/connections/{connectionKey}/dataObjects/{securableId}/accessGroup
//	DELETE /api/connections/{connectionKey}/dataObjects/{securableId}/accessGroup
//
// It is not in the published connections API reference yet. PUT with the
// "bind" action binds a group and DELETE unbinds it. While a group is bound,
// Immuta refuses to delete the securable.
const accessGroupActionBind = "bind"

// AccessGroup is the identity group bound to a Custom securable, as
// GET .../accessGroup returns it:
//
//	{"state": "LIVE", "identityConnectionKey": "entra-prod", "identityGroupId": "..."}
//
// The zero value means no group is bound.
type AccessGroup struct {
	IdentityGroupID       string `json:"identityGroupId"`
	IdentityConnectionKey string `json:"identityConnectionKey"`
	State                 string `json:"state"`
}

// Bound reports whether a group is bound.
func (g AccessGroup) Bound() bool {
	return g.IdentityGroupID != ""
}

// String describes the binding for messages.
func (g AccessGroup) String() string {
	if !g.Bound() {
		return "no identity group"
	}

	description := "the identity group " + g.IdentityGroupID
	if g.IdentityConnectionKey != "" {
		description += " of the connection " + g.IdentityConnectionKey
	}
	if g.State != "" {
		description += " (state " + g.State + ")"
	}

	return description
}

// BindAccessGroup binds an identity group data object to a Custom securable.
// The group must not be bound to another securable.
func (c *Client) BindAccessGroup(ctx context.Context, connectionKey, securableID, identityGroupID string) error {
	if err := validateDataObjectRef(connectionKey, securableID); err != nil {
		return err
	}
	if strings.TrimSpace(identityGroupID) == "" {
		return errors.New("an identity group data object id is required")
	}

	_, err := c.do(ctx, request{
		method:   http.MethodPut,
		segments: []string{"api", "connections", connectionKey, "dataObjects", securableID, "accessGroup"},
		body:     map[string]any{"action": accessGroupActionBind, "identityGroupId": identityGroupID},
	})

	return err
}

// UnbindAccessGroup removes the identity group bound to a Custom securable.
// It returns an error matching ErrNotFound when Immuta reports the securable,
// or its binding, as not found.
func (c *Client) UnbindAccessGroup(ctx context.Context, connectionKey, securableID string) error {
	if err := validateDataObjectRef(connectionKey, securableID); err != nil {
		return err
	}

	_, err := c.do(ctx, request{
		method:   http.MethodDelete,
		segments: []string{"api", "connections", connectionKey, "dataObjects", securableID, "accessGroup"},
	})

	return err
}

// GetAccessGroup returns the identity group bound to a Custom securable; its
// zero value when none is bound. It returns an error matching ErrNotFound
// when the securable does not exist.
func (c *Client) GetAccessGroup(ctx context.Context, connectionKey, securableID string) (AccessGroup, error) {
	if err := validateDataObjectRef(connectionKey, securableID); err != nil {
		return AccessGroup{}, err
	}

	body, err := c.do(ctx, request{
		method:   http.MethodGet,
		segments: []string{"api", "connections", connectionKey, "dataObjects", securableID, "accessGroup"},
	})
	if err != nil {
		return AccessGroup{}, err
	}

	return decodeAccessGroup(body)
}

// decodeAccessGroup reads an access group response. An empty body, null, or
// a null identityGroupId means no group is bound. A response without an
// identityGroupId field is an error rather than "unbound", so that a
// response this client does not understand never makes Terraform forget a
// binding.
func decodeAccessGroup(body []byte) (AccessGroup, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return AccessGroup{}, nil
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &fields); err != nil {
		return AccessGroup{}, fmt.Errorf("decoding access group: %w", err)
	}
	if _, ok := fields["identityGroupId"]; !ok {
		return AccessGroup{}, errors.New("decoding access group: the API response has no identityGroupId")
	}

	var group struct {
		IdentityGroupID       *string `json:"identityGroupId"`
		IdentityConnectionKey *string `json:"identityConnectionKey"`
		State                 *string `json:"state"`
	}
	if err := json.Unmarshal(trimmed, &group); err != nil {
		return AccessGroup{}, fmt.Errorf("decoding access group: %w", err)
	}
	if group.IdentityGroupID == nil || *group.IdentityGroupID == "" {
		return AccessGroup{}, nil
	}

	result := AccessGroup{IdentityGroupID: *group.IdentityGroupID}
	if group.IdentityConnectionKey != nil {
		result.IdentityConnectionKey = *group.IdentityConnectionKey
	}
	if group.State != nil {
		result.State = *group.State
	}

	return result, nil
}
