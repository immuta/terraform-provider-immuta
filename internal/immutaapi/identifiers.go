package immutaapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

// Documented identifier criteria types.
const (
	// IdentifierTypeRegex matches a regular expression against column values.
	IdentifierTypeRegex = "regex"
	// IdentifierTypeDictionary matches a list of values against column values.
	IdentifierTypeDictionary = "dictionary"
	// IdentifierTypeColumnNameRegex matches a regular expression against
	// column names.
	IdentifierTypeColumnNameRegex = "columnNameRegex"
)

// IdentifierTypes lists the documented identifier criteria types.
func IdentifierTypes() []string {
	return []string{IdentifierTypeRegex, IdentifierTypeDictionary, IdentifierTypeColumnNameRegex}
}

// IdentifierConfig is the documented criteria configuration of an identifier.
type IdentifierConfig struct {
	// Regex is used by the regex and columnNameRegex types.
	Regex string `json:"regex,omitempty"`
	// Values is used by the dictionary type.
	Values []string `json:"values,omitempty"`
	// CaseSensitive only applies to the dictionary type and defaults to false.
	CaseSensitive *bool `json:"caseSensitive,omitempty"`
}

// IdentifierTag is a tag applied when an identifier matches.
type IdentifierTag struct {
	ID          *int64 `json:"id,omitempty"`
	Name        string `json:"name"`
	DisplayName string `json:"displayName,omitempty"`
}

// IdentifierDomain is the domain an identifier belongs to.
type IdentifierDomain struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Identifier is the documented identifier object.
type Identifier struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Type        string            `json:"type"`
	Config      IdentifierConfig  `json:"config"`
	Tags        []IdentifierTag   `json:"tags"`
	Domain      *IdentifierDomain `json:"domain,omitempty"`
	CreatedAt   string            `json:"createdAt,omitempty"`
	UpdatedAt   string            `json:"updatedAt,omitempty"`
}

// DomainID returns the ID of the domain the identifier is in, or an empty
// string when it is a global identifier.
func (i *Identifier) DomainID() string {
	if i.Domain == nil {
		return ""
	}

	return i.Domain.ID
}

// TagNames returns the fully qualified names of the tags the identifier
// applies.
func (i *Identifier) TagNames() []string {
	names := make([]string, 0, len(i.Tags))
	for _, tag := range i.Tags {
		names = append(names, tag.Name)
	}

	return names
}

// identifierTagRef is the tag reference accepted on write.
type identifierTagRef struct {
	Name string `json:"name"`
}

// SaveIdentifierRequest is the documented body of POST /sdd/identifier and
// PUT /sdd/identifier/{id}.
//
// The update endpoint does not support partial updates, so both calls send
// the complete identifier.
type SaveIdentifierRequest struct {
	Name        string             `json:"name"`
	Description string             `json:"description"`
	DomainID    *string            `json:"domainId,omitempty"`
	Type        string             `json:"type"`
	Config      IdentifierConfig   `json:"config"`
	Tags        []identifierTagRef `json:"tags"`
}

// NewSaveIdentifierRequest builds a save request from tag names.
func NewSaveIdentifierRequest(name, description, identifierType string, config IdentifierConfig, tagNames []string) SaveIdentifierRequest {
	tags := make([]identifierTagRef, 0, len(tagNames))
	for _, tagName := range tagNames {
		tags = append(tags, identifierTagRef{Name: tagName})
	}

	return SaveIdentifierRequest{
		Name:        name,
		Description: description,
		Type:        identifierType,
		Config:      config,
		Tags:        tags,
	}
}

