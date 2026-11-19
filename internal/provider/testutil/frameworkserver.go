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

	"github.com/immuta/terraform-provider-immuta/internal/immutaapi"
)

// FrameworkServer is an in-memory implementation of the documented
// /frameworks endpoints used by the immuta_framework resource.
//
// It returns tags and rules in the reverse of the order they were sent, so
// that tests prove ordering never produces a diff, and it always returns empty
// lists as [] the way the documented responses do.
type FrameworkServer struct {
	server *httptest.Server

	mu         sync.Mutex
	frameworks map[string]*immutaapi.Framework
	requests   []RecordedRequest
	bodies     []map[string]any
	nextID     int64
	clock      time.Time
}

// NewFrameworkServer starts a fake Immuta frameworks API.
func NewFrameworkServer() *FrameworkServer {
	s := &FrameworkServer{
		frameworks: map[string]*immutaapi.Framework{},
		clock:      time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
	}
	s.server = httptest.NewServer(http.HandlerFunc(s.handle))

	return s
}

// URL is the base URL of the fake API.
func (s *FrameworkServer) URL() string { return s.server.URL }

// Close shuts the fake API down.
func (s *FrameworkServer) Close() { s.server.Close() }

// Requests returns the requests received so far.
func (s *FrameworkServer) Requests() []RecordedRequest {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]RecordedRequest(nil), s.requests...)
}

// LastBody returns the most recent POST or PUT body, decoded generically.
func (s *FrameworkServer) LastBody() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.bodies) == 0 {
		return nil
	}

	return s.bodies[len(s.bodies)-1]
}

// Count returns the number of stored frameworks.
func (s *FrameworkServer) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.frameworks)
}

// Framework returns a copy of a stored framework.
func (s *FrameworkServer) Framework(id string) (immutaapi.Framework, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	framework, ok := s.frameworks[id]
	if !ok {
		return immutaapi.Framework{}, false
	}

	return *framework, true
}

// SetDescription changes a description out of band, simulating drift.
func (s *FrameworkServer) SetDescription(id, description string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if framework, ok := s.frameworks[id]; ok {
		framework.Description = description
		framework.Version = s.nextUUID()
	}
}

// Remove deletes a framework out of band, simulating remote deletion.
func (s *FrameworkServer) Remove(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.frameworks, id)
}

// nextUUID must be called with the lock held.
func (s *FrameworkServer) nextUUID() string {
	s.nextID++

	return fmt.Sprintf("00000000-0000-4000-9000-%012d", s.nextID)
}

func (s *FrameworkServer) handle(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.requests = append(s.requests, RecordedRequest{Method: r.Method, Path: r.URL.EscapedPath()})
	s.mu.Unlock()

	if r.Header.Get("Authorization") != TestAPIKey {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "invalid credentials"})

		return
	}

	segments := splitPath(r.URL.EscapedPath())
	if len(segments) == 0 || segments[0] != "frameworks" || len(segments) > 2 {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "unknown endpoint"})

		return
	}

	if len(segments) == 1 {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusNotFound, map[string]string{"message": "unknown endpoint"})

			return
		}
		s.save(w, r, "")

		return
	}

	id, err := url.PathUnescape(segments[1])
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid path"})

		return
	}

	switch r.Method {
	case http.MethodGet:
		s.get(w, id)
	case http.MethodPut:
		s.save(w, r, id)
	case http.MethodDelete:
		s.delete(w, id)
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "unknown endpoint"})
	}
}

// frameworkPatch is a framework body in which every field is optional, as
// the documented update allows.
type frameworkPatch struct {
	ShortName   *string                    `json:"shortName"`
	Name        *string                    `json:"name"`
	Description *string                    `json:"description"`
	Tags        *[]immutaapi.FrameworkTag  `json:"tags"`
	Rules       *[]immutaapi.FrameworkRule `json:"rules"`
	Active      *bool                      `json:"active"`
}

