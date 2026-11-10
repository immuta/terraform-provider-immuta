// Package testutil provides HTTP fixtures, normalizers and helpers shared by
// the provider unit and acceptance tests.
package testutil

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
)

// TestAPIKey is the API key accepted by DomainServer.
const TestAPIKey = "test-api-key"

// RecordedRequest captures what a test server received. Authorization headers
// and request bodies are deliberately not recorded.
type RecordedRequest struct {
	Method string
	Path   string
}

// DomainServer is an in-memory implementation of the documented Immuta
// /domain endpoints. It lets resource tests exercise the full Terraform
// lifecycle without a live Immuta tenant.
type DomainServer struct {
	server *httptest.Server

	mu       sync.Mutex
	domains  map[string]*storedDomain
	requests []RecordedRequest
	nextID   int

	dataSources map[int]*storedDataSource
	jobDelay    int
	clock       time.Time

	assignmentDelay int
}

type storedDomain struct {
	ID             string      `json:"id"`
	Name           string      `json:"name"`
	Description    string      `json:"description"`
	Type           string      `json:"type"`
	AssignmentType string      `json:"assignmentType"`
	Tags           []domainTag `json:"tags"`
	Settings       setting     `json:"settings"`
	CreatedBy      int64       `json:"createdBy"`
	CreatedAt      string      `json:"createdAt"`
	UpdatedAt      string      `json:"updatedAt"`

	pending *pendingAssignment
}

// pendingAssignment is an assignment Immuta accepted but GET /domain/{id}
// does not show yet, for the given number of reads.
type pendingAssignment struct {
	assignmentType string
	tags           []domainTag
	reads          int
}

type domainTag struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	ID          int64  `json:"id"`
}

type setting struct {
	Autoscanning bool `json:"autoscanning"`
}

// NewDomainServer starts a fake Immuta domain API.
func NewDomainServer() *DomainServer {
	s := &DomainServer{
		domains: map[string]*storedDomain{},
		clock:   time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
	}
	s.server = httptest.NewServer(http.HandlerFunc(s.handle))

	return s
}

// URL is the base URL of the fake API.
func (s *DomainServer) URL() string {
	return s.server.URL
}

// Close shuts the fake API down.
func (s *DomainServer) Close() {
	s.server.Close()
}

// Requests returns the requests received so far.
func (s *DomainServer) Requests() []RecordedRequest {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]RecordedRequest(nil), s.requests...)
}

// Count returns the number of stored domains.
func (s *DomainServer) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.domains)
}

// Description returns the stored description of a domain.
func (s *DomainServer) Description(id string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	domain, ok := s.domains[id]
	if !ok {
		return "", false
	}

	return domain.Description, true
}

// SetDescription changes a description out of band, simulating remote drift.
func (s *DomainServer) SetDescription(id, description string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if domain, ok := s.domains[id]; ok {
		domain.Description = description
		domain.UpdatedAt = s.tick()
	}
}

// Assignment returns the assignment type and tag names of a domain,
// including an assignment Immuta has accepted but not applied yet.
func (s *DomainServer) Assignment(id string) (string, []string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	domain, ok := s.domains[id]
	if !ok {
		return "", nil
	}

	assignmentType, tags := domain.AssignmentType, domain.Tags
	if domain.pending != nil {
		assignmentType, tags = domain.pending.assignmentType, domain.pending.tags
	}

	var names []string
	for _, tag := range tags {
		names = append(names, tag.Name)
	}

	return assignmentType, names
}

// SetAssignmentDelay makes GET /domain/{id} keep returning the previous
// assignment for the given number of reads after each assignment request, as
// Immuta does while it applies the assignment in the background.
func (s *DomainServer) SetAssignmentDelay(reads int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.assignmentDelay = reads
}

// SetAssignment changes the assignment of a domain out of band, simulating
// remote drift.
func (s *DomainServer) SetAssignment(id, assignmentType string, tagNames ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if domain, ok := s.domains[id]; ok {
		domain.AssignmentType = assignmentType
		domain.Tags = storedTags(tagNames)
		domain.pending = nil
	}
}