func (r SaveIdentifierRequest) validate() error {
	if strings.TrimSpace(r.Name) == "" {
		return errors.New("an identifier name is required")
	}
	if strings.TrimSpace(r.Description) == "" {
		return errors.New("an identifier description is required")
	}

	switch r.Type {
	case IdentifierTypeRegex, IdentifierTypeColumnNameRegex:
		if strings.TrimSpace(r.Config.Regex) == "" {
			return fmt.Errorf("identifier type %q requires a regular expression", r.Type)
		}
	case IdentifierTypeDictionary:
		if len(r.Config.Values) == 0 {
			return errors.New(`identifier type "dictionary" requires at least one value`)
		}
	default:
		return fmt.Errorf("identifier type must be one of %s, got %q", strings.Join(IdentifierTypes(), ", "), r.Type)
	}

	if len(r.Tags) == 0 {
		return errors.New("at least one tag is required")
	}

	return nil
}

// CreateIdentifier creates an identifier with POST /sdd/identifier.
func (c *Client) CreateIdentifier(ctx context.Context, req SaveIdentifierRequest) (*Identifier, error) {
	if err := req.validate(); err != nil {
		return nil, err
	}

	body, err := c.do(ctx, request{
		method:   http.MethodPost,
		segments: []string{"sdd", "identifier"},
		body:     req,
	})
	if err != nil {
		return nil, err
	}

	return decodeIdentifier(body)
}

// GetIdentifier reads an identifier with GET /sdd/identifier/{id}.
func (c *Client) GetIdentifier(ctx context.Context, id string) (*Identifier, error) {
	if strings.TrimSpace(id) == "" {
		return nil, errors.New("an identifier id is required")
	}

	body, err := c.do(ctx, request{
		method:   http.MethodGet,
		segments: []string{"sdd", "identifier", id},
	})
	if err != nil {
		return nil, err
	}

	return decodeIdentifier(body)
}

// UpdateIdentifier replaces an identifier with PUT /sdd/identifier/{id}.
// Immuta does not support partial updates, so the whole identifier is sent.
func (c *Client) UpdateIdentifier(ctx context.Context, id string, req SaveIdentifierRequest) (*Identifier, error) {
	if strings.TrimSpace(id) == "" {
		return nil, errors.New("an identifier id is required")
	}
	if err := req.validate(); err != nil {
		return nil, err
	}

	body, err := c.do(ctx, request{
		method:   http.MethodPut,
		segments: []string{"sdd", "identifier", id},
		body:     req,
	})
	if err != nil {
		return nil, err
	}

	return decodeIdentifier(body)
}

// DeleteIdentifier deletes an identifier with DELETE /sdd/identifier/{id}.
func (c *Client) DeleteIdentifier(ctx context.Context, id string) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("an identifier id is required")
	}

	_, err := c.do(ctx, request{
		method:   http.MethodDelete,
		segments: []string{"sdd", "identifier", id},
	})

	return err
}

// CopyIdentifiersToDomain clones existing identifiers into a domain with
// POST /sdd/identifier/bulk-copy/{domainId} and returns the copies.
//
// Immuta clones rather than moves: the originals are left alone and the new
// identifiers live in the domain. The response reports
// {"count", "createdIdentifiers", "errors"}. Any reported error fails the
// call. Each created entry may be the new id or an object carrying it, so the
// copies are read back with GetIdentifier rather than decoded from the
// response.
func (c *Client) CopyIdentifiersToDomain(ctx context.Context, domainID string, identifierIDs []string) ([]Identifier, error) {
	if strings.TrimSpace(domainID) == "" {
		return nil, errors.New("a domain id is required")
	}
	if len(identifierIDs) == 0 {
		return nil, errors.New("at least one identifier id is required")
	}

	for _, id := range identifierIDs {
		if strings.TrimSpace(id) == "" {
			return nil, errors.New("an identifier id is required")
		}
	}

	body, err := c.do(ctx, request{
		method:   http.MethodPost,
		segments: []string{"sdd", "identifier", "bulk-copy", domainID},
		body:     identifierIDs,
	})
	if err != nil {
		return nil, err
	}

	createdIDs, err := decodeBulkCopyResponse(body)
	if err != nil {
		return nil, err
	}

	if len(createdIDs) != len(identifierIDs) {
		return nil, fmt.Errorf("copied %d identifiers but Immuta reported %d created", len(identifierIDs), len(createdIDs))
	}

	copies := make([]Identifier, 0, len(createdIDs))
	for _, id := range createdIDs {
		identifier, err := c.GetIdentifier(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("reading the copied identifier %s: %w", id, err)
		}
		copies = append(copies, *identifier)
	}

	return copies, nil
}

