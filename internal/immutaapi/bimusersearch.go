package immutaapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// userSearchPageSize is the page size used to search users.
const userSearchPageSize = 100

// SearchUsersOptions are the documented GET /bim/user filters the provider
// uses. Immuta matches email and userid partially.
type SearchUsersOptions struct {
	Email  string
	UserID string
	IAMID  string
}

// SearchUsers returns every user matching the options, with the documented
// GET /bim/user, following its pages. Matches are partial: callers wanting
// one user must filter the result for an exact match.
func (c *Client) SearchUsers(ctx context.Context, opts SearchUsersOptions) ([]User, error) {
	if opts.Email == "" && opts.UserID == "" {
		return nil, errors.New("an email or a user id is required to search users")
	}

	var users []User
	for offset := 0; ; {
		query := url.Values{}
		query.Set("offset", strconv.Itoa(offset))
		query.Set("size", strconv.Itoa(userSearchPageSize))
		if opts.Email != "" {
			query.Set("email", opts.Email)
		}
		if opts.UserID != "" {
			query.Set("userid", opts.UserID)
		}
		if opts.IAMID != "" {
			query.Set("iamid", opts.IAMID)
		}

		body, err := c.do(ctx, request{
			method:   http.MethodGet,
			segments: []string{"bim", "user"},
			query:    query,
		})
		if err != nil {
			return nil, err
		}

		page, err := decodeUserList(body)
		if err != nil {
			return nil, err
		}
		users = append(users, page...)
		offset += len(page)

		if len(page) < userSearchPageSize {
			return users, nil
		}
	}
}

// decodeUserList decodes a list of detailed user objects. The documentation
// calls the response a list without showing its envelope, so both a bare
// array and the {"count": n, "hits": [...]} envelope Immuta uses for searches
// are accepted.
func decodeUserList(body []byte) ([]User, error) {
	trimmed := bytes.TrimSpace(body)

	var users []User
	if bytes.HasPrefix(trimmed, []byte("[")) {
		if err := json.Unmarshal(trimmed, &users); err != nil {
			return nil, fmt.Errorf("decoding user search: %w", err)
		}

		return users, nil
	}

	var envelope struct {
		Hits *[]User `json:"hits"`
	}
	if err := json.Unmarshal(trimmed, &envelope); err != nil {
		return nil, fmt.Errorf("decoding user search: %w", err)
	}
	if envelope.Hits == nil {
		return nil, errors.New("decoding user search: the response is neither a list of users nor an object with hits")
	}

	return *envelope.Hits, nil
}