// Remove deletes a domain out of band, simulating remote deletion.
func (s *DomainServer) Remove(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.domains, id)
}

// tick must be called with the lock held.
func (s *DomainServer) tick() string {
	s.clock = s.clock.Add(time.Second)

	return s.clock.Format("2006-01-02T15:04:05.000Z")
}

func (s *DomainServer) handle(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.requests = append(s.requests, RecordedRequest{Method: r.Method, Path: r.URL.EscapedPath()})
	s.mu.Unlock()

	if r.Header.Get("Authorization") != TestAPIKey {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "invalid credentials"})

		return
	}

	segments := splitPath(r.URL.EscapedPath())
	if s.handleDataSources(w, r, segments) {
		return
	}

	switch {
	case len(segments) == 1 && segments[0] == "domain" && r.Method == http.MethodPost:
		s.create(w, r)
	case len(segments) == 1 && segments[0] == "domain" && r.Method == http.MethodGet:
		s.list(w)
	case len(segments) == 3 && segments[0] == "domain" && segments[1] == "name" && r.Method == http.MethodGet:
		s.getByName(w, segments[2])
	case len(segments) == 2 && segments[0] == "domain" && r.Method == http.MethodGet:
		s.get(w, segments[1])
	case len(segments) == 2 && segments[0] == "domain" && r.Method == http.MethodPut:
		s.update(w, r, segments[1])
	case len(segments) == 2 && segments[0] == "domain" && r.Method == http.MethodDelete:
		s.delete(w, segments[1])
	case len(segments) == 3 && segments[0] == "domain" && segments[2] == "settings" && r.Method == http.MethodPut:
		s.updateSettings(w, r, segments[1])
	case len(segments) == 3 && segments[0] == "domain" && segments[2] == "assignment" && r.Method == http.MethodPost:
		s.assign(w, r, segments[1])
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "unknown endpoint"})
	}
}

func (s *DomainServer) create(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name        string  `json:"name"`
		Type        string  `json:"type"`
		Description *string `json:"description"`
	}

	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid request body"})

		return
	}

	if body.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "name is required"})

		return
	}

	if body.Type != "domain" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "type must be domain"})

		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, existing := range s.domains {
		if existing.Name == body.Name {
			writeJSON(w, http.StatusConflict, map[string]string{"message": "a domain with that name already exists"})

			return
		}
	}

	s.nextID++
	now := s.tick()

	domain := &storedDomain{
		ID:             fmt.Sprintf("c7458ee9-03e5-4b6f-8395-%012d", s.nextID),
		Name:           body.Name,
		Type:           body.Type,
		AssignmentType: "manual",
		Tags:           []domainTag{},
		CreatedBy:      2,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if body.Description != nil {
		domain.Description = *body.Description
	}

	s.domains[domain.ID] = domain

	writeJSON(w, http.StatusOK, domain)
}