// decodeBulkCopyResponse returns the ids of the created copies, or an error
// carrying the errors Immuta reported. Error messages from Immuta are
// included; anything else about the body is described by shape only.
func decodeBulkCopyResponse(body []byte) ([]string, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil, errEmptyBody
	}

	var response struct {
		CreatedIdentifiers []json.RawMessage `json:"createdIdentifiers"`
		Errors             []json.RawMessage `json:"errors"`
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &fields); err != nil {
		return nil, fmt.Errorf("unexpected bulk copy response: %s", describeJSONShape(trimmed))
	}
	if _, ok := fields["createdIdentifiers"]; !ok {
		return nil, fmt.Errorf("unexpected bulk copy response: %s", describeJSONShape(trimmed))
	}
	if err := json.Unmarshal(trimmed, &response); err != nil {
		return nil, fmt.Errorf("unexpected bulk copy response: %s", describeJSONShape(trimmed))
	}

	if len(response.Errors) > 0 {
		messages := make([]string, 0, len(response.Errors))
		for _, entry := range response.Errors {
			messages = append(messages, bulkCopyErrorMessage(entry))
		}

		return nil, fmt.Errorf("Immuta reported %d error(s) copying the identifier: %s", len(messages), strings.Join(messages, "; "))
	}

	ids := make([]string, 0, len(response.CreatedIdentifiers))
	for _, entry := range response.CreatedIdentifiers {
		id := createdIdentifierID(entry)
		if id == "" {
			return nil, fmt.Errorf("unexpected entry in createdIdentifiers: %s", describeJSONShape(entry))
		}
		ids = append(ids, id)
	}

	return ids, nil
}

// createdIdentifierID reads the id out of a createdIdentifiers entry, which
// may be the id itself or an object carrying it.
func createdIdentifierID(entry json.RawMessage) string {
	var id string
	if err := json.Unmarshal(entry, &id); err == nil {
		return strings.TrimSpace(id)
	}

	var object struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(entry, &object); err == nil {
		return strings.TrimSpace(object.ID)
	}

	return ""
}

// bulkCopyErrorMessage renders one entry of the errors array: a plain
// string, or the message field of an object.
func bulkCopyErrorMessage(entry json.RawMessage) string {
	var text string
	if err := json.Unmarshal(entry, &text); err == nil && text != "" {
		return text
	}

	var object map[string]any
	if err := json.Unmarshal(entry, &object); err == nil {
		for _, key := range []string{"message", "error", "reason"} {
			if value, ok := object[key].(string); ok && value != "" {
				return value
			}
		}
	}

	return "an error of " + describeJSONShape(entry)
}

// describeJSONShape summarises a JSON document without revealing any of its
// values, so that it is safe to put in an error message.
func describeJSONShape(data []byte) string {
	var decoded any
	if err := json.Unmarshal(data, &decoded); err != nil {
		return "not valid JSON"
	}

	switch value := decoded.(type) {
	case map[string]any:
		keys := make([]string, 0, len(value))
		for key := range value {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		return fmt.Sprintf("an object with the keys [%s]", strings.Join(keys, ", "))
	case []any:
		return fmt.Sprintf("an array of %d elements that are not identifiers", len(value))
	default:
		return fmt.Sprintf("a %T", value)
	}
}

func decodeIdentifier(body []byte) (*Identifier, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil, errEmptyBody
	}

	var identifier Identifier
	if err := json.Unmarshal(trimmed, &identifier); err != nil {
		return nil, fmt.Errorf("decoding identifier response: %w", err)
	}

	if identifier.ID == "" {
		return nil, errors.New("decoding identifier response: the API response did not contain an identifier id")
	}

	return &identifier, nil
}
