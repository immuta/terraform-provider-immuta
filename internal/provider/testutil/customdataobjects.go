package testutil

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// renameBusyPolls is how many reads of a data object still return its old
// name after a rename, the way an asynchronous rename task would.
const renameBusyPolls = 1

// DataObjectName returns the name Immuta reports for a data object, and
// whether the data object exists.
func (s *ConnectionServer) DataObjectName(id string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	object, ok := s.dataObjects[id]
	if !ok {
		return "", false
	}
	if object.pendingName != "" {
		return object.pendingName, true
	}

	return object.name, true
}

// DataObjectParent returns the id of a data object's parent, "" at the top
// of its connection.
func (s *ConnectionServer) DataObjectParent(id string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	if object, ok := s.dataObjects[id]; ok {
		return object.parent
	}

	return ""
}

// DataObjectDeletes returns how many data object DELETE requests were made.
func (s *ConnectionServer) DataObjectDeletes() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.objectDeletes
}

// AccessGroup returns the id of the identity group bound to a securable, ""
// when none is.
func (s *ConnectionServer) AccessGroup(securableID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.accessGroups[securableID]
}

// AccessGroupUnbinds returns how many access group DELETE requests were made.
func (s *ConnectionServer) AccessGroupUnbinds() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.accessGroupUnbinds
}

// SetAccessGroup binds (or with "" clears) an identity group out of band,
// as a change in the Immuta UI would.
func (s *ConnectionServer) SetAccessGroup(securableID, identityGroupID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.accessGroups == nil {
		s.accessGroups = map[string]string{}
	}
	if identityGroupID == "" {
		delete(s.accessGroups, securableID)

		return
	}
	s.accessGroups[securableID] = identityGroupID
}

func (s *ConnectionServer) technologyLocked(connectionKey string) string {
	connection, ok := s.connections[connectionKey]
	if !ok {
		return ""
	}
	technology, _ := connection.body["technology"].(string)

	return technology
}

// createCustomDataObject follows the documented rules: only FOLDER and
// SECURABLE, only in a Custom connection, only at its top or in a folder,
// and unique by name and type under a parent.
func (s *ConnectionServer) createCustomDataObject(w http.ResponseWriter, r *http.Request, connectionKey string) {
	var body map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeConnectionError(w, http.StatusBadRequest, "VALIDATION", "invalid body")

		return
	}
	var name, objectType string
	var parent *string
	_ = json.Unmarshal(body["name"], &name)
	_ = json.Unmarshal(body["type"], &objectType)
	if raw, ok := body["parentId"]; ok {
		if err := json.Unmarshal(raw, &parent); err != nil || parent == nil {
			writeConnectionError(w, http.StatusBadRequest, "VALIDATION", "parentId must not be null")

			return
		}
	}
	for field := range body {
		if field != "name" && field != "type" && field != "parentId" {
			writeConnectionError(w, http.StatusBadRequest, "VALIDATION", "unknown field "+field)

			return
		}
	}
	name = strings.TrimSpace(name)
	if name == "" || (objectType != "FOLDER" && objectType != "SECURABLE") {
		writeConnectionError(w, http.StatusBadRequest, "VALIDATION", "a name and a FOLDER or SECURABLE type are required")

		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	switch technology := s.technologyLocked(connectionKey); technology {
	case "":
		writeConnectionError(w, http.StatusNotFound, "CONN004", "connection not found")

		return
	case "Custom":
	default:
		writeConnectionError(w, http.StatusBadRequest, "VALIDATION", "data objects can only be created in a Custom connection, not "+technology)

		return
	}

	parentID := ""
	if parent != nil {
		parentObject, ok := s.dataObjects[*parent]
		if !ok || parentObject.connectionKey != connectionKey {
			writeConnectionError(w, http.StatusNotFound, "NOT_FOUND", "parent data object not found")

			return
		}
		if parentObject.objectType != "FOLDER" {
			writeConnectionError(w, http.StatusBadRequest, "VALIDATION", "the parent must be a FOLDER")

			return
		}
		parentID = *parent
	}
	for _, object := range s.dataObjects {
		if object.connectionKey == connectionKey && object.parent == parentID && object.name == name && object.objectType == objectType {
			writeConnectionError(w, http.StatusConflict, "CONFLICT", "a data object with this name and type already exists")

			return
		}
	}

	if s.dataObjects == nil {
		s.dataObjects = map[string]*storedDataObject{}
	}
	s.objectSeq++
	id := fmt.Sprintf("00000000-0000-4000-8000-%012d", s.objectSeq)
	object := &storedDataObject{id: id, connectionKey: connectionKey, name: name, objectType: objectType, parent: parentID}
	s.dataObjects[id] = object

	writeJSON(w, http.StatusCreated, s.dataObjectPayloadLocked(object))
}

// renameCustomDataObject renames asynchronously: the old name is reported
// for renameBusyPolls more reads.
func (s *ConnectionServer) renameCustomDataObject(w http.ResponseWriter, r *http.Request, connectionKey, id string) {
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Name) == "" {
		writeConnectionError(w, http.StatusBadRequest, "VALIDATION", "a name is required")

		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	object, ok := s.dataObjects[id]
	if !ok || object.connectionKey != connectionKey {
		writeConnectionError(w, http.StatusNotFound, "NOT_FOUND", "data object not found")

		return
	}
	if object.objectType != "FOLDER" && object.objectType != "SECURABLE" {
		writeConnectionError(w, http.StatusBadRequest, "VALIDATION", "only Custom folders and securables can be renamed")

		return
	}

	object.pendingName = strings.TrimSpace(body.Name)
	object.pendingPolls = renameBusyPolls

	writeJSON(w, http.StatusAccepted, map[string]any{"status": "accepted", "taskId": "00000000-0000-4000-8000-000000000001"})
}

