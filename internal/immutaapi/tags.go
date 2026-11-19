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

// TagSourceCurated is the source Immuta records for tags created through the
// API rather than pulled from an external catalog.
const TagSourceCurated = "curated"

// Tag is the documented tag object.
type Tag struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	Source        string `json:"source,omitempty"`
	Description   string `json:"description,omitempty"`
	Deleted       bool   `json:"deleted"`
	SystemCreated bool   `json:"systemCreated"`
	CreatedAt     string `json:"createdAt,omitempty"`
	UpdatedAt     string `json:"updatedAt,omitempty"`
}

// CreateTags creates tags with POST /tag.
//
// Tag names are fully qualified, with a dot separating a parent tag from its
// children. Immuta creates any missing parents, but it rejects a plain create
// whose root tag already exists, such as a child of the system created
// Discovered hierarchy. For that case rootTag names the existing root and the
// request is sent with the documented rootTag field, which adds the children
// under it. Pass an empty rootTag when the root does not exist yet.
//
// rootTag.deleteHierarchy is always sent as false. True would make Immuta
// delete every existing child of the root, which this method never does.
func (c *Client) CreateTags(ctx context.Context, names []string, rootTag string) ([]Tag, error) {
	if len(names) == 0 {
		return nil, errors.New("at least one tag name is required")
	}

	tags := make([]map[string]string, 0, len(names))
	for _, name := range names {
		if strings.TrimSpace(name) == "" {
			return nil, errors.New("a tag name is required")
		}
		if rootTag != "" && !strings.HasPrefix(name, rootTag+".") {
			return nil, fmt.Errorf("tag %q is not a child of the root tag %q", name, rootTag)
		}
		tags = append(tags, map[string]string{"name": name})
	}

	payload := map[string]any{"tags": tags}
	if rootTag != "" {
		payload["rootTag"] = createTagsRoot{Name: rootTag, DeleteHierarchy: false}
	}

	body, err := c.do(ctx, request{
		method:   http.MethodPost,
		segments: []string{"tag"},
		body:     payload,
	})
	if err != nil {
		return nil, err
	}

	return decodeTags(body)
}

// createTagsRoot is the documented rootTag object. DeleteHierarchy has no
// omitempty so that false is always sent explicitly, as the Immuta UI does.
type createTagsRoot struct {
	Name            string `json:"name"`
	DeleteHierarchy bool   `json:"deleteHierarchy"`
}

// RootTagName returns the top level tag of a fully qualified tag name, or an
// empty string when the name has no parent.
func RootTagName(name string) string {
	root, _, found := strings.Cut(name, ".")
	if !found {
		return ""
	}

	return root
}

// FindTag looks a tag up by its fully qualified name with GET /tag.
//
// The documented search is a wildcard match, so the results are filtered down
// to an exact name. It returns an error matching ErrNotFound when the tag
// does not exist or has been deleted.
func (c *Client) FindTag(ctx context.Context, name string) (*Tag, error) {
	if strings.TrimSpace(name) == "" {
		return nil, errors.New("a tag name is required")
	}

	body, err := c.do(ctx, request{
		method:   http.MethodGet,
		segments: []string{"tag"},
		query:    url.Values{"searchText": []string{name}},
	})
	if err != nil {
		return nil, err
	}

	tags, err := decodeTags(body)
	if err != nil {
		return nil, err
	}

	for _, tag := range tags {
		if tag.Name == name && !tag.Deleted {
			return &tag, nil
		}
	}

	return nil, ErrNotFound
}

// DeleteTag deletes a tag with DELETE /tag/{tag}.
//
// When deleteHierarchy is true Immuta also deletes every child tag.
func (c *Client) DeleteTag(ctx context.Context, name string, deleteHierarchy bool) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("a tag name is required")
	}

	_, err := c.do(ctx, request{
		method:   http.MethodDelete,
		segments: []string{"tag", name},
		query:    url.Values{"deleteHierarchy": []string{strconv.FormatBool(deleteHierarchy)}},
	})

	return err
}

func decodeTags(body []byte) ([]Tag, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil, errEmptyBody
	}

	var tags []Tag
	if err := json.Unmarshal(trimmed, &tags); err != nil {
		return nil, fmt.Errorf("decoding tag response: %w", err)
	}

	return tags, nil
}
