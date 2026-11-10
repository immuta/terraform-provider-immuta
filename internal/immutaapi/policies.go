package immutaapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

// Global policies are written with the declarative V2 endpoint,
// POST /api/v2/policy, which identifies a policy by its policyKey. The V2 API
// documents no read or delete, so those use the V1 global policy endpoints,
// which identify a policy by a numeric id and also return its policyKey.

// GlobalPolicy is the part of a V1 global policy object the provider uses.
// Undocumented and unused fields are ignored.
type GlobalPolicy struct {
	ID        string `json:"id"`
	PolicyKey string `json:"policyKey"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	Deleted   bool   `json:"deleted"`

	// Fingerprint is a hash of the documented V1 policy content: actions,
	// circumstances, staged and certification. That content is not in the V2
	// document format, so it cannot be compared with a document, but a change
	// to the fingerprint shows the policy was changed in Immuta.
	Fingerprint string `json:"-"`
}

// UnmarshalJSON accepts the id as a number, as documented, or as a string.
func (p *GlobalPolicy) UnmarshalJSON(data []byte) error {
	type plain GlobalPolicy

	var decoded struct {
		ID json.RawMessage `json:"id"`
		*plain
	}
	decoded.plain = (*plain)(p)

	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}

	id, err := flexibleID(decoded.ID)
	if err != nil {
		return err
	}
	p.ID = id

	fingerprint, err := globalPolicyFingerprint(data)
	if err != nil {
		return err
	}
	p.Fingerprint = fingerprint

	return nil
}

// globalPolicyFingerprint hashes the documented content fields of a V1 global
// policy object. The fields are re-encoded first, so key order and whitespace
// do not change the hash; array order does, as it may be significant.
func globalPolicyFingerprint(data []byte) (string, error) {
	var content struct {
		Actions       any `json:"actions"`
		Circumstances any `json:"circumstances"`
		Staged        any `json:"staged"`
		Certification any `json:"certification"`
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&content); err != nil {
		return "", err
	}

	// encoding/json writes map keys in sorted order.
	canonical, err := json.Marshal(content)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)

	return hex.EncodeToString(sum[:]), nil
}

// SaveGlobalPolicy creates or updates a global policy with
// POST /api/v2/policy. The document is sent as given; Immuta matches it to an
// existing policy by its policyKey.
//
// The response is not documented, so the id of the saved policy is resolved
// with ResolveGlobalPolicyID.
func (c *Client) SaveGlobalPolicy(ctx context.Context, document map[string]any) ([]byte, error) {
	if key, _ := document["policyKey"].(string); strings.TrimSpace(key) == "" {
		return nil, errors.New("a policy document needs a policyKey")
	}

	return c.do(ctx, request{
		method:   http.MethodPost,
		segments: []string{"api", "v2", "policy"},
		body:     document,
	})
}

// GetGlobalPolicy reads a global policy with GET /policy/global/{policyId}.
// It returns an error matching ErrNotFound when the policy does not exist or
// is flagged as deleted.
func (c *Client) GetGlobalPolicy(ctx context.Context, id string) (*GlobalPolicy, error) {
	if strings.TrimSpace(id) == "" {
		return nil, errors.New("a global policy id is required")
	}

	body, err := c.do(ctx, request{
		method:   http.MethodGet,
		segments: []string{"policy", "global", id},
	})
	if err != nil {
		return nil, err
	}

	var policy GlobalPolicy
	if err := json.Unmarshal(bytes.TrimSpace(body), &policy); err != nil {
		return nil, fmt.Errorf("decoding global policy response: %w", err)
	}
	if policy.ID == "" {
		return nil, errors.New("decoding global policy response: the API response did not contain a policy id")
	}
	if policy.Deleted {
		return nil, fmt.Errorf("global policy %s is deleted: %w", id, ErrNotFound)
	}

	return &policy, nil
}

// DeleteGlobalPolicy deletes a global policy with
// DELETE /policy/global/{policyId}.
func (c *Client) DeleteGlobalPolicy(ctx context.Context, id string) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("a global policy id is required")
	}

	_, err := c.do(ctx, request{
		method:   http.MethodDelete,
		segments: []string{"policy", "global", id},
	})

	return err
}

// globalPolicySearchSize bounds the GET /policy/global name search. Names
// matching more policies than this are not expected; if it happens, the
// lookup reports that the policy was not found rather than guessing.
const globalPolicySearchSize = 100

// listGlobalPolicyIDs returns the ids of global policies whose name contains
// searchText, with GET /policy/global.
func (c *Client) listGlobalPolicyIDs(ctx context.Context, policyType, searchText string) ([]string, error) {
	query := url.Values{}
	query.Set("nameOnly", "true")
	query.Set("searchText", searchText)
	query.Set("size", fmt.Sprint(globalPolicySearchSize))
	if policyType != "" {
		query.Set("type", policyType)
	}

	body, err := c.do(ctx, request{
		method:   http.MethodGet,
		segments: []string{"policy", "global"},
		query:    query,
	})
	if err != nil {
		return nil, err
	}

	// The reference shows a bare array but also describes a hits field, so
	// both are accepted.
	var policies []GlobalPolicy
	if err := json.Unmarshal(body, &policies); err != nil {
		var wrapped struct {
			Hits []GlobalPolicy `json:"hits"`
		}
		if wrappedErr := json.Unmarshal(body, &wrapped); wrappedErr != nil {
			return nil, fmt.Errorf("decoding global policy list: %w", err)
		}
		policies = wrapped.Hits
	}

	ids := make([]string, 0, len(policies))
	for _, policy := range policies {
		if policy.ID != "" {
			ids = append(ids, policy.ID)
		}
	}

	return ids, nil
}

// ResolveGlobalPolicyID finds the V1 id of the global policy with the given
// policyKey. Known ids, such as the id already in state, and any id in the
// POST /api/v2/policy response are tried first;
// otherwise the policies are searched by name and type. Every candidate is
// confirmed by reading it back and comparing its policyKey, so a policy with
// a similar name is never mistaken for this one.
func (c *Client) ResolveGlobalPolicyID(ctx context.Context, saveResponse []byte, policyKey, name, policyType string, knownIDs ...string) (string, error) {
	candidates := slices.Concat(knownIDs, idsInResponse(saveResponse))

	if id, err := c.firstWithPolicyKey(ctx, candidates, policyKey); id != "" || err != nil {
		return id, err
	}

	searched, err := c.listGlobalPolicyIDs(ctx, policyType, name)
	if err != nil {
		return "", fmt.Errorf("looking up the id of global policy %q: %w", policyKey, err)
	}

	if id, err := c.firstWithPolicyKey(ctx, searched, policyKey); id != "" || err != nil {
		return id, err
	}

	return "", fmt.Errorf("immuta accepted the global policy %q, but no global policy named %q with that policyKey was found", policyKey, name)
}

func (c *Client) firstWithPolicyKey(ctx context.Context, ids []string, policyKey string) (string, error) {
	for _, id := range ids {
		policy, err := c.GetGlobalPolicy(ctx, id)
		if IsNotFound(err) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("reading global policy %s: %w", id, err)
		}
		if policy.PolicyKey == policyKey {
			return policy.ID, nil
		}
	}

	return "", nil
}

// idsInResponse collects candidate policy ids from an undocumented response:
// a top-level id, a policy.id, or the ids of a top-level array.
func idsInResponse(body []byte) []string {
	type idOnly struct {
		ID json.RawMessage `json:"id"`
	}
	type withID struct {
		ID     json.RawMessage `json:"id"`
		Policy *idOnly         `json:"policy"`
	}

	var objects []withID

	trimmed := bytes.TrimSpace(body)
	switch {
	case len(trimmed) == 0:
		return nil
	case trimmed[0] == '[':
		_ = json.Unmarshal(trimmed, &objects)
	case trimmed[0] == '{':
		var object withID
		if json.Unmarshal(trimmed, &object) == nil {
			objects = append(objects, object)
		}
	}

	var ids []string
	for _, object := range objects {
		raws := []json.RawMessage{object.ID}
		if object.Policy != nil {
			raws = append(raws, object.Policy.ID)
		}
		for _, raw := range raws {
			if id, err := flexibleID(raw); err == nil && id != "" {
				ids = append(ids, id)
			}
		}
	}

	return ids
}
