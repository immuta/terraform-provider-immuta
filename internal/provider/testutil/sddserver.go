package testutil

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// SDDServer is an in-memory implementation of the Immuta tag and identifier
// endpoints used by the immuta_tag, immuta_identifier and
// immuta_domain_identifier resources.
//
// Every endpoint it implements is documented. It reproduces the two rules
// that matter most for the provider: a tag referenced by an identifier must
// already exist, and copying identifiers into a domain clones them rather
// than moving them.
type SDDServer struct {
	server *httptest.Server

	mu          sync.Mutex
	tags        map[string]*StoredTag
	identifiers map[string]*StoredIdentifier
	domains     map[string]string
	requests    []RecordedRequest

	rootTagRequests []RootTagRequest

	// createdAsObjects makes bulk copy report each created copy as an object
	// carrying its id rather than as the bare id.
	createdAsObjects bool
	nextID           int64
	clock            time.Time
}

// StoredTag is the fake API's record of a tag.
type StoredTag struct {
	ID            int64
	Name          string
	Source        string
	SystemCreated bool
}

// SetBulkCopyCreatedAsObjects selects how bulk copy reports each created
// copy: as an object carrying its id when true, as the bare id when false.
// Only the response envelope has been observed on a real tenant, so the
// provider is tested against both.
func (s *SDDServer) SetBulkCopyCreatedAsObjects(asObjects bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.createdAsObjects = asObjects
}

// SeedCopy stores an identifier in a domain directly, standing in for an
// orphaned copy left behind by an earlier failed apply.
func (s *SDDServer) SeedCopy(sourceID, domainID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	source := s.identifiers[sourceID]
	clone := *source
	clone.ID = s.nextIdentifierID()
	clone.DomainID = domainID
	s.identifiers[clone.ID] = &clone

	return clone.ID
}

// RootTagRequest records the rootTag object of a POST /tag request.
type RootTagRequest struct {
	Name            string
	DeleteHierarchy bool
	// DeleteHierarchySent is false when the field was omitted entirely.
	DeleteHierarchySent bool
}

// RootTagRequests returns the rootTag objects received so far.
func (s *SDDServer) RootTagRequests() []RootTagRequest {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]RootTagRequest(nil), s.rootTagRequests...)
}

// StoredIdentifier is the fake API's record of an identifier.
type StoredIdentifier struct {
	ID            string
	Name          string
	Description   string
	Type          string
	Regex         string
	Values        []string
	CaseSensitive bool
	TagNames      []string
	DomainID      string
	CreatedAt     string
	UpdatedAt     string
}

// NewSDDServer starts a fake Immuta tag and identifier API.
func NewSDDServer() *SDDServer {
	s := &SDDServer{
		tags:        map[string]*StoredTag{},
		identifiers: map[string]*StoredIdentifier{},
		domains:     map[string]string{},
		clock:       time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
	}
	s.server = httptest.NewServer(http.HandlerFunc(s.handle))

	return s
}

// URL is the base URL of the fake API.
func (s *SDDServer) URL() string { return s.server.URL }

// Close shuts the fake API down.
func (s *SDDServer) Close() { s.server.Close() }

// Requests returns the requests received so far.
func (s *SDDServer) Requests() []RecordedRequest {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]RecordedRequest(nil), s.requests...)
}

// AddDomain registers a domain so that identifiers can be created in it.
func (s *SDDServer) AddDomain(id, name string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.domains[id] = name
}

// TagCount returns the number of tags that have not been deleted.
func (s *SDDServer) TagCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.tags)
}

// IdentifierCount returns the number of stored identifiers.
func (s *SDDServer) IdentifierCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.identifiers)
}

// TagNames returns the stored tag names in a stable order.
func (s *SDDServer) TagNames() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	names := make([]string, 0, len(s.tags))
	for name := range s.tags {
		names = append(names, name)
	}
	sort.Strings(names)

	return names
}

// Identifier returns a copy of a stored identifier.
func (s *SDDServer) Identifier(id string) (StoredIdentifier, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	identifier, ok := s.identifiers[id]
	if !ok {
		return StoredIdentifier{}, false
	}

	return *identifier, true
}

// SeedTag creates a tag directly, standing in for a tag that already exists
// in the tenant.
func (s *SDDServer) SeedTag(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.createTagLocked(name)
}

