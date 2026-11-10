package testutil

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
)

// connectionFixedSettings mirrors the connections API reference: settings
// that PATCH /api/connections/{connectionKey} does not accept. It is kept
// separate from the provider's own table so that the fake API checks the
// provider independently.
var connectionFixedSettings = map[string]map[string][]string{
	"BigQuery":   {"clientConfiguration": {"directoryCustomerId", "immutaDataset", "location", "projectId"}},
	"Databricks": {"clientConfiguration": {"host", "immutaSystemCatalog", "port"}, "policyConfiguration": {"excludeGroupPattern"}},
	"Redshift":   {"clientConfiguration": {"host", "immutaSystemDatabase", "port"}},
	"Snowflake":  {"clientConfiguration": {"host", "immutaSystemDatabase", "port"}},
	"Teradata":   {},
	"Trino":      {},
}

// ConnectionServer is an in-memory implementation of the connections API
// used by immuta_connection.
//
// Like the real API it registers and deletes connections asynchronously,
// redacts secrets and adds fields of its own to every response, rejects
// updates to settings that cannot change, and reports errors as a list.
type ConnectionServer struct {
	server *httptest.Server

	mu          sync.Mutex
	connections map[string]*storedConnection
	busyPolls   int
	finalState  string
	teardown    any
	creates     int
	deletes     int
	patches     []map[string]any
	requests    []RecordedRequest

	dataObjects    map[string]*storedDataObject
	objectSeq      int
	hiddenSearches int
	objectSearches int
	objectPatches  []map[string]any
	tagNames       map[int64]string
	tagPatches     []DataObjectTagPatch
}

type storedConnection struct {
	body      map[string]any
	state     string
	busyPolls int
}

// NewConnectionServer starts a fake Immuta connections API. Each connection
// reports itself busy for one read after every change before settling.
func NewConnectionServer() *ConnectionServer {
	s := &ConnectionServer{connections: map[string]*storedConnection{}, busyPolls: 1, finalState: "ACTIVE"}
	s.server = httptest.NewServer(http.HandlerFunc(s.handle))

	return s
}

// URL is the base URL of the fake API.
func (s *ConnectionServer) URL() string { return s.server.URL }

// Close shuts the fake API down.
func (s *ConnectionServer) Close() { s.server.Close() }

// SettleAs sets the state connections reach once registered or updated.
func (s *ConnectionServer) SettleAs(state string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.finalState = state
}

// ReturnTeardown makes deletes return teardown instructions.
func (s *ConnectionServer) ReturnTeardown(teardown any) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.teardown = teardown
}

// Body returns the stored registration payload, secrets included.
func (s *ConnectionServer) Body(key string) (map[string]any, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	connection, ok := s.connections[key]
	if !ok {
		return nil, false
	}

	return connection.body, true
}

// Count returns the number of stored connections.
func (s *ConnectionServer) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.connections)
}

// Creates returns how many connections were registered.
func (s *ConnectionServer) Creates() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.creates
}

// Deletes returns how many connections were deleted.
func (s *ConnectionServer) Deletes() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.deletes
}

// Patches returns the bodies of every accepted update.
func (s *ConnectionServer) Patches() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]map[string]any(nil), s.patches...)
}

// Requests returns the requests received so far.
func (s *ConnectionServer) Requests() []RecordedRequest {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]RecordedRequest(nil), s.requests...)
}

// SetSetting changes a setting out of band, simulating drift.
func (s *ConnectionServer) SetSetting(key, section, setting string, value any) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if connection, ok := s.connections[key]; ok {
		sectionMap, _ := connection.body[section].(map[string]any)
		if sectionMap == nil {
			sectionMap = map[string]any{}
			connection.body[section] = sectionMap
		}
		sectionMap[setting] = value
	}
}

// Remove deletes a connection out of band, simulating remote deletion.
func (s *ConnectionServer) Remove(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.connections, key)
}

func (s *ConnectionServer) handle(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.requests = append(s.requests, RecordedRequest{Method: r.Method, Path: r.URL.EscapedPath()})
	s.mu.Unlock()

	if r.Header.Get("Authorization") != TestAPIKey {
		writeConnectionError(w, http.StatusUnauthorized, "AUTH001", "invalid credentials")

		return
	}

	segments := splitPath(r.URL.EscapedPath())
	if len(segments) >= 4 && segments[0] == "api" && segments[1] == "connections" && s.handleDataObjects(w, r, segments) {
		return
	}
	switch {
	case len(segments) == 2 && segments[0] == "api" && segments[1] == "connections" && r.Method == http.MethodPost:
		s.create(w, r)
	case len(segments) == 3 && segments[0] == "api" && segments[1] == "connections":
		key, _ := url.PathUnescape(segments[2])
		switch r.Method {
		case http.MethodGet:
			s.get(w, key)
		case http.MethodPatch:
			s.patch(w, r, key)
		case http.MethodDelete:
			s.delete(w, key)
		default:
			writeConnectionError(w, http.StatusNotFound, "NOT_FOUND", "unknown endpoint")
		}
	default:
		writeConnectionError(w, http.StatusNotFound, "NOT_FOUND", "unknown endpoint")
	}
}

