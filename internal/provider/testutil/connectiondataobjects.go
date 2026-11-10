package testutil

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

// DataObjectLevel is one level of a data object path, such as a catalog.
type DataObjectLevel struct {
	Name string
	Type string
}

type storedDataObject struct {
	id            string
	connectionKey string
	name          string
	objectType    string
	parent        string
	// syncPolicy is set on this data object; nil means inherited.
	syncPolicy *bool
	// tags are the ids of the tags applied to this data object itself.
	tags []int64
}

// DataObjectTagPatch records the tags object of a data object PATCH.
type DataObjectTagPatch struct {
	ID     string
	Add    []int64
	Remove []int64
}

// AddTag makes a tag known, so that it can be applied to data objects.
func (s *ConnectionServer) AddTag(id int64, name string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.tagNames == nil {
		s.tagNames = map[int64]string{}
	}
	s.tagNames[id] = name
}

// ApplyDataObjectTag applies a tag to a data object out of band, as a user in
// the Immuta UI or sensitive data discovery would.
func (s *ConnectionServer) ApplyDataObjectTag(id string, tagID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if object, ok := s.dataObjects[id]; ok && !slices.Contains(object.tags, tagID) {
		object.tags = append(object.tags, tagID)
	}
}

// RemoveDataObjectTag removes a tag from a data object out of band.
func (s *ConnectionServer) RemoveDataObjectTag(id string, tagID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if object, ok := s.dataObjects[id]; ok {
		object.tags = slices.DeleteFunc(object.tags, func(existing int64) bool { return existing == tagID })
	}
}

// DataObjectTags returns the ids of the tags applied to a data object itself,
// sorted.
func (s *ConnectionServer) DataObjectTags(id string) []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()

	object, ok := s.dataObjects[id]
	if !ok {
		return nil
	}

	tags := slices.Clone(object.tags)
	slices.Sort(tags)

	return tags
}

// DataObjectTagPatches returns the tags object of every data object PATCH
// that had one, in order.
func (s *ConnectionServer) DataObjectTagPatches() []DataObjectTagPatch {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]DataObjectTagPatch(nil), s.tagPatches...)
}

// AddDataObjects stores a data object and its parents, as a connection
// crawl would, and returns the data object's id. Existing levels are reused.
func (s *ConnectionServer) AddDataObjects(connectionKey string, levels ...DataObjectLevel) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.dataObjects == nil {
		s.dataObjects = map[string]*storedDataObject{}
	}

	parent := ""
	for _, level := range levels {
		id := ""
		for _, object := range s.dataObjects {
			if object.connectionKey == connectionKey && object.parent == parent && object.name == level.Name {
				id = object.id
			}
		}
		if id == "" {
			s.objectSeq++
			id = fmt.Sprintf("00000000-0000-4000-8000-%012d", s.objectSeq)
			s.dataObjects[id] = &storedDataObject{id: id, connectionKey: connectionKey, name: level.Name, objectType: level.Type, parent: parent}
		}
		parent = id
	}

	return parent
}

// HideDataObjectsForSearches makes the next searches find no data objects,
// the way a connection crawl that has not finished yet would.
func (s *ConnectionServer) HideDataObjectsForSearches(searches int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.hiddenSearches = searches
}

// DataObjectSearches returns how many data object searches were made.
func (s *ConnectionServer) DataObjectSearches() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.objectSearches
}

// DataObjectSyncPolicy returns the syncPolicy set on a data object; nil when
// it is inherited.
func (s *ConnectionServer) DataObjectSyncPolicy(id string) *bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if object, ok := s.dataObjects[id]; ok {
		return object.syncPolicy
	}

	return nil
}

// SetDataObjectSyncPolicy changes the syncPolicy of a data object out of
// band, as a change in Immuta would.
func (s *ConnectionServer) SetDataObjectSyncPolicy(id string, syncPolicy *bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if object, ok := s.dataObjects[id]; ok {
		object.syncPolicy = syncPolicy
	}
}

// RemoveDataObject deletes a data object out of band, as a connection sync
// would when the remote object is dropped.
func (s *ConnectionServer) RemoveDataObject(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.dataObjects, id)
}

