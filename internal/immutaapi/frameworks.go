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

// FrameworkMaxRules is the documented maximum number of rules in a framework.
const FrameworkMaxRules = 50

// SensitivityDimensionConfidentiality is the only documented sensitivity
// dimension.
const SensitivityDimensionConfidentiality = "confidentiality"

// Documented sensitivity levels.
const (
	SensitivityNonSensitive    = 0
	SensitivitySensitive       = 1
	SensitivityHighlySensitive = 2
)

// FrameworkSensitivity is the documented sensitivity attached to a framework
// tag.
type FrameworkSensitivity struct {
	Dimension   string `json:"dimension"`
	Sensitivity int64  `json:"sensitivity"`
}

// FrameworkTag is the documented tags object of a framework.
type FrameworkTag struct {
	Name          string                 `json:"name"`
	Source        string                 `json:"source"`
	Sensitivities []FrameworkSensitivity `json:"sensitivities"`
}

// FrameworkTagRef references a tag by name and source. It is used for the
// classification tag and the criteria of a rule.
type FrameworkTagRef struct {
	Name   string `json:"name"`
	Source string `json:"source"`
}

// FrameworkRule is the documented rules object of a framework.
type FrameworkRule struct {
	Name               string            `json:"name"`
	ClassificationTag  FrameworkTagRef   `json:"classificationTag"`
	ColumnTags         []FrameworkTagRef `json:"columnTags"`
	NeighborColumnTags []FrameworkTagRef `json:"neighborColumnTags"`
	TableTags          []FrameworkTagRef `json:"tableTags"`
}

// Framework is the documented framework reference object.
type Framework struct {
	ID          string          `json:"id"`
	Version     string          `json:"version,omitempty"`
	ShortName   string          `json:"shortName"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	CreatedBy   *int64          `json:"createdBy,omitempty"`
	CreatedAt   string          `json:"createdAt,omitempty"`
	Tags        []FrameworkTag  `json:"tags"`
	Rules       []FrameworkRule `json:"rules"`
	Active      bool            `json:"active"`
}

// UnmarshalJSON decodes a framework, tolerating the ways real responses
// differ from the reference:
//
//   - the id may be a string or a number. The reference documents it as a
//     string, but the request parameter tables describe it as a number.
//   - createdBy is documented as an integer, but Immuta returns an object.
//     It is informational only, so a shape that is not understood is
//     ignored rather than failing the request. Failing here would be
//     especially harmful after a create, because the framework would
//     already exist in Immuta without being recorded in Terraform state.
func (f *Framework) UnmarshalJSON(data []byte) error {
	type plain Framework

	var decoded struct {
		ID        json.RawMessage `json:"id"`
		CreatedBy json.RawMessage `json:"createdBy"`
		*plain
	}
	decoded.plain = (*plain)(f)

	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}

	id, err := flexibleID(decoded.ID)
	if err != nil {
		return err
	}
	f.ID = id
	f.CreatedBy = flexibleUserID(decoded.CreatedBy)

	return nil
}

// flexibleUserID extracts a numeric user id from a number, a numeric string
// or an object with an id field. It returns nil for anything else.
func flexibleUserID(raw json.RawMessage) *int64 {
	var object struct {
		ID json.RawMessage `json:"id"`
	}
	if trimmed := bytes.TrimSpace(raw); len(trimmed) > 0 && trimmed[0] == '{' {
		if json.Unmarshal(trimmed, &object) != nil {
			return nil
		}
		raw = object.ID
	}

	text, err := flexibleID(raw)
	if err != nil || text == "" {
		return nil
	}

	id, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return nil
	}

	return &id
}

// flexibleID decodes an id that may be a JSON string or number.
func flexibleID(raw json.RawMessage) (string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return "", nil
	}

	var text string
	if err := json.Unmarshal(trimmed, &text); err == nil {
		return text, nil
	}

	var number json.Number
	if err := json.Unmarshal(trimmed, &number); err == nil {
		return number.String(), nil
	}

	return "", errors.New("the id is neither a string nor a number")
}

// SaveFrameworkRequest is the documented framework payload, used by
// POST /frameworks and PUT /frameworks/{frameworkId}.
//
// The update endpoint supports partial updates, but the provider always sends
// the complete framework so that Terraform state converges on what Immuta
// stores. Active has no omitempty so that false is always sent.
type SaveFrameworkRequest struct {
	ShortName   string          `json:"shortName"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Tags        []FrameworkTag  `json:"tags"`
	Rules       []FrameworkRule `json:"rules"`
	Active      bool            `json:"active"`
}

// withEmptyLists returns a copy in which every nil list is an empty one, so
// that the request carries [] rather than null, as the documentation shows.
func (r SaveFrameworkRequest) withEmptyLists() SaveFrameworkRequest {
	tags := make([]FrameworkTag, 0, len(r.Tags))
	for _, tag := range r.Tags {
		tag.Sensitivities = nonNil(tag.Sensitivities)
		tags = append(tags, tag)
	}

	rules := make([]FrameworkRule, 0, len(r.Rules))
	for _, rule := range r.Rules {
		rule.ColumnTags = nonNil(rule.ColumnTags)
		rule.NeighborColumnTags = nonNil(rule.NeighborColumnTags)
		rule.TableTags = nonNil(rule.TableTags)
		rules = append(rules, rule)
	}

	r.Tags = tags
	r.Rules = rules

	return r
}