// assign implements POST /domain/{domainId}/assignment as documented:
// dynamic needs at least one tag, and the response counts the data sources
// added to, kept in and removed from the domain by their tags.
func (s *DomainServer) assign(w http.ResponseWriter, r *http.Request, id string) {
	var body struct {
		AssignmentType string `json:"assignmentType"`
		Tags           []struct {
			Name string `json:"name"`
		} `json:"tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid body"})

		return
	}
	var names []string
	for _, tag := range body.Tags {
		names = append(names, tag.Name)
	}
	if (body.AssignmentType != "manual" && body.AssignmentType != "dynamic") ||
		(body.AssignmentType == "dynamic" && (len(names) == 0 || slices.Contains(names, ""))) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "assignmentType must be manual, or dynamic with at least one tag"})

		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	domain, ok := s.domains[id]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "domain not found"})

		return
	}

	if s.assignmentDelay > 0 {
		domain.pending = &pendingAssignment{assignmentType: body.AssignmentType, tags: storedTags(names), reads: s.assignmentDelay}
	} else {
		domain.AssignmentType = body.AssignmentType
		domain.Tags = storedTags(names)
		domain.pending = nil
	}
	domain.UpdatedAt = s.tick()

	var added, kept, removed int
	if body.AssignmentType == "dynamic" {
		added, kept, removed = s.assignByTags(id, names)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"assignmentType": body.AssignmentType, "dataSourcesAdded": added, "dataSourcesKept": kept, "dataSourcesRemoved": removed,
	})
}

// storedTags returns tags as Immuta returns them, in another order than
// given, so that ordering is never mistaken for a change.
func storedTags(names []string) []domainTag {
	tags := []domainTag{}
	for i := len(names) - 1; i >= 0; i-- {
		tags = append(tags, domainTag{Name: names[i], DisplayName: names[i], ID: int64(100 + i)})
	}

	return tags
}

func (s *DomainServer) get(w http.ResponseWriter, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	domain, ok := s.domains[id]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "domain not found"})

		return
	}

	if pending := domain.pending; pending != nil {
		if pending.reads > 0 {
			pending.reads--
		} else {
			domain.AssignmentType, domain.Tags, domain.pending = pending.assignmentType, pending.tags, nil
		}
	}

	writeJSON(w, http.StatusOK, domain)
}

func (s *DomainServer) getByName(w http.ResponseWriter, rawName string) {
	name, err := url.PathUnescape(rawName)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid domain name"})

		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, domain := range s.domains {
		if domain.Name == name {
			writeJSON(w, http.StatusOK, domain)

			return
		}
	}

	writeJSON(w, http.StatusNotFound, map[string]string{"message": "domain not found"})
}

func (s *DomainServer) list(w http.ResponseWriter) {
	s.mu.Lock()
	defer s.mu.Unlock()

	domains := make([]*storedDomain, 0, len(s.domains))
	for _, domain := range s.domains {
		domains = append(domains, domain)
	}

	writeJSON(w, http.StatusOK, map[string]any{"data": domains})
}

// Autoscanning returns the autoscanning setting of a domain.
func (s *DomainServer) Autoscanning(id string) (bool, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	domain, ok := s.domains[id]
	if !ok {
		return false, false
	}

	return domain.Settings.Autoscanning, true
}

// SetAutoscanning changes the autoscanning setting out of band, as a user in
// the Immuta UI would.
func (s *DomainServer) SetAutoscanning(id string, autoscanning bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if domain, ok := s.domains[id]; ok {
		domain.Settings.Autoscanning = autoscanning
		domain.UpdatedAt = s.tick()
	}
}

// updateSettings implements the documented PUT /domain/{domainId}/settings,
// which takes an optional autoscanning value and echoes the setting back.
func (s *DomainServer) updateSettings(w http.ResponseWriter, r *http.Request, id string) {
	var body struct {
		Autoscanning *bool `json:"autoscanning"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid request body"})

		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	domain, ok := s.domains[id]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "domain not found"})

		return
	}

	if body.Autoscanning != nil {
		domain.Settings.Autoscanning = *body.Autoscanning
		domain.UpdatedAt = s.tick()
	}

	writeJSON(w, http.StatusOK, map[string]bool{"autoscanning": domain.Settings.Autoscanning})
}

func (s *DomainServer) update(w http.ResponseWriter, r *http.Request, id string) {
	var body struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
	}

	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid request body"})

		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	domain, ok := s.domains[id]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "domain not found"})

		return
	}

	if body.Name != nil {
		domain.Name = *body.Name
	}
	if body.Description != nil {
		domain.Description = *body.Description
	}
	domain.UpdatedAt = s.tick()

	writeJSON(w, http.StatusOK, domain)
}

func (s *DomainServer) delete(w http.ResponseWriter, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.domains[id]; !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "domain not found"})

		return
	}

	delete(s.domains, id)
	w.WriteHeader(http.StatusNoContent)
}

func splitPath(path string) []string {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return nil
	}

	return strings.Split(trimmed, "/")
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