// advanceRenameLocked finishes a pending rename once it has been read
// renameBusyPolls times under its old name.
func (s *ConnectionServer) advanceRenameLocked(object *storedDataObject) {
	if object.pendingName == "" {
		return
	}
	if object.pendingPolls > 0 {
		object.pendingPolls--

		return
	}
	object.name, object.pendingName = object.pendingName, ""
}

// deleteCustomDataObject deletes a data object, its descendants, and any
// binding of theirs.
func (s *ConnectionServer) deleteCustomDataObject(w http.ResponseWriter, connectionKey, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	object, ok := s.dataObjects[id]
	if !ok || object.connectionKey != connectionKey {
		writeConnectionError(w, http.StatusNotFound, "NOT_FOUND", "data object not found")

		return
	}

	s.objectDeletes++
	doomed := []string{id}
	for i := 0; i < len(doomed); i++ {
		for _, child := range s.dataObjects {
			if child.parent == doomed[i] {
				doomed = append(doomed, child.id)
			}
		}
	}
	// Immuta refuses to delete a data object while it, or a securable under
	// it, still has a bound access group.
	for _, gone := range doomed {
		if _, bound := s.accessGroups[gone]; bound {
			writeConnectionError(w, http.StatusConflict, "CONFLICT",
				"Remove authored access and unbind every accessGroup before deleting this data object")

			return
		}
	}
	for _, gone := range doomed {
		delete(s.dataObjects, gone)
	}

	writeJSON(w, http.StatusAccepted, map[string]any{"taskId": "00000000-0000-4000-8000-000000000000"})
}

// handleAccessGroup serves GET, PUT and DELETE of a securable's access group.
func (s *ConnectionServer) handleAccessGroup(w http.ResponseWriter, r *http.Request, connectionKey, securableID string) {
	var body struct {
		Action          string `json:"action"`
		IdentityGroupID string `json:"identityGroupId"`
	}
	if r.Method == http.MethodPut {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeConnectionError(w, http.StatusBadRequest, "VALIDATION", "invalid body")

			return
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	securable, ok := s.dataObjects[securableID]
	if !ok || securable.connectionKey != connectionKey {
		writeConnectionError(w, http.StatusNotFound, "NOT_FOUND", "data object not found")

		return
	}

	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.accessGroupPayloadLocked(securableID))
	case http.MethodPut:
		s.putAccessGroupLocked(w, securable, body.Action, body.IdentityGroupID)
	case http.MethodDelete:
		s.accessGroupUnbinds++
		delete(s.accessGroups, securableID)
		w.WriteHeader(http.StatusNoContent)
	default:
		writeConnectionError(w, http.StatusMethodNotAllowed, "VALIDATION", "method not allowed")
	}
}

func (s *ConnectionServer) putAccessGroupLocked(w http.ResponseWriter, securable *storedDataObject, action, groupID string) {
	switch action {
	case "bind":
		if s.technologyLocked(securable.connectionKey) != "Custom" || securable.objectType != "SECURABLE" {
			writeConnectionError(w, http.StatusBadRequest, "VALIDATION", "the target must be a Custom SECURABLE")

			return
		}
		group, ok := s.dataObjects[groupID]
		if technology := s.technologyLocked(group.connectionKeyOrEmpty()); !ok || (technology != "Entra" && technology != "Okta") {
			writeConnectionError(w, http.StatusBadRequest, "VALIDATION", "identityGroupId must be an identity group data object")

			return
		}
		for bound, existing := range s.accessGroups {
			if existing == groupID || bound == securable.id {
				writeConnectionError(w, http.StatusConflict, "CONFLICT", "the identity group or the securable is already bound")

				return
			}
		}
		if s.accessGroups == nil {
			s.accessGroups = map[string]string{}
		}
		s.accessGroups[securable.id] = groupID
	default:
		writeConnectionError(w, http.StatusBadRequest, "VALIDATION", "action must be bind: unbind with DELETE")

		return
	}

	writeJSON(w, http.StatusOK, s.accessGroupPayloadLocked(securable.id))
}

// accessGroupPayloadLocked renders a binding as GET .../accessGroup does.
func (s *ConnectionServer) accessGroupPayloadLocked(securableID string) map[string]any {
	groupID, ok := s.accessGroups[securableID]
	if !ok {
		return map[string]any{"identityGroupId": nil}
	}

	payload := map[string]any{"state": "LIVE", "identityGroupId": groupID}
	if group, ok := s.dataObjects[groupID]; ok {
		payload["identityConnectionKey"] = group.connectionKey
	}

	return payload
}

func (o *storedDataObject) connectionKeyOrEmpty() string {
	if o == nil {
		return ""
	}

	return o.connectionKey
}

// AddConnection registers an active connection out of band, for tests that
// need one, such as an Entra connection whose groups are bound, without
// managing it with Terraform.
func (s *ConnectionServer) AddConnection(key, technology string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.connections[key] = &storedConnection{
		body:  map[string]any{"technology": technology, "connectionKey": key},
		state: "ACTIVE",
	}
}