func nonNil[T any](values []T) []T {
	if values == nil {
		return []T{}
	}

	return values
}

// validate checks the documented required parameters and the rule limit, so
// that an invalid framework fails before a request is sent.
func (r SaveFrameworkRequest) validate() error {
	if strings.TrimSpace(r.ShortName) == "" {
		return errors.New("a framework short name is required")
	}
	if strings.TrimSpace(r.Name) == "" {
		return errors.New("a framework name is required")
	}
	if strings.TrimSpace(r.Description) == "" {
		return errors.New("a framework description is required")
	}
	if len(r.Rules) > FrameworkMaxRules {
		return fmt.Errorf("a framework can have at most %d rules, got %d", FrameworkMaxRules, len(r.Rules))
	}

	for _, tag := range r.Tags {
		if err := validateFrameworkTagRef(FrameworkTagRef{Name: tag.Name, Source: tag.Source}, "framework tag"); err != nil {
			return err
		}
		for _, sensitivity := range tag.Sensitivities {
			if strings.TrimSpace(sensitivity.Dimension) == "" {
				return fmt.Errorf("a sensitivity dimension is required on the framework tag %q", tag.Name)
			}
		}
	}

	for _, rule := range r.Rules {
		if strings.TrimSpace(rule.Name) == "" {
			return errors.New("a framework rule name is required")
		}
		if err := validateFrameworkTagRef(rule.ClassificationTag, fmt.Sprintf("classification tag of the rule %q", rule.Name)); err != nil {
			return err
		}
		for _, criteria := range [][]FrameworkTagRef{rule.ColumnTags, rule.NeighborColumnTags, rule.TableTags} {
			for _, ref := range criteria {
				if err := validateFrameworkTagRef(ref, fmt.Sprintf("criteria tag of the rule %q", rule.Name)); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func validateFrameworkTagRef(ref FrameworkTagRef, what string) error {
	if strings.TrimSpace(ref.Name) == "" {
		return fmt.Errorf("a name is required for every %s", what)
	}
	if strings.TrimSpace(ref.Source) == "" {
		return fmt.Errorf("a source is required for the %s %q", what, ref.Name)
	}

	return nil
}

// CreateFramework creates a framework with POST /frameworks.
func (c *Client) CreateFramework(ctx context.Context, req SaveFrameworkRequest) (*Framework, error) {
	if err := req.validate(); err != nil {
		return nil, err
	}

	body, err := c.do(ctx, request{
		method:   http.MethodPost,
		segments: []string{"frameworks"},
		body:     req.withEmptyLists(),
	})
	if err != nil {
		return nil, err
	}

	return decodeFramework(body)
}

// GetFramework reads a framework with GET /frameworks/{frameworkId}. It
// returns an error matching ErrNotFound when the framework does not exist.
func (c *Client) GetFramework(ctx context.Context, id string) (*Framework, error) {
	if err := validateFrameworkID(id); err != nil {
		return nil, err
	}

	body, err := c.do(ctx, request{
		method:   http.MethodGet,
		segments: []string{"frameworks", id},
	})
	if err != nil {
		return nil, err
	}

	return decodeFramework(body)
}

// UpdateFramework replaces a framework with PUT /frameworks/{frameworkId}.
//
// When Immuta answers with an empty body the framework is read back with
// GetFramework, so the caller always gets the stored framework.
func (c *Client) UpdateFramework(ctx context.Context, id string, req SaveFrameworkRequest) (*Framework, error) {
	if err := validateFrameworkID(id); err != nil {
		return nil, err
	}
	if err := req.validate(); err != nil {
		return nil, err
	}

	body, err := c.do(ctx, request{
		method:   http.MethodPut,
		segments: []string{"frameworks", id},
		body:     req.withEmptyLists(),
	})
	if err != nil {
		return nil, err
	}

	framework, err := decodeFramework(body)
	if errors.Is(err, errEmptyBody) {
		return c.GetFramework(ctx, id)
	}

	return framework, err
}

// DeleteFramework deletes a framework with DELETE /frameworks/{frameworkId}.
func (c *Client) DeleteFramework(ctx context.Context, id string) error {
	if err := validateFrameworkID(id); err != nil {
		return err
	}

	_, err := c.do(ctx, request{
		method:   http.MethodDelete,
		segments: []string{"frameworks", id},
	})

	return err
}

// validateFrameworkID guards against requests to /frameworks/ with an empty
// id, which would otherwise hit the list endpoint.
func validateFrameworkID(id string) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("a framework id is required")
	}

	return nil
}

func decodeFramework(body []byte) (*Framework, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil, errEmptyBody
	}

	var framework Framework
	if err := json.Unmarshal(trimmed, &framework); err != nil {
		return nil, fmt.Errorf("decoding framework response: %w", err)
	}

	if framework.ID == "" {
		return nil, errors.New("decoding framework response: the API response did not contain a framework id")
	}

	return &framework, nil
}
