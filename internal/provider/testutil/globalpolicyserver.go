package testutil

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// GlobalPolicyServer is an in-memory implementation of the policy endpoints
// used by immuta_global_policy: the V2 POST /api/v2/policy, which saves a
// document by its policyKey, and the V1 global policy read, search and
// delete endpoints.
//
// By default the V2 response holds no id, so the provider must look the
// policy up, which is the path taken when the response is not understood.
type GlobalPolicyServer struct {
	server *httptest.Server

	mu            sync.Mutex
	policies      map[string]*storedPolicy
	nextID        int
	respondWithID bool
	// getsBeforeFailure counts down the single-policy reads that succeed
	// before one fails; zero means no failure is planned.
	getsBeforeFailure int
	saves, deletes    int
	requests          []RecordedRequest
}

type storedPolicy struct {
	id       string
	document map[string]any
	name     string
	// actions is the V1 shaped content returned by the V1 read. A save
	// derives it from the document; EditActions changes it out of band.
	actions any
}

// NewGlobalPolicyServer starts a fake Immuta policy API.
func NewGlobalPolicyServer() *GlobalPolicyServer {
	s := &GlobalPolicyServer{policies: map[string]*storedPolicy{}}
	s.server = httptest.NewServer(http.HandlerFunc(s.handle))

	return s
}

// URL is the base URL of the fake API.
func (s *GlobalPolicyServer) URL() string { return s.server.URL }

// Close shuts the fake API down.
func (s *GlobalPolicyServer) Close() { s.server.Close() }

// RespondWithID makes the V2 save response include the policy id.
func (s *GlobalPolicyServer) RespondWithID() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.respondWithID = true
}

// Count returns the number of stored policies.
func (s *GlobalPolicyServer) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.policies)
}

// Saves returns how many saves were received.
func (s *GlobalPolicyServer) Saves() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.saves
}

// Deletes returns how many deletes were received.
func (s *GlobalPolicyServer) Deletes() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.deletes
}

// Requests returns the requests received so far.
func (s *GlobalPolicyServer) Requests() []RecordedRequest {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]RecordedRequest(nil), s.requests...)
}

// Policy returns the id and stored document of the policy with a key.
func (s *GlobalPolicyServer) Policy(policyKey string) (string, map[string]any, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, policy := range s.policies {
		if policy.document["policyKey"] == policyKey {
			return policy.id, policy.document, true
		}
	}

	return "", nil, false
}

// Rename changes a policy's name out of band, simulating drift.
func (s *GlobalPolicyServer) Rename(id, name string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if policy, ok := s.policies[id]; ok {
		policy.name = name
	}
}

// EditActions changes a policy's V1 actions out of band, simulating an edit
// made in Immuta to the content of the policy.
func (s *GlobalPolicyServer) EditActions(id string, actions any) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if policy, ok := s.policies[id]; ok {
		policy.actions = actions
	}
}

// FailGetAfter makes one read of a single policy fail, after the next skip
// reads succeed. The failure is not transient, so it is not retried.
func (s *GlobalPolicyServer) FailGetAfter(skip int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.getsBeforeFailure = skip + 1
}

// Remove deletes a policy out of band, simulating remote deletion.
func (s *GlobalPolicyServer) Remove(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.policies, id)
}

func (s *GlobalPolicyServer) handle(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.requests = append(s.requests, RecordedRequest{Method: r.Method, Path: r.URL.EscapedPath()})
	s.mu.Unlock()

	if r.Header.Get("Authorization") != TestAPIKey {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "invalid credentials"})

		return
	}

	segments := splitPath(r.URL.EscapedPath())

	switch {
	case r.Method == http.MethodPost && strings.Join(segments, "/") == "api/v2/policy":
		s.save(w, r)
	case r.Method == http.MethodGet && strings.Join(segments, "/") == "policy/global":
		s.search(w, r.URL.Query())
	case len(segments) == 3 && segments[0] == "policy" && segments[1] == "global":
		id, _ := url.PathUnescape(segments[2])
		switch r.Method {
		case http.MethodGet:
			s.get(w, id)
		case http.MethodDelete:
			s.delete(w, id)
		default:
			writeJSON(w, http.StatusNotFound, map[string]string{"message": "unknown endpoint"})
		}
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "unknown endpoint"})
	}
}

func (s *GlobalPolicyServer) save(w http.ResponseWriter, r *http.Request) {
	var document map[string]any
	if err := json.NewDecoder(r.Body).Decode(&document); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid body"})

		return
	}

	key, _ := document["policyKey"].(string)
	name, _ := document["name"].(string)
	policyType, _ := document["type"].(string)
	if key == "" || name == "" || (policyType != "data" && policyType != "subscription") || document["actions"] == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "policyKey, name, type and actions are required"})

		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.saves++

	var policy *storedPolicy
	for _, existing := range s.policies {
		if existing.document["policyKey"] == key {
			policy = existing
		}
	}
	if policy == nil {
		s.nextID++
		policy = &storedPolicy{id: strconv.Itoa(s.nextID)}
		s.policies[policy.id] = policy
	}
	policy.document = document
	policy.name = name
	policy.actions = []any{map[string]any{"type": "v1-shaped action", "from": document["actions"]}}

	response := map[string]any{"policyKey": key, "name": name}
	if s.respondWithID {
		id, _ := strconv.Atoi(policy.id)
		response["id"] = id
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *GlobalPolicyServer) search(w http.ResponseWriter, query url.Values) {
	s.mu.Lock()
	defer s.mu.Unlock()

	searchText := strings.ToLower(query.Get("searchText"))
	policyType := query.Get("type")

	var ids []int
	for _, policy := range s.policies {
		if policyType != "" && policy.document["type"] != policyType {
			continue
		}
		if !strings.Contains(strings.ToLower(policy.name), searchText) {
			continue
		}
		id, _ := strconv.Atoi(policy.id)
		ids = append(ids, id)
	}
	sort.Ints(ids)

	results := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		policy := s.policies[strconv.Itoa(id)]
		results = append(results, map[string]any{"id": id, "name": policy.name, "type": policy.document["type"]})
	}

	writeJSON(w, http.StatusOK, results)
}

func (s *GlobalPolicyServer) get(w http.ResponseWriter, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.getsBeforeFailure > 0 {
		s.getsBeforeFailure--
		if s.getsBeforeFailure == 0 {
			writeJSON(w, http.StatusForbidden, map[string]string{"message": "read failed"})

			return
		}
	}

	policy, ok := s.policies[id]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "policy not found"})

		return
	}

	// A V1 policy object, which is not in the V2 document format.
	numericID, _ := strconv.Atoi(policy.id)
	writeJSON(w, http.StatusOK, map[string]any{
		"id":        numericID,
		"policyKey": policy.document["policyKey"],
		"name":      policy.name,
		"type":      policy.document["type"],
		"template":  false,
		"deleted":   false,
		"staged":    false,
		"actions":   policy.actions,
	})
}

func (s *GlobalPolicyServer) delete(w http.ResponseWriter, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.policies[id]; !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "policy not found"})

		return
	}

	s.deletes++
	delete(s.policies, id)
	writeJSON(w, http.StatusOK, map[string]any{"id": id})
}
