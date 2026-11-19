package immutaapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// RequestFormSearchLimit is the number of request forms asked for from the
// Request app request form search.
const RequestFormSearchLimit = 100

// RequestForm is the part of a Request app request form the provider uses.
type RequestForm struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// SearchRequestForms searches request forms by name with the documented
// Request app GET /api/request-form, asking Immuta for exact matches only.
// Callers should still check the name, as the match may ignore case.
func (c *Client) SearchRequestForms(ctx context.Context, name string) ([]RequestForm, error) {
	if strings.TrimSpace(name) == "" {
		return nil, errors.New("a name is required to search request forms")
	}

	query := url.Values{}
	query.Set("nameFilter", name)
	query.Set("exactMatch", "true")
	query.Set("limit", strconv.Itoa(RequestFormSearchLimit))

	body, err := c.requestAppDo(ctx, request{
		method:   http.MethodGet,
		segments: []string{"api", "request-form"},
		query:    query,
	})
	if err != nil {
		return nil, err
	}

	var decoded struct {
		Data *[]RequestForm `json:"data"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, fmt.Errorf("decoding request form search: %w", err)
	}
	if decoded.Data == nil {
		return nil, errors.New("decoding request form search: the API response did not contain a data list")
	}

	return *decoded.Data, nil
}