// SeedIdentifier creates an identifier directly, standing in for a reference
// identifier that already exists in the tenant.
func (s *SDDServer) SeedIdentifier(name, identifierType, regex string, tagNames []string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, tagName := range tagNames {
		s.createTagLocked(tagName)
	}

	now := s.tick()
	identifier := &StoredIdentifier{
		ID:          s.nextIdentifierID(),
		Name:        name,
		Description: "Seeded by the test",
		Type:        identifierType,
		Regex:       regex,
		TagNames:    append([]string(nil), tagNames...),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	s.identifiers[identifier.ID] = identifier

	return identifier.ID
}

// SetIdentifierDescription changes a description out of band, simulating
// drift.
func (s *SDDServer) SetIdentifierDescription(id, description string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if identifier, ok := s.identifiers[id]; ok {
		identifier.Description = description
		identifier.UpdatedAt = s.tick()
	}
}

// RemoveIdentifier deletes an identifier out of band, simulating remote
// deletion.
func (s *SDDServer) RemoveIdentifier(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.identifiers, id)
}

// RemoveTag deletes a tag out of band, simulating remote deletion.
func (s *SDDServer) RemoveTag(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.tags, name)
}

// tick must be called with the lock held.
func (s *SDDServer) tick() string {
	s.clock = s.clock.Add(time.Second)

	return s.clock.Format("2006-01-02T15:04:05.000Z")
}

// nextIdentifierID must be called with the lock held. The shape mimics a
// UUID closely enough for tests without pulling in a dependency.
func (s *SDDServer) nextIdentifierID() string {
	s.nextID++

	return fmt.Sprintf("00000000-0000-4000-8000-%012d", s.nextID)
}

// createTagLocked creates a tag and every parent in its hierarchy, the way
// Immuta does for a dotted name.
func (s *SDDServer) createTagLocked(name string) *StoredTag {
	segments := strings.Split(name, ".")

	var created *StoredTag
	for i := range segments {
		qualified := strings.Join(segments[:i+1], ".")
		if existing, ok := s.tags[qualified]; ok {
			created = existing

			continue
		}

		s.nextID++
		created = &StoredTag{ID: s.nextID, Name: qualified, Source: "curated"}
		s.tags[qualified] = created
	}

	return created
}

func (s *SDDServer) handle(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.requests = append(s.requests, RecordedRequest{Method: r.Method, Path: r.URL.EscapedPath()})
	s.mu.Unlock()

	if r.Header.Get("Authorization") != TestAPIKey {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "invalid credentials"})

		return
	}

	escaped := splitPath(r.URL.EscapedPath())
	segments := make([]string, 0, len(escaped))
	for _, segment := range escaped {
		unescaped, err := url.PathUnescape(segment)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid path"})

			return
		}
		segments = append(segments, unescaped)
	}

	switch {
	case len(segments) >= 1 && segments[0] == "tag":
		s.handleTag(w, r, segments[1:])
	case len(segments) >= 2 && segments[0] == "sdd" && segments[1] == "identifier":
		s.handleIdentifier(w, r, segments[2:])
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "unknown endpoint"})
	}
}

func (s *SDDServer) handleTag(w http.ResponseWriter, r *http.Request, rest []string) {
	switch {
	case len(rest) == 0 && r.Method == http.MethodPost:
		s.createTags(w, r)
	case len(rest) == 0 && r.Method == http.MethodGet:
		s.searchTags(w, r)
	case len(rest) == 1 && r.Method == http.MethodDelete:
		s.deleteTag(w, r, rest[0])
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "unknown endpoint"})
	}
}

func (s *SDDServer) createTags(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RootTag *struct {
			Name            string `json:"name"`
			DeleteHierarchy *bool  `json:"deleteHierarchy"`
		} `json:"rootTag"`
		Tags []struct {
			Name string `json:"name"`
		} `json:"tags"`
	}

	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid request body"})

		return
	}

	if len(body.Tags) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "at least one tag is required"})

		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, tag := range body.Tags {
		if strings.TrimSpace(tag.Name) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": "a tag name is required"})

			return
		}
	}

	if body.RootTag == nil {
		// Like Immuta, a plain create is rejected when it would add to a
		// root tag that already exists.
		for _, tag := range body.Tags {
			root, _, nested := strings.Cut(tag.Name, ".")
			if _, exists := s.tags[root]; nested && exists {
				writeJSON(w, http.StatusBadRequest, map[string]string{
					"message": fmt.Sprintf("tag %q overlaps the existing tag hierarchy %q", tag.Name, root),
				})

				return
			}
		}
	} else {
		root := body.RootTag.Name
		deleteHierarchy := body.RootTag.DeleteHierarchy != nil && *body.RootTag.DeleteHierarchy
		s.rootTagRequests = append(s.rootTagRequests, RootTagRequest{
			Name:                root,
			DeleteHierarchy:     deleteHierarchy,
			DeleteHierarchySent: body.RootTag.DeleteHierarchy != nil,
		})

		if _, exists := s.tags[root]; !exists {
			writeJSON(w, http.StatusNotFound, map[string]string{"message": fmt.Sprintf("root tag %q not found", root)})

			return
		}

		for _, tag := range body.Tags {
			if !strings.HasPrefix(tag.Name, root+".") {
				writeJSON(w, http.StatusBadRequest, map[string]string{
					"message": fmt.Sprintf("tag %q is not under the root tag %q", tag.Name, root),
				})

				return
			}
		}

		// Documented behaviour: deleteHierarchy true removes the existing
		// children of the root before the new ones are added.
		if deleteHierarchy {
			for candidate := range s.tags {
				if strings.HasPrefix(candidate, root+".") {
					delete(s.tags, candidate)
				}
			}
		}
	}

	payload := make([]map[string]any, 0, len(body.Tags))
	for _, tag := range body.Tags {
		payload = append(payload, tagPayload(s.createTagLocked(tag.Name)))
	}

	writeJSON(w, http.StatusOK, payload)
}

