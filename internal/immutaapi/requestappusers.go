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

// RequestAppUserSearchLimit is the number of users asked for from the
// Request app user search. The API has no paging, so a search returning this
// many users may have left some out.
const RequestAppUserSearchLimit = 100

// RequestAppUser is the part of a Request app user the provider uses. The
// Request app is the only documented API returning the Immuta global user
// ID, which identifies a user across identity managers, for example as the
// subject matter expert of a data product.
type RequestAppUser struct {
	IAMID        string `json:"iamId"`
	GlobalUserID string `json:"globalUserId"`
	Username     string `json:"username"`
	Name         string `json:"name"`
	Email        string `json:"email"`
}

// SearchRequestAppUsers searches users with the documented Request app
// GET /api/metadata/users. Matches are partial: callers wanting one user must
// filter the result for an exact match. truncated reports that the search
// returned as many users as it was asked for, so more may exist.
func (c *Client) SearchRequestAppUsers(ctx context.Context, filter string) (users []RequestAppUser, truncated bool, err error) {
	if strings.TrimSpace(filter) == "" {
		return nil, false, errors.New("a filter is required to search users")
	}

	query := url.Values{}
	query.Set("filter", filter)
	query.Set("limit", strconv.Itoa(RequestAppUserSearchLimit))

	body, err := c.requestAppDo(ctx, request{
		method:   http.MethodGet,
		segments: []string{"api", "metadata", "users"},
		query:    query,
	})
	if err != nil {
		return nil, false, err
	}

	var decoded struct {
		Users *[]RequestAppUser `json:"users"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, false, fmt.Errorf("decoding user search: %w", err)
	}
	if decoded.Users == nil {
		return nil, false, errors.New("decoding user search: the API response did not contain a users list")
	}

	return *decoded.Users, len(*decoded.Users) >= RequestAppUserSearchLimit, nil
}
