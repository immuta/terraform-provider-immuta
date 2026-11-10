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

// dataObjectSearchPageSize is the largest page the data object search allows.
const dataObjectSearchPageSize = 100

// DataObject holds the documented data object fields the provider uses.
type DataObject struct {
	ID            string
	Name          string
	Type          string
	ConnectionKey string
	// Path is the object path from the top of the connection to the object,
	// the object included.
	Path []DataObjectPathSegment
	// SyncPolicy is the "Data policy" setting; nil when Immuta returned none.
	SyncPolicy *bool
	// SyncPolicyInherited reports whether SyncPolicy comes from a parent data
	// object or the connection rather than being set on this data object.
	SyncPolicyInherited bool
	// Tags are the tag assignments Immuta reports on the data object, both
	// applied to it directly and inherited.
	Tags []DataObjectTag
}

// DataObjectTag is one documented tag assignment on a data object.
type DataObjectTag struct {
	ID   int64
	Name string
	// Inherited reports whether the tag comes from a parent data object or the
	// connection rather than being applied to this data object.
	Inherited bool
}

// DirectTag returns the tag with the given id when it is applied to the data
// object itself, ignoring inherited assignments.
func (o *DataObject) DirectTag(id int64) (DataObjectTag, bool) {
	for _, tag := range o.Tags {
		if tag.ID == id && !tag.Inherited {
			return tag, true
		}
	}

	return DataObjectTag{}, false
}

// DataObjectPathSegment is one level of a data object path.
type DataObjectPathSegment struct {
	ID   string
	Name string
	Type string
}

// PathNames returns the names along the object path.
func (o *DataObject) PathNames() []string {
	names := make([]string, 0, len(o.Path))
	for _, segment := range o.Path {
		names = append(names, segment.Name)
	}

	return names
}

// SearchDataObjects returns every data object of a connection with the given
// name, with the documented POST /api/connections/dataObjects/_search,
// following its cursor. Callers wanting an exact name or path must filter
// the result.
func (c *Client) SearchDataObjects(ctx context.Context, connectionKey, name string) ([]DataObject, error) {
	if err := validateConnectionKey(connectionKey); err != nil {
		return nil, err
	}
	if strings.TrimSpace(name) == "" {
		return nil, errors.New("a data object name is required")
	}

	var objects []DataObject
	cursor := ""
	for {
		page := map[string]any{"limit": dataObjectSearchPageSize}
		if cursor != "" {
			page["cursor"] = cursor
		}

		body, err := c.do(ctx, request{
			method:   http.MethodPost,
			segments: []string{"api", "connections", "dataObjects", "_search"},
			body: map[string]any{
				"filter": map[string]any{
					"connection": map[string]any{"connectionKey": connectionKey},
					"dataObject": map[string]any{"name": name},
				},
				"meta": map[string]any{"page": page},
			},
		})
		if err != nil {
			return nil, err
		}

		var decoded struct {
			Data []json.RawMessage `json:"data"`
			Meta struct {
				Page struct {
					Cursor string `json:"cursor"`
				} `json:"page"`
			} `json:"meta"`
		}
		if err := json.Unmarshal(bytes.TrimSpace(body), &decoded); err != nil {
			return nil, fmt.Errorf("decoding data object search: %w", err)
		}
		for _, item := range decoded.Data {
			object, err := decodeDataObject(item)
			if err != nil {
				return nil, err
			}
			objects = append(objects, *object)
		}

		if len(decoded.Data) == 0 || decoded.Meta.Page.Cursor == "" || decoded.Meta.Page.Cursor == cursor {
			return objects, nil
		}
		cursor = decoded.Meta.Page.Cursor
	}
}

// GetDataObject reads a data object with the documented
// GET /api/connections/{connectionKey}/dataObjects/{id}. It returns an error
// matching ErrNotFound when the data object does not exist.
func (c *Client) GetDataObject(ctx context.Context, connectionKey, id string) (*DataObject, error) {
	if err := validateDataObjectRef(connectionKey, id); err != nil {
		return nil, err
	}

	body, err := c.do(ctx, request{
		method:   http.MethodGet,
		segments: []string{"api", "connections", connectionKey, "dataObjects", id},
	})
	if err != nil {
		return nil, err
	}

	return decodeDataObject(body)
}