func (s *SDDServer) searchTags(w http.ResponseWriter, r *http.Request) {
	search := r.URL.Query().Get("searchText")

	s.mu.Lock()
	defer s.mu.Unlock()

	names := make([]string, 0, len(s.tags))
	for name := range s.tags {
		if search == "" || strings.Contains(strings.ToLower(name), strings.ToLower(search)) {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	payload := make([]map[string]any, 0, len(names))
	for _, name := range names {
		payload = append(payload, tagPayload(s.tags[name]))
	}

	writeJSON(w, http.StatusOK, payload)
}

func (s *SDDServer) deleteTag(w http.ResponseWriter, r *http.Request, name string) {
	deleteHierarchy := r.URL.Query().Get("deleteHierarchy") == "true"

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.tags[name]; !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "tag not found"})

		return
	}

	delete(s.tags, name)

	if deleteHierarchy {
		prefix := name + "."
		for candidate := range s.tags {
			if strings.HasPrefix(candidate, prefix) {
				delete(s.tags, candidate)
			}
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *SDDServer) handleIdentifier(w http.ResponseWriter, r *http.Request, rest []string) {
	switch {
	case len(rest) == 0 && r.Method == http.MethodPost:
		s.saveIdentifier(w, r, "")
	case len(rest) == 1 && r.Method == http.MethodGet:
		s.getIdentifier(w, rest[0])
	case len(rest) == 1 && r.Method == http.MethodPut:
		s.saveIdentifier(w, r, rest[0])
	case len(rest) == 1 && r.Method == http.MethodDelete:
		s.deleteIdentifier(w, rest[0])
	case len(rest) == 2 && rest[0] == "bulk-copy" && r.Method == http.MethodPost:
		s.bulkCopy(w, r, rest[1])
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "unknown endpoint"})
	}
}

// saveIdentifier handles both create and the documented full-replacement
// update. An empty id means create.
func (s *SDDServer) saveIdentifier(w http.ResponseWriter, r *http.Request, id string) {
	var body struct {
		Name        string  `json:"name"`
		Description string  `json:"description"`
		DomainID    *string `json:"domainId"`
		Type        string  `json:"type"`
		Config      struct {
			Regex         string   `json:"regex"`
			Values        []string `json:"values"`
			CaseSensitive *bool    `json:"caseSensitive"`
		} `json:"config"`
		Tags []struct {
			Name string `json:"name"`
		} `json:"tags"`
	}

	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid request body"})

		return
	}

	if body.Name == "" || body.Description == "" || body.Type == "" || len(body.Tags) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "name, description, type and tags are required"})

		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var existing *StoredIdentifier
	if id != "" {
		var ok bool
		existing, ok = s.identifiers[id]
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{"message": "identifier not found"})

			return
		}
	}

	// Immuta requires every referenced tag to exist already.
	tagNames := make([]string, 0, len(body.Tags))
	for _, tag := range body.Tags {
		if _, ok := s.tags[tag.Name]; !ok {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"message": fmt.Sprintf("tag %q does not exist", tag.Name),
			})

			return
		}
		tagNames = append(tagNames, tag.Name)
	}

	domainID := ""
	if body.DomainID != nil {
		domainID = *body.DomainID
	}
	if domainID != "" {
		if _, ok := s.domains[domainID]; !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{"message": "domain not found"})

			return
		}
	}

	now := s.tick()
	if existing == nil {
		existing = &StoredIdentifier{ID: s.nextIdentifierID(), CreatedAt: now}
		s.identifiers[existing.ID] = existing
	}

	existing.Name = body.Name
	existing.Description = body.Description
	existing.Type = body.Type
	existing.Regex = body.Config.Regex
	existing.Values = append([]string(nil), body.Config.Values...)
	existing.CaseSensitive = body.Config.CaseSensitive != nil && *body.Config.CaseSensitive
	existing.TagNames = tagNames
	existing.DomainID = domainID
	existing.UpdatedAt = now

	writeJSON(w, http.StatusOK, s.identifierPayloadLocked(existing))
}

