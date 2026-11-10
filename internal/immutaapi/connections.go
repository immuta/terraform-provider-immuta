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

// Connection states documented by the connections API.
const (
	ConnectionStateActivating = "ACTIVATING"
	ConnectionStateActive     = "ACTIVE"
	ConnectionStateUpdating   = "UPDATING"
	ConnectionStateUnhealthy  = "UNHEALTHY"
	ConnectionStateDeleting   = "DELETING"
)

// Connection is a connection returned by the connections API. Raw holds the
// whole response, with secrets redacted by Immuta, so that callers can
// compare any field without the client modelling every technology.
type Connection struct {
	ConnectionKey string
	Technology    string
	State         string
	Raw           map[string]any
}

// ConnectionDeletion is the documented response of a connection delete.
type ConnectionDeletion struct {
	TaskID string `json:"taskId"`
	// Teardown is returned for some technologies. Its format is not
	// documented, so it is kept as raw JSON.
	Teardown json.RawMessage `json:"teardown"`
}

// CreateConnection registers a connection with POST /api/connections. The
// body is the documented registration payload, including the technology,
// the connectionKey and the credential.
func (c *Client) CreateConnection(ctx context.Context, body map[string]any) (*Connection, error) {
	key, _ := body["connectionKey"].(string)
	if err := validateConnectionKey(key); err != nil {
		return nil, err
	}
	if technology, _ := body["technology"].(string); strings.TrimSpace(technology) == "" {
		return nil, errors.New("a connection technology is required")
	}

	response, err := c.do(ctx, request{
		method:   http.MethodPost,
		segments: []string{"api", "connections"},
		body:     body,
	})
	if err != nil {
		return nil, err
	}

	connection, err := decodeConnection(response)
	if errors.Is(err, errEmptyBody) {
		return c.GetConnection(ctx, key)
	}

	return connection, err
}

// GetConnection reads a connection with GET /api/connections/{connectionKey}.
// It returns an error matching ErrNotFound when the connection does not
// exist.
func (c *Client) GetConnection(ctx context.Context, key string) (*Connection, error) {
	if err := validateConnectionKey(key); err != nil {
		return nil, err
	}

	response, err := c.do(ctx, request{
		method:   http.MethodGet,
		segments: []string{"api", "connections", key},
	})
	if err != nil {
		return nil, err
	}

	return decodeConnection(response)
}

// UpdateConnection partially updates a connection with
// PATCH /api/connections/{connectionKey}. Only the fields in body change.
func (c *Client) UpdateConnection(ctx context.Context, key string, body map[string]any) (*Connection, error) {
	if err := validateConnectionKey(key); err != nil {
		return nil, err
	}

	response, err := c.do(ctx, request{
		method:   http.MethodPatch,
		segments: []string{"api", "connections", key},
		body:     body,
	})
	if err != nil {
		return nil, err
	}

	connection, err := decodeConnection(response)
	if errors.Is(err, errEmptyBody) {
		return c.GetConnection(ctx, key)
	}

	return connection, err
}

// DeleteConnection deletes a connection, and all of its data objects, with
// DELETE /api/connections/{connectionKey}. Immuta deletes it asynchronously.
func (c *Client) DeleteConnection(ctx context.Context, key string) (*ConnectionDeletion, error) {
	if err := validateConnectionKey(key); err != nil {
		return nil, err
	}

	response, err := c.do(ctx, request{
		method:   http.MethodDelete,
		segments: []string{"api", "connections", key},
	})
	if err != nil {
		return nil, err
	}

	var deletion ConnectionDeletion
	if trimmed := bytes.TrimSpace(response); len(trimmed) > 0 {
		if err := json.Unmarshal(trimmed, &deletion); err != nil {
			return nil, fmt.Errorf("decoding connection delete response: %w", err)
		}
	}

	return &deletion, nil
}

// validateConnectionKey guards against requests to /api/connections/ with an
// empty key, which would otherwise hit the list endpoint.
func validateConnectionKey(key string) error {
	if strings.TrimSpace(key) == "" {
		return errors.New("a connection key is required")
	}

	return nil
}

func decodeConnection(body []byte) (*Connection, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil, errEmptyBody
	}

	var raw map[string]any
	if err := json.Unmarshal(trimmed, &raw); err != nil {
		return nil, fmt.Errorf("decoding connection response: %w", err)
	}

	connection := &Connection{Raw: raw}
	connection.ConnectionKey, _ = raw["connectionKey"].(string)
	connection.Technology, _ = raw["technology"].(string)
	connection.State, _ = raw["state"].(string)

	if connection.ConnectionKey == "" {
		return nil, errors.New("decoding connection response: the API response did not contain a connectionKey")
	}

	return connection, nil
}