// handleDataObjects serves the documented data object endpoints. It reports
// false for other requests.
func (s *ConnectionServer) handleDataObjects(w http.ResponseWriter, r *http.Request, segments []string) bool {
	switch {
	case len(segments) == 4 && segments[2] == "dataObjects" && segments[3] == "_search" && r.Method == http.MethodPost:
		s.searchDataObjects(w, r)
	case len(segments) == 5 && segments[3] == "dataObjects" && (r.Method == http.MethodGet || r.Method == http.MethodPatch):
		id := segments[4]

		s.mu.Lock()
		defer s.mu.Unlock()

		object, ok := s.dataObjects[id]
		if !ok || object.connectionKey != unescape(segments[2]) {
			writeConnectionError(w, http.StatusNotFound, "NOT_FOUND", "data object not found")

			return true
		}
		if r.Method == http.MethodPatch && !s.patchDataObject(w, r, object) {
			return true
		}
		writeJSON(w, http.StatusOK, s.dataObjectPayloadLocked(object))
	default:
		return false
	}

	return true
}

func (s *ConnectionServer) patchDataObject(w http.ResponseWriter, r *http.Request, object *storedDataObject) bool {
	var body struct {
		Settings map[string]json.RawMessage `json:"settings"`
		Tags     *struct {
			Add    []tagPatchEntry `json:"add"`
			Remove []tagPatchEntry `json:"remove"`
		} `json:"tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || (body.Settings == nil && body.Tags == nil) {
		writeConnectionError(w, http.StatusBadRequest, "VALIDATION", "settings or tags is required")

		return false
	}
	if body.Tags != nil && !s.patchDataObjectTagsLocked(w, object, body.Tags.Add, body.Tags.Remove) {
		return false
	}
	if body.Settings == nil {
		return true
	}
	for setting := range body.Settings {
		if setting != "syncPolicy" && setting != "owners" {
			writeConnectionError(w, http.StatusBadRequest, "VALIDATION", "unknown setting "+setting)

			return false
		}
	}
	if raw, ok := body.Settings["syncPolicy"]; ok {
		var syncPolicy *bool
		if err := json.Unmarshal(raw, &syncPolicy); err != nil {
			writeConnectionError(w, http.StatusBadRequest, "VALIDATION", "syncPolicy must be a boolean or null")

			return false
		}
		object.syncPolicy = syncPolicy
	}
	s.objectPatches = append(s.objectPatches, map[string]any{"id": object.id, "settings": string(mustJSON(body.Settings))})

	return true
}

// tagPatchEntry is one documented entry of a tags add or remove list.
type tagPatchEntry struct {
	ID *int64 `json:"id"`
}

// patchDataObjectTagsLocked applies the documented add and remove lists.
// Like Immuta, it rejects a tag that does not exist; adding a tag that is
// already applied, or removing one that is not, changes nothing.
func (s *ConnectionServer) patchDataObjectTagsLocked(w http.ResponseWriter, object *storedDataObject, add, remove []tagPatchEntry) bool {
	patch := DataObjectTagPatch{ID: object.id}
	for _, entries := range []struct {
		list []tagPatchEntry
		into *[]int64
	}{{add, &patch.Add}, {remove, &patch.Remove}} {
		for _, entry := range entries.list {
			if entry.ID == nil || *entry.ID < 0 {
				writeConnectionError(w, http.StatusBadRequest, "VALIDATION", "a tag entry needs a non-negative id")

				return false
			}
			if _, known := s.tagNames[*entry.ID]; !known {
				writeConnectionError(w, http.StatusNotFound, "NOT_FOUND", fmt.Sprintf("tag %d not found", *entry.ID))

				return false
			}
			*entries.into = append(*entries.into, *entry.ID)
		}
	}

	for _, id := range patch.Add {
		if !slices.Contains(object.tags, id) {
			object.tags = append(object.tags, id)
		}
	}
	object.tags = slices.DeleteFunc(object.tags, func(id int64) bool { return slices.Contains(patch.Remove, id) })
	s.tagPatches = append(s.tagPatches, patch)

	return true
}

// DataObjectPatches returns the settings of every data object PATCH, as
// JSON, in order.
func (s *ConnectionServer) DataObjectPatches() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]map[string]any(nil), s.objectPatches...)
}

func (s *ConnectionServer) searchDataObjects(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Filter struct {
			Connection struct {
				ConnectionKey string `json:"connectionKey"`
			} `json:"connection"`
			DataObject struct {
				Name string `json:"name"`
			} `json:"dataObject"`
		} `json:"filter"`
		Meta struct {
			Page struct {
				Limit  int    `json:"limit"`
				Cursor string `json:"cursor"`
			} `json:"page"`
		} `json:"meta"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Meta.Page.Limit < 1 || body.Meta.Page.Limit > 100 {
		writeConnectionError(w, http.StatusBadRequest, "VALIDATION", "invalid search")

		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.objectSearches++
	var matching []*storedDataObject
	if s.hiddenSearches > 0 {
		s.hiddenSearches--
	} else {
		for _, object := range s.dataObjects {
			// Partial, case-insensitive name matches, as a search would.
			if object.connectionKey == body.Filter.Connection.ConnectionKey &&
				strings.Contains(strings.ToLower(object.name), strings.ToLower(body.Filter.DataObject.Name)) {
				matching = append(matching, object)
			}
		}
	}
	slices.SortFunc(matching, func(a, b *storedDataObject) int { return strings.Compare(a.id, b.id) })

	offset, _ := strconv.Atoi(body.Meta.Page.Cursor)
	end := min(offset+body.Meta.Page.Limit, len(matching))
	data := []any{}
	for _, object := range matching[min(offset, len(matching)):end] {
		data = append(data, s.dataObjectPayloadLocked(object))
	}
	page := map[string]any{"limit": body.Meta.Page.Limit}
	if end < len(matching) {
		page["cursor"] = strconv.Itoa(end)
	}

	writeJSON(w, http.StatusOK, map[string]any{"data": data, "meta": map[string]any{"page": page}})
}

// dataObjectPayloadLocked renders a data object as documented, with its
// effective syncPolicy: its own, else the nearest parent's, else the
// connection's (false).
func (s *ConnectionServer) dataObjectPayloadLocked(object *storedDataObject) map[string]any {
	var chain []*storedDataObject
	for current := object; current != nil; current = s.dataObjects[current.parent] {
		chain = append([]*storedDataObject{current}, chain...)
		if current.parent == "" {
			break
		}
	}

	var objectPath map[string]any
	for i := len(chain) - 1; i >= 0; i-- {
		segment := map[string]any{"id": chain[i].id, "name": chain[i].name, "type": chain[i].objectType}
		if objectPath != nil {
			segment["child"] = objectPath
		}
		objectPath = segment
	}

	syncPolicy := map[string]any{"value": false, "inheritedFrom": map[string]any{"id": object.connectionKey, "type": "CONNECTION"}}
	for i := len(chain) - 1; i >= 0; i-- {
		if chain[i].syncPolicy == nil {
			continue
		}
		syncPolicy = map[string]any{"value": *chain[i].syncPolicy}
		if chain[i] != object {
			syncPolicy["inheritedFrom"] = map[string]any{"id": chain[i].id, "type": "DATA_OBJECT"}
		}

		break
	}

	return map[string]any{
		"id":            object.id,
		"name":          object.name,
		"type":          object.objectType,
		"connectionKey": object.connectionKey,
		"objectPath":    objectPath,
		"settings":      map[string]any{"syncPolicy": syncPolicy, "owners": []any{}},
		"tags":          s.dataObjectTagsPayloadLocked(chain),
	}
}

// dataObjectTagsPayloadLocked renders the documented tag assignments of the
// last data object in chain: its own tags, then those of its parents, nearest
// first, marked with inheritedFrom. A tag is listed once.
func (s *ConnectionServer) dataObjectTagsPayloadLocked(chain []*storedDataObject) []any {
	tags := []any{}
	seen := map[int64]bool{}
	for i := len(chain) - 1; i >= 0; i-- {
		for _, id := range chain[i].tags {
			if seen[id] {
				continue
			}
			seen[id] = true

			name := s.tagNames[id]
			tag := map[string]any{
				"id":          id,
				"name":        name,
				"displayName": name[strings.LastIndex(name, ".")+1:],
				"source":      "curated",
				"application": map[string]any{"appliedAt": "2026-01-01T00:00:00Z", "appliedBy": nil},
			}
			if i != len(chain)-1 {
				tag["inheritedFrom"] = map[string]any{"id": chain[i].id, "type": "DATA_OBJECT"}
			}
			tags = append(tags, tag)
		}
	}

	return tags
}

func unescape(segment string) string {
	value, err := url.PathUnescape(segment)
	if err != nil {
		return segment
	}

	return value
}

func mustJSON(value any) []byte {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}

	return encoded
}