func (s *SDDServer) getIdentifier(w http.ResponseWriter, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	identifier, ok := s.identifiers[id]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "identifier not found"})

		return
	}

	writeJSON(w, http.StatusOK, s.identifierPayloadLocked(identifier))
}

func (s *SDDServer) deleteIdentifier(w http.ResponseWriter, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.identifiers[id]; !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "identifier not found"})

		return
	}

	delete(s.identifiers, id)
	w.WriteHeader(http.StatusNoContent)
}

// bulkCopy clones identifiers into a domain, leaving the originals alone. It
// answers with the {count, createdIdentifiers, errors} envelope a real tenant
// returns. A copy whose name is already taken in the domain is reported in
// errors and not made; that rule is an assumption standing in for whatever
// Immuta actually rejects.
func (s *SDDServer) bulkCopy(w http.ResponseWriter, r *http.Request, domainID string) {
	var ids []string
	if err := json.NewDecoder(r.Body).Decode(&ids); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "the payload must be an array of identifier ids"})

		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.domains[domainID]; !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "domain not found"})

		return
	}

	created := []any{}
	failures := []any{}

	for _, id := range ids {
		source, ok := s.identifiers[id]
		if !ok {
			failures = append(failures, map[string]any{"identifierId": id, "message": fmt.Sprintf("identifier %s not found", id)})

			continue
		}

		if s.domainHasNameLocked(domainID, source.Name) {
			failures = append(failures, map[string]any{
				"identifierId": id,
				"message":      fmt.Sprintf("an identifier named %q already exists in this domain", source.Name),
			})

			continue
		}

		now := s.tick()
		clone := *source
		clone.ID = s.nextIdentifierID()
		clone.DomainID = domainID
		clone.TagNames = append([]string(nil), source.TagNames...)
		clone.Values = append([]string(nil), source.Values...)
		clone.CreatedAt = now
		clone.UpdatedAt = now
		s.identifiers[clone.ID] = &clone

		if s.createdAsObjects {
			created = append(created, map[string]any{"id": clone.ID, "name": clone.Name})
		} else {
			created = append(created, clone.ID)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"count":              len(created),
		"createdIdentifiers": created,
		"errors":             failures,
	})
}

func (s *SDDServer) domainHasNameLocked(domainID, name string) bool {
	for _, identifier := range s.identifiers {
		if identifier.DomainID == domainID && identifier.Name == name {
			return true
		}
	}

	return false
}

func (s *SDDServer) identifierPayloadLocked(identifier *StoredIdentifier) map[string]any {
	tags := make([]map[string]any, 0, len(identifier.TagNames))
	for _, name := range identifier.TagNames {
		tag := s.tags[name]
		entry := map[string]any{"name": name}
		if tag != nil {
			entry["id"] = tag.ID
		}
		if index := strings.LastIndex(name, "."); index >= 0 {
			entry["displayName"] = name[index+1:]
		}
		tags = append(tags, entry)
	}

	config := map[string]any{}
	switch identifier.Type {
	case "dictionary":
		config["values"] = identifier.Values
		config["caseSensitive"] = identifier.CaseSensitive
	default:
		config["regex"] = identifier.Regex
	}

	payload := map[string]any{
		"id":          identifier.ID,
		"name":        identifier.Name,
		"description": identifier.Description,
		"type":        identifier.Type,
		"config":      config,
		"tags":        tags,
		"createdAt":   identifier.CreatedAt,
		"updatedAt":   identifier.UpdatedAt,
	}

	if identifier.DomainID != "" {
		payload["domain"] = map[string]any{
			"id":   identifier.DomainID,
			"name": s.domains[identifier.DomainID],
		}
	}

	return payload
}

func tagPayload(tag *StoredTag) map[string]any {
	return map[string]any{
		"id":            tag.ID,
		"name":          tag.Name,
		"source":        tag.Source,
		"deleted":       false,
		"systemCreated": tag.SystemCreated,
	}
}
