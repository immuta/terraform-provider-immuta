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
	"strings"
)

// DomainTypeDomain is the only documented domain type.
const DomainTypeDomain = "domain"

// Domain is the documented domain object.
type Domain struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	Description    string          `json:"description,omitempty"`
	Type           string          `json:"type,omitempty"`
	AssignmentType string          `json:"assignmentType,omitempty"`
	Settings       *DomainSettings `json:"settings,omitempty"`
	Tags           []DomainTag     `json:"tags,omitempty"`
	CreatedBy      *int64          `json:"createdBy,omitempty"`
	CreatedAt      string          `json:"createdAt,omitempty"`
	UpdatedAt      string          `json:"updatedAt,omitempty"`
}

// DomainSettings holds the documented domain settings object.
type DomainSettings struct {
	Autoscanning *bool `json:"autoscanning,omitempty"`
}

// DomainTag is a tag used for dynamic data source assignment.
type DomainTag struct {
	ID          *int64 `json:"id,omitempty"`
	Name        string `json:"name"`
	DisplayName string `json:"displayName,omitempty"`
}

// CreateDomainRequest is the documented POST /domain body.
//
// Only the attributes owned by the immuta_domain resource are sent. Domain
// permissions and data source assignments are managed by dedicated resources.
type CreateDomainRequest struct {
	Name        string  `json:"name"`
	Type        string  `json:"type"`
	Description *string `json:"description,omitempty"`
}

// UpdateDomainRequest is the documented PUT /domain/{domainId} body. Both
// attributes are optional so that only changed values are sent.
type UpdateDomainRequest struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
}

// IsEmpty reports whether the request would not change anything.
func (r UpdateDomainRequest) IsEmpty() bool {
	return r.Name == nil && r.Description == nil
}

// ListDomainsOptions holds the documented GET /domain query parameters.
type ListDomainsOptions struct {
	Offset       *int
	Size         *int
	SearchText   string
	IsExactMatch *bool
}

func (o ListDomainsOptions) query() url.Values {
	query := url.Values{}
	if o.Offset != nil {
		query.Set("offset", strconv.Itoa(*o.Offset))
	}
	if o.Size != nil {
		query.Set("size", strconv.Itoa(*o.Size))
	}
	if o.SearchText != "" {
		query.Set("searchText", o.SearchText)
	}
	if o.IsExactMatch != nil {
		query.Set("isExactMatch", strconv.FormatBool(*o.IsExactMatch))
	}

	return query
}

// CreateDomain creates a domain with POST /domain.
func (c *Client) CreateDomain(ctx context.Context, req CreateDomainRequest) (*Domain, error) {
	body, err := c.do(ctx, request{
		method:   http.MethodPost,
		segments: []string{"domain"},
		body:     req,
	})
	if err != nil {
		return nil, err
	}

	return decodeDomain(body)
}

// GetDomain reads a domain with GET /domain/{domainId}. It returns an error
// matching ErrNotFound when the domain does not exist.
func (c *Client) GetDomain(ctx context.Context, domainID string) (*Domain, error) {
	if err := validateDomainID(domainID); err != nil {
		return nil, err
	}

	body, err := c.do(ctx, request{
		method:   http.MethodGet,
		segments: []string{"domain", domainID},
	})
	if err != nil {
		return nil, err
	}

	return decodeDomain(body)
}

// GetDomainByName reads a domain with GET /domain/name/{domainName}.
func (c *Client) GetDomainByName(ctx context.Context, name string) (*Domain, error) {
	if strings.TrimSpace(name) == "" {
		return nil, errors.New("a domain name is required")
	}

	body, err := c.do(ctx, request{
		method:   http.MethodGet,
		segments: []string{"domain", "name", name},
	})
	if err != nil {
		return nil, err
	}

	return decodeDomain(body)
}