func (s *FrameworkServer) save(w http.ResponseWriter, r *http.Request, id string) {
	var raw map[string]any
	var patch frameworkPatch

	decoder := json.NewDecoder(r.Body)
	var body json.RawMessage
	if err := decoder.Decode(&body); err != nil ||
		json.Unmarshal(body, &raw) != nil ||
		json.Unmarshal(body, &patch) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid body"})

		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.bodies = append(s.bodies, raw)

	var framework *immutaapi.Framework
	if id == "" {
		if patch.ShortName == nil || patch.Name == nil || patch.Description == nil ||
			patch.Tags == nil || patch.Rules == nil || patch.Active == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": "shortName, name, description, tags, rules and active are required"})

			return
		}

		createdBy := int64(2)
		s.clock = s.clock.Add(time.Second)
		framework = &immutaapi.Framework{
			ID:        s.nextUUID(),
			CreatedBy: &createdBy,
			CreatedAt: s.clock.Format("2006-01-02T15:04:05.000Z"),
		}
	} else {
		existing, ok := s.frameworks[id]
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{"message": "framework not found"})

			return
		}
		copied := *existing
		framework = &copied
	}

	if patch.ShortName != nil {
		framework.ShortName = *patch.ShortName
	}
	if patch.Name != nil {
		framework.Name = *patch.Name
	}
	if patch.Description != nil {
		framework.Description = *patch.Description
	}
	if patch.Tags != nil {
		framework.Tags = *patch.Tags
	}
	if patch.Rules != nil {
		framework.Rules = *patch.Rules
	}
	if patch.Active != nil {
		framework.Active = *patch.Active
	}

	if message := validateFramework(framework); message != "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": message})

		return
	}

	framework.Version = s.nextUUID()
	s.frameworks[framework.ID] = framework

	writeJSON(w, http.StatusOK, frameworkPayload(framework))
}

func validateFramework(framework *immutaapi.Framework) string {
	if strings.TrimSpace(framework.ShortName) == "" || strings.TrimSpace(framework.Name) == "" ||
		strings.TrimSpace(framework.Description) == "" {
		return "shortName, name and description must not be empty"
	}
	if len(framework.Rules) > immutaapi.FrameworkMaxRules {
		return fmt.Sprintf("a framework can have at most %d rules", immutaapi.FrameworkMaxRules)
	}

	return ""
}

func (s *FrameworkServer) get(w http.ResponseWriter, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	framework, ok := s.frameworks[id]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "framework not found"})

		return
	}

	writeJSON(w, http.StatusOK, frameworkPayload(framework))
}

func (s *FrameworkServer) delete(w http.ResponseWriter, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.frameworks[id]; !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "framework not found"})

		return
	}

	delete(s.frameworks, id)
	w.WriteHeader(http.StatusNoContent)
}

// frameworkPayload renders a framework with every list reversed and every
// empty list as [].
func frameworkPayload(framework *immutaapi.Framework) map[string]any {
	tags := make([]map[string]any, 0, len(framework.Tags))
	for _, tag := range slices.Backward(framework.Tags) {
		sensitivities := make([]map[string]any, 0, len(tag.Sensitivities))
		for _, sensitivity := range slices.Backward(tag.Sensitivities) {
			sensitivities = append(sensitivities, map[string]any{
				"dimension":   sensitivity.Dimension,
				"sensitivity": sensitivity.Sensitivity,
			})
		}
		tags = append(tags, map[string]any{"name": tag.Name, "source": tag.Source, "sensitivities": sensitivities})
	}

	rules := make([]map[string]any, 0, len(framework.Rules))
	for _, rule := range slices.Backward(framework.Rules) {
		rules = append(rules, map[string]any{
			"name":               rule.Name,
			"classificationTag":  map[string]any{"name": rule.ClassificationTag.Name, "source": rule.ClassificationTag.Source},
			"columnTags":         tagRefPayload(rule.ColumnTags),
			"neighborColumnTags": tagRefPayload(rule.NeighborColumnTags),
			"tableTags":          tagRefPayload(rule.TableTags),
		})
	}

	return map[string]any{
		"id":          framework.ID,
		"version":     framework.Version,
		"shortName":   framework.ShortName,
		"name":        framework.Name,
		"description": framework.Description,
		// Real tenants return createdBy as an object, not the documented integer.
		"createdBy": map[string]any{"id": *framework.CreatedBy, "name": "Test User"},
		"createdAt": framework.CreatedAt,
		"tags":      tags,
		"rules":     rules,
		"active":    framework.Active,
	}
}

func tagRefPayload(refs []immutaapi.FrameworkTagRef) []map[string]any {
	payload := make([]map[string]any, 0, len(refs))
	for _, ref := range slices.Backward(refs) {
		payload = append(payload, map[string]any{"name": ref.Name, "source": ref.Source})
	}

	return payload
}
