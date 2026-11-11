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
//	PUT /api/connections/{connectionKey}/dataObjects/{securableId}/accessGroup
//	GET /api/connections/{connectionKey}/dataObjects/{securableId}/accessGroup
//
// It is not in the published connections API reference yet. The bind body,
// {"action": "bind", "identityGroupId": ...}, comes from the UAC Custom
// connection guidance. The unbind body and the GET response shape are
// assumptions, kept here so that they are easy to correct.
const (
	accessGroupActionBind   = "bind"
	accessGroupActionUnbind = "unbind"
)

// BindAccessGroup binds an identity group data object to a Custom securable.
// The group must not be bound to another securable.
func (c *Client) BindAccessGroup(ctx context.Context, connectionKey, securableID, identityGroupID string) error {
	return c.putAccessGroup(ctx, connectionKey, securableID, accessGroupActionBind, identityGroupID)
}

// UnbindAccessGroup removes the binding of an identity group from a Custom
// securable.
func (c *Client) UnbindAccessGroup(ctx context.Context, connectionKey, securableID, identityGroupID string) error {
	return c.putAccessGroup(ctx, connectionKey, securableID, accessGroupActionUnbind, identityGroupID)
}

func (c *Client) putAccessGroup(ctx context.Context, connectionKey, securableID, action, identityGroupID string) error {
	if err := validateDataObjectRef(connectionKey, securableID); err != nil {
		return err
	}
	if strings.TrimSpace(identityGroupID) == "" {
		return errors.New("an identity group data object id is required")
	}

	_, err := c.do(ctx, request{
		method:   http.MethodPut,
		segments: []string{"api", "connections", connectionKey, "dataObjects", securableID, "accessGroup"},
		body:     map[string]any{"action": action, "identityGroupId": identityGroupID},
	})

	return err
}

// GetAccessGroup returns the id of the identity group data object bound to a
// Custom securable, or "" when none is bound. It returns an error matching
// ErrNotFound when the securable does not exist.
func (c *Client) GetAccessGroup(ctx context.Context, connectionKey, securableID string) (string, error) {
	if err := validateDataObjectRef(connectionKey, securableID); err != nil {
		return "", err
	}

	body, err := c.do(ctx, request{
		method:   http.MethodGet,
		segments: []string{"api", "connections", connectionKey, "dataObjects", securableID, "accessGroup"},
	})
	if err != nil {
		return "", err
	}

	return decodeAccessGroup(body)
}

// decodeAccessGroup reads the bound identity group id from either
// {"identityGroupId": "..."} or {"identityGroup": {"id": "..."}}. An empty
// body, or either field set to null, means no group is bound. Any other
// response is an error rather than "unbound", so that a response this
// client does not understand never makes Terraform forget a binding.
func decodeAccessGroup(body []byte) (string, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return "", nil
	}

	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &decoded); err != nil {
		return "", fmt.Errorf("decoding access group: %w", err)
	}

	if raw, ok := decoded["identityGroupId"]; ok {
		var id *string
		if err := json.Unmarshal(raw, &id); err != nil {
			return "", fmt.Errorf("decoding access group: identityGroupId: %w", err)
		}
		if id == nil {
			return "", nil
		}

		return *id, nil
	}

	if raw, ok := decoded["identityGroup"]; ok {
		var group *struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(raw, &group); err != nil {
			return "", fmt.Errorf("decoding access group: identityGroup: %w", err)
		}
		if group == nil {
			return "", nil
		}
		if group.ID == "" {
			return "", errors.New("decoding access group: identityGroup has no id")
		}

		return group.ID, nil
	}

	return "", errors.New("decoding access group: the API response has neither identityGroupId nor identityGroup")
}
