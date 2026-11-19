package immutaapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// Documented domain data source assignment types.
const (
	DomainAssignmentManual  = "manual"
	DomainAssignmentDynamic = "dynamic"
)

// DomainAssignmentResult is the documented POST /domain/{domainId}/assignment
// response.
type DomainAssignmentResult struct {
	AssignmentType     string `json:"assignmentType"`
	DataSourcesAdded   int    `json:"dataSourcesAdded"`
	DataSourcesKept    int    `json:"dataSourcesKept"`
	DataSourcesRemoved int    `json:"dataSourcesRemoved"`
}

// SetDomainAssignment changes how data sources are assigned to a domain, with
// the documented POST /domain/{domainId}/assignment. Dynamic assignment needs
// at least one tag name: every data source with any of the tags is then in
// the domain. Manual assignment takes no tags.
func (c *Client) SetDomainAssignment(ctx context.Context, domainID, assignmentType string, tagNames []string) (*DomainAssignmentResult, error) {
	if err := validateDomainID(domainID); err != nil {
		return nil, err
	}

	body := map[string]any{"assignmentType": assignmentType}
	switch assignmentType {
	case DomainAssignmentDynamic:
		if len(tagNames) == 0 {
			return nil, errors.New("dynamic domain assignment needs at least one tag")
		}
		tags := make([]map[string]string, 0, len(tagNames))
		for _, name := range tagNames {
			tags = append(tags, map[string]string{"name": name})
		}
		body["tags"] = tags
	case DomainAssignmentManual:
		if len(tagNames) != 0 {
			return nil, errors.New("manual domain assignment takes no tags")
		}
	default:
		return nil, fmt.Errorf("unknown domain assignment type %q", assignmentType)
	}

	response, err := c.do(ctx, request{
		method:   http.MethodPost,
		segments: []string{"domain", domainID, "assignment"},
		body:     body,
	})
	if err != nil {
		return nil, err
	}

	result := &DomainAssignmentResult{}
	if trimmed := bytes.TrimSpace(response); len(trimmed) > 0 {
		if err := json.Unmarshal(trimmed, result); err != nil {
			return nil, fmt.Errorf("decoding domain assignment response: %w", err)
		}
	}

	return result, nil
}