func (s *ConnectionServer) create(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeConnectionError(w, http.StatusBadRequest, "CONN001", "invalid body")

		return
	}

	key, _ := body["connectionKey"].(string)
	technology, _ := body["technology"].(string)
	client, _ := body["clientConfiguration"].(map[string]any)
	credential, _ := client["credential"].(map[string]any)
	if _, known := connectionFixedSettings[technology]; !known || key == "" || credential["authenticationType"] == nil {
		writeConnectionError(w, http.StatusBadRequest, "CONN001", "technology, connectionKey and a credential are required")

		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.connections[key]; exists {
		writeConnectionError(w, http.StatusConflict, "CONN002", "a connection with this key already exists")

		return
	}

	s.creates++
	connection := &storedConnection{body: body, state: "ACTIVATING", busyPolls: s.busyPolls}
	s.connections[key] = connection

	writeJSON(w, http.StatusCreated, s.render(connection))
}

// get returns the connection, advancing it from a busy state to its final
// state, or deleting it, once its busy reads are used up.
func (s *ConnectionServer) get(w http.ResponseWriter, key string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	connection, ok := s.connections[key]
	if !ok {
		writeConnectionError(w, http.StatusNotFound, "CONN004", "connection not found")

		return
	}

	if connection.busyPolls > 0 {
		connection.busyPolls--
	} else {
		switch connection.state {
		case "ACTIVATING", "UPDATING":
			connection.state = s.finalState
		case "DELETING":
			delete(s.connections, key)
			writeConnectionError(w, http.StatusNotFound, "CONN004", "connection not found")

			return
		}
	}

	writeJSON(w, http.StatusOK, s.render(connection))
}

func (s *ConnectionServer) patch(w http.ResponseWriter, r *http.Request, key string) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeConnectionError(w, http.StatusBadRequest, "CONN001", "invalid body")

		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	connection, ok := s.connections[key]
	if !ok {
		writeConnectionError(w, http.StatusNotFound, "CONN004", "connection not found")

		return
	}

	technology, _ := connection.body["technology"].(string)
	for section, value := range body {
		if section == "technology" || section == "connectionKey" || section == "syncOnCreate" {
			writeConnectionError(w, http.StatusBadRequest, "CONN001", section+" cannot be updated")

			return
		}
		settings, _ := value.(map[string]any)
		for setting := range settings {
			if slices.Contains(connectionFixedSettings[technology][section], setting) {
				writeConnectionError(w, http.StatusBadRequest, "CONN001", section+"."+setting+" cannot be updated")

				return
			}
		}
	}

	for section, value := range body {
		settings, isMap := value.(map[string]any)
		stored, storedIsMap := connection.body[section].(map[string]any)
		if !isMap || !storedIsMap {
			connection.body[section] = value

			continue
		}
		for setting, settingValue := range settings {
			stored[setting] = settingValue
		}
	}

	s.patches = append(s.patches, body)
	connection.state = "UPDATING"
	connection.busyPolls = s.busyPolls

	writeJSON(w, http.StatusOK, s.render(connection))
}

func (s *ConnectionServer) delete(w http.ResponseWriter, key string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	connection, ok := s.connections[key]
	if !ok || connection.state == "DELETING" {
		writeConnectionError(w, http.StatusNotFound, "CONN004", "connection not found")

		return
	}

	s.deletes++
	connection.state = "DELETING"
	connection.busyPolls = s.busyPolls

	response := map[string]any{"taskId": "task-" + key}
	if s.teardown != nil {
		response["teardown"] = s.teardown
	}
	writeJSON(w, http.StatusAccepted, response)
}

// render returns a connection as the API does: secrets redacted, and with
// fields and defaults of its own that a configuration never sets.
func (s *ConnectionServer) render(connection *storedConnection) map[string]any {
	encoded, _ := json.Marshal(connection.body)

	var rendered map[string]any
	_ = json.Unmarshal(encoded, &rendered)

	if client, ok := rendered["clientConfiguration"].(map[string]any); ok {
		if credential, ok := client["credential"].(map[string]any); ok {
			for name := range credential {
				if name != "authenticationType" && name != "username" && !strings.HasSuffix(name, "Id") {
					delete(credential, name)
				}
			}
		}
		if _, ok := client["port"]; !ok {
			client["port"] = 443
		}
	}
	if sync, ok := rendered["dataObjectSync"].(map[string]any); ok {
		if _, ok := sync["startTime"]; !ok {
			sync["startTime"] = 0
		}
	}
	delete(rendered, "syncOnCreate")

	rendered["state"] = connection.state
	rendered["health"] = map[string]any{"status": "healthy"}
	rendered["activeTasks"] = []any{}
	rendered["immutaResourcePrefix"] = "immuta"

	return rendered
}

func writeConnectionError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"errors": []map[string]string{{"code": code, "message": message}}})
}