// SetDataObjectSyncPolicy sets the "Data policy" setting of a data object
// with the documented PATCH /api/connections/{connectionKey}/dataObjects/{id}.
// nil clears it, so that the data object inherits it again. No other setting
// is sent, so owners are left as they are.
func (c *Client) SetDataObjectSyncPolicy(ctx context.Context, connectionKey, id string, syncPolicy *bool) error {
	if err := validateDataObjectRef(connectionKey, id); err != nil {
		return err
	}

	_, err := c.do(ctx, request{
		method:   http.MethodPatch,
		segments: []string{"api", "connections", connectionKey, "dataObjects", id},
		body:     map[string]any{"settings": map[string]any{"syncPolicy": syncPolicy}},
	})

	return err
}

// PatchDataObjectTags adds and removes tag assignments on a data object with
// the documented tags field of PATCH
// /api/connections/{connectionKey}/dataObjects/{id}. Only the listed tags
// change; every other tag on the data object is left as it is.
func (c *Client) PatchDataObjectTags(ctx context.Context, connectionKey, id string, add, remove []int64) error {
	if err := validateDataObjectRef(connectionKey, id); err != nil {
		return err
	}
	if len(add) == 0 && len(remove) == 0 {
		return errors.New("at least one tag to add or remove is required")
	}

	_, err := c.do(ctx, request{
		method:   http.MethodPatch,
		segments: []string{"api", "connections", connectionKey, "dataObjects", id},
		body:     map[string]any{"tags": map[string]any{"add": tagIDEntries(add), "remove": tagIDEntries(remove)}},
	})

	return err
}

// tagIDEntries renders tag ids as documented patch entries. It never returns
// nil, so that an empty list is sent as [] rather than null.
func tagIDEntries(ids []int64) []map[string]int64 {
	entries := make([]map[string]int64, 0, len(ids))
	for _, id := range ids {
		entries = append(entries, map[string]int64{"id": id})
	}

	return entries
}

func validateDataObjectRef(connectionKey, id string) error {
	if err := validateConnectionKey(connectionKey); err != nil {
		return err
	}
	if strings.TrimSpace(id) == "" {
		return errors.New("a data object id is required")
	}

	return nil
}

// objectPathSegment is the documented recursive response object path.
type objectPathSegment struct {
	ID    string             `json:"id"`
	Name  string             `json:"name"`
	Type  string             `json:"type"`
	Child *objectPathSegment `json:"child"`
}

func decodeDataObject(body []byte) (*DataObject, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil, errEmptyBody
	}

	var decoded struct {
		ID            string             `json:"id"`
		Name          string             `json:"name"`
		Type          string             `json:"type"`
		ConnectionKey string             `json:"connectionKey"`
		ObjectPath    *objectPathSegment `json:"objectPath"`
		Settings      struct {
			SyncPolicy *struct {
				Value         *bool           `json:"value"`
				InheritedFrom json.RawMessage `json:"inheritedFrom"`
			} `json:"syncPolicy"`
		} `json:"settings"`
		Tags []struct {
			ID            int64           `json:"id"`
			Name          string          `json:"name"`
			InheritedFrom json.RawMessage `json:"inheritedFrom"`
		} `json:"tags"`
	}
	if err := json.Unmarshal(trimmed, &decoded); err != nil {
		return nil, fmt.Errorf("decoding data object: %w", err)
	}
	if decoded.ID == "" {
		return nil, errors.New("decoding data object: the API response did not contain a data object id")
	}

	object := &DataObject{
		ID:            decoded.ID,
		Name:          decoded.Name,
		Type:          decoded.Type,
		ConnectionKey: decoded.ConnectionKey,
	}
	for segment := decoded.ObjectPath; segment != nil; segment = segment.Child {
		object.Path = append(object.Path, DataObjectPathSegment{ID: segment.ID, Name: segment.Name, Type: segment.Type})
	}
	if syncPolicy := decoded.Settings.SyncPolicy; syncPolicy != nil {
		object.SyncPolicy = syncPolicy.Value
		object.SyncPolicyInherited = isPresent(syncPolicy.InheritedFrom)
	}
	for _, tag := range decoded.Tags {
		object.Tags = append(object.Tags, DataObjectTag{ID: tag.ID, Name: tag.Name, Inherited: isPresent(tag.InheritedFrom)})
	}

	return object, nil
}

// isPresent reports whether an optional JSON value was sent and is not null.
func isPresent(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)

	return len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null"))
}