// UpdateDomain updates the name or description with PUT /domain/{domainId}.
func (c *Client) UpdateDomain(ctx context.Context, domainID string, req UpdateDomainRequest) (*Domain, error) {
	if err := validateDomainID(domainID); err != nil {
		return nil, err
	}

	body, err := c.do(ctx, request{
		method:   http.MethodPut,
		segments: []string{"domain", domainID},
		body:     req,
	})
	if err != nil {
		return nil, err
	}

	domain, err := decodeDomain(body)
	if err != nil {
		// Some deployments answer PUT with an empty body. The caller refreshes
		// with a GET, so an empty update response is not fatal.
		if errors.Is(err, errEmptyBody) {
			return nil, nil
		}

		return nil, err
	}

	return domain, nil
}

// SetDomainAutoscanning changes the documented autoscanning setting with
// PUT /domain/{domainId}/settings. The response only echoes the setting, so
// callers refresh the domain with a GET.
func (c *Client) SetDomainAutoscanning(ctx context.Context, domainID string, autoscanning bool) error {
	if err := validateDomainID(domainID); err != nil {
		return err
	}

	_, err := c.do(ctx, request{
		method:   http.MethodPut,
		segments: []string{"domain", domainID, "settings"},
		body:     DomainSettings{Autoscanning: &autoscanning},
	})

	return err
}

// DeleteDomain deletes a domain with DELETE /domain/{domainId}.
func (c *Client) DeleteDomain(ctx context.Context, domainID string) error {
	if err := validateDomainID(domainID); err != nil {
		return err
	}

	_, err := c.do(ctx, request{
		method:   http.MethodDelete,
		segments: []string{"domain", domainID},
	})

	return err
}

// ListDomains lists domains with GET /domain.
func (c *Client) ListDomains(ctx context.Context, opts ListDomainsOptions) ([]Domain, error) {
	body, err := c.do(ctx, request{
		method:   http.MethodGet,
		segments: []string{"domain"},
		query:    opts.query(),
	})
	if err != nil {
		return nil, err
	}

	return decodeDomains(body)
}

// validateDomainID guards against requests to /domain/ with an empty id,
// which would otherwise hit the list endpoint.
func validateDomainID(domainID string) error {
	if strings.TrimSpace(domainID) == "" {
		return errors.New("a domain id is required")
	}

	return nil
}

// errEmptyBody is returned by the decoders when the API answered with an
// empty body.
var errEmptyBody = errors.New("empty response body")

// decodeDomains decodes the documented list response, which wraps domain
// objects in a data array.
func decodeDomains(body []byte) ([]Domain, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil, errEmptyBody
	}

	var envelope struct {
		Data []Domain `json:"data"`
	}
	if err := json.Unmarshal(trimmed, &envelope); err != nil {
		return nil, fmt.Errorf("decoding domain list response: %w", err)
	}

	return envelope.Data, nil
}

// decodeDomain decodes a single domain. The documented responses use both a
// bare domain object and a data envelope, so both shapes are accepted.
func decodeDomain(body []byte) (*Domain, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil, errEmptyBody
	}

	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(trimmed, &envelope); err != nil {
		return nil, fmt.Errorf("decoding domain response: %w", err)
	}

	payload := trimmed
	if len(bytes.TrimSpace(envelope.Data)) > 0 {
		payload = bytes.TrimSpace(envelope.Data)
	}

	if bytes.HasPrefix(payload, []byte("[")) {
		var domains []Domain
		if err := json.Unmarshal(payload, &domains); err != nil {
			return nil, fmt.Errorf("decoding domain response: %w", err)
		}
		if len(domains) == 0 {
			return nil, ErrNotFound
		}

		return &domains[0], nil
	}

	var domain Domain
	if err := json.Unmarshal(payload, &domain); err != nil {
		return nil, fmt.Errorf("decoding domain response: %w", err)
	}

	if domain.ID == "" {
		return nil, errors.New("decoding domain response: the API response did not contain a domain id")
	}

	return &domain, nil
}

// Autoscanning reports the documented settings.autoscanning value, and whether
// the API returned it.
func (d *Domain) Autoscanning() (bool, bool) {
	if d == nil || d.Settings == nil || d.Settings.Autoscanning == nil {
		return false, false
	}

	return *d.Settings.Autoscanning, true
}
