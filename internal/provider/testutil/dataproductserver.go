package testutil

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// dataProductUnreadableKeys are definition keys the Request app API accepts
// but does not return when a data product is read.
var dataProductUnreadableKeys = []string{
	"requestFormId", "dataAccessRequestFormId", "maskingExceptionRequestFormId",
	"dataAccessReviewFlow", "maskingExceptionReviewFlow",
}

// TestRequestAppToken is the personal access token accepted by
// DataProductServer, as a bearer token, on the Request app API.
const TestRequestAppToken = "test-request-app-pat"

// DataProductServer is an in-memory implementation of the Request app
// (formerly Marketplace) data product endpoints, served under /marketplace
// with a bearer personal access token, as on the documented regional URLs. It
// also serves the documented Govern GET /domain/{domainId}/datasources, with
// the Govern API key, so that data products can be built from the data
// sources of a domain. Each API rejects the other's credential.
//
// Like the documented API, it keeps data sources apart from the rest of the
// definition, returns them in its own order, does not return review flows or
// request form ids, and adds fields of its own to every data product.
type DataProductServer struct {
	server *httptest.Server

	mu         sync.Mutex
	products   map[string]*storedDataProduct
	nextID     int
	updates    []map[string]any
	setSources int
	requests   []RecordedRequest

	domainSources map[string][]DomainDataSource
	users         []RequestAppUser
	requestForms  []RequestForm
	assets        map[string]*storedAsset
}

// RequestForm is a request form served by the Request app
// GET /api/request-form.
type RequestForm struct {
	ID   string
	Name string
}

// RequestAppUser is a user served by the Request app
// GET /api/metadata/users.
type RequestAppUser struct {
	IAMID        string
	GlobalUserID string
	Username     string
	Name         string
	Email        string
}

// DomainDataSource is a data source served by GET /domain/{domainId}/datasources.
type DomainDataSource struct {
	ID   int
	Name string
}

type storedDataProduct struct {
	definition map[string]any
	sourceIDs  []string
	// reviewFlows are graphs saved with the review flow endpoint.
	reviewFlows map[string]map[string]any
}

// NewDataProductServer starts a fake Request app API.
func NewDataProductServer() *DataProductServer {
	s := &DataProductServer{products: map[string]*storedDataProduct{}, domainSources: map[string][]DomainDataSource{}}
	s.server = httptest.NewServer(http.HandlerFunc(s.handle))

	return s
}

// URL is the base URL of the fake Govern API: the tenant host.
func (s *DataProductServer) URL() string { return s.server.URL }

// RequestAppURL is the base URL of the fake Request app API.
func (s *DataProductServer) RequestAppURL() string { return s.server.URL + "/marketplace" }

// Close shuts the fake API down.
func (s *DataProductServer) Close() { s.server.Close() }

// Product returns a stored definition and its source ids.
func (s *DataProductServer) Product(id string) (map[string]any, []string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	product, ok := s.products[id]
	if !ok {
		return nil, nil, false
	}

	return product.definition, append([]string(nil), product.sourceIDs...), true
}

// Count returns the number of stored data products.
func (s *DataProductServer) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.products)
}

// Updates returns the bodies of every accepted data product update.
func (s *DataProductServer) Updates() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]map[string]any(nil), s.updates...)
}

// DataSourceUpdates returns how many times data sources were replaced.
func (s *DataProductServer) DataSourceUpdates() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.setSources
}

// Requests returns the requests received so far.
func (s *DataProductServer) Requests() []RecordedRequest {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]RecordedRequest(nil), s.requests...)
}

// Rename changes a name out of band, simulating drift.
func (s *DataProductServer) Rename(id, name string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if product, ok := s.products[id]; ok {
		product.definition["name"] = name
	}
}

// SetDataSources replaces data sources out of band, simulating drift.
func (s *DataProductServer) SetDataSources(id string, sourceIDs ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if product, ok := s.products[id]; ok {
		product.sourceIDs = sourceIDs
	}
}

// SetDomainDataSources sets the data sources listed for a domain.
func (s *DataProductServer) SetDomainDataSources(domainID string, dataSources ...DomainDataSource) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.domainSources[domainID] = dataSources
}

// AddUser adds a user to the Request app user search and returns its global
// user ID.
func (s *DataProductServer) AddUser(iamID, username, name, email string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	user := RequestAppUser{
		IAMID:        iamID,
		GlobalUserID: fmt.Sprintf("00000000-0000-4000-9000-%012d", len(s.users)+1),
		Username:     username,
		Name:         name,
		Email:        email,
	}
	s.users = append(s.users, user)

	return user.GlobalUserID
}

// AddRequestForm adds a request form to the Request app request form search
// and returns its id.
func (s *DataProductServer) AddRequestForm(name string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	form := RequestForm{ID: fmt.Sprintf("cform%08d", len(s.requestForms)+1), Name: name}
	s.requestForms = append(s.requestForms, form)

	return form.ID
}

// Remove deletes a data product out of band, simulating remote deletion.
func (s *DataProductServer) Remove(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.products, id)
}

func (s *DataProductServer) handle(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.requests = append(s.requests, RecordedRequest{Method: r.Method, Path: r.URL.EscapedPath()})
	s.mu.Unlock()

	segments := splitPath(r.URL.EscapedPath())

	wantAuthorization := TestAPIKey
	if len(segments) > 0 && segments[0] == "marketplace" {
		wantAuthorization = "Bearer " + TestRequestAppToken
	}
	if r.Header.Get("Authorization") != wantAuthorization {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"message": "Not authenticated.", "error": "Unauthorized", "statusCode": 401})

		return
	}

	if len(segments) == 3 && segments[0] == "domain" && segments[2] == "datasources" && r.Method == http.MethodGet {
		domainID, _ := url.PathUnescape(segments[1])
		s.listDomainDataSources(w, r, domainID)

		return
	}
	if len(segments) == 4 && segments[0] == "marketplace" && segments[1] == "api" && segments[2] == "metadata" &&
		segments[3] == "users" && r.Method == http.MethodGet {
		s.searchUsers(w, r)

		return
	}
	if len(segments) == 3 && segments[0] == "marketplace" && segments[1] == "api" && segments[2] == "request-form" &&
		r.Method == http.MethodGet {
		s.searchRequestForms(w, r)

		return
	}
	if len(segments) >= 4 && segments[0] == "marketplace" && segments[1] == "api" && segments[2] == "asset" {
		s.handleAssetReviewFlows(w, r, segments[3:])

		return
	}
	if len(segments) < 3 || segments[0] != "marketplace" || segments[1] != "api" || segments[2] != "data-product" {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "unknown endpoint"})

		return
	}
	rest := segments[3:]

	switch {
	case len(rest) == 0 && r.Method == http.MethodPost:
		s.create(w, r)
	case len(rest) == 1:
		id, _ := url.PathUnescape(rest[0])
		switch r.Method {
		case http.MethodGet:
			s.get(w, id)
		case http.MethodPut:
			s.update(w, r, id)
		case http.MethodDelete:
			s.delete(w, id)
		default:
			writeJSON(w, http.StatusNotFound, map[string]string{"message": "unknown endpoint"})
		}
	case len(rest) == 3 && rest[1] == "review-flows" && r.Method == http.MethodPut:
		id, _ := url.PathUnescape(rest[0])
		s.putDataProductReviewFlow(w, r, id, rest[2])
	case len(rest) == 2 && rest[1] == "datasources" && r.Method == http.MethodPut:
		id, _ := url.PathUnescape(rest[0])
		s.replaceDataSources(w, r, id)
	case len(rest) == 3 && rest[1] == "datasources" && rest[2] == "all" && r.Method == http.MethodGet:
		id, _ := url.PathUnescape(rest[0])
		s.listDataSources(w, id)
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "unknown endpoint"})
	}
}

func decodeSourceIDs(value any) ([]string, bool) {
	items, ok := value.([]any)
	if !ok {
		return nil, false
	}

	sourceIDs := make([]string, 0, len(items))
	for _, item := range items {
		dataSource, _ := item.(map[string]any)
		sourceID, ok := dataSource["sourceId"].(string)
		if !ok || sourceID == "" {
			return nil, false
		}
		sourceIDs = append(sourceIDs, sourceID)
	}

	return sourceIDs, true
}

// nameInUse must be called with the lock held.
func (s *DataProductServer) nameInUse(name, except string) bool {
	for id, product := range s.products {
		if id != except && strings.EqualFold(product.definition["name"].(string), name) {
			return true
		}
	}

	return false
}

func (s *DataProductServer) create(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid body"})

		return
	}

	name, _ := body["name"].(string)
	domains, _ := body["domains"].([]any)
	metadata, _ := body["metadata"].(map[string]any)
	expert, _ := metadata["subjectMatterExpert"].(map[string]any)
	expertID, _ := expert["id"].(string)
	sourceIDs, validSources := decodeSourceIDs(body["dataSources"])
	_, suspended := body["suspended"]
	if name == "" || len(domains) == 0 || expertID == "" || !validSources || suspended {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "name, metadata.subjectMatterExpert.id, domains and dataSources are required, and suspended is not accepted"})

		return
	}
	// As the documented API, without review-flow graphs, which the provider
	// does not send.
	requestFormID, _ := body["requestFormId"].(string)
	dataAccessFormID, _ := body["dataAccessRequestFormId"].(string)
	maskingFormID, _ := body["maskingExceptionRequestFormId"].(string)
	if requestFormID == "" && (dataAccessFormID == "" || maskingFormID == "") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "Validation error: Provide requestFormId or both dataAccessRequestFormId and maskingExceptionRequestFormId when creating a data product. at \"requestFormId\""})

		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.nameInUse(name, "") {
		writeJSON(w, http.StatusConflict, map[string]string{"message": "Data product name already in use"})

		return
	}

	id, _ := body["id"].(string)
	if id == "" {
		s.nextID++
		id = fmt.Sprintf("00000000-0000-4000-8000-%012d", s.nextID)
	}
	if _, exists := s.products[id]; exists {
		writeJSON(w, http.StatusConflict, map[string]string{"message": "Data product already exists"})

		return
	}

	delete(body, "dataSources")
	body["id"] = id
	body["suspended"] = false
	product := &storedDataProduct{definition: body, sourceIDs: sourceIDs}
	s.products[id] = product

	writeJSON(w, http.StatusOK, renderDataProduct(product))
}

func (s *DataProductServer) get(w http.ResponseWriter, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	product, ok := s.products[id]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Data product not found"})

		return
	}

	writeJSON(w, http.StatusOK, renderDataProduct(product))
}

func (s *DataProductServer) update(w http.ResponseWriter, r *http.Request, id string) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid body"})

		return
	}
	if _, ok := body["dataSources"]; ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "dataSources is not part of the update; use the datasources endpoint"})

		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	product, ok := s.products[id]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Data product not found"})

		return
	}
	if name, ok := body["name"].(string); ok && s.nameInUse(name, id) {
		writeJSON(w, http.StatusConflict, map[string]string{"message": "Data product name already in use"})

		return
	}

	for key, value := range body {
		product.definition[key] = value
	}
	s.updates = append(s.updates, body)

	writeJSON(w, http.StatusOK, renderDataProduct(product))
}

func (s *DataProductServer) replaceDataSources(w http.ResponseWriter, r *http.Request, id string) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid body"})

		return
	}
	sourceIDs, ok := decodeSourceIDs(body["dataSources"])
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "dataSources must be a list of {sourceId}"})

		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	product, found := s.products[id]
	if !found {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Data product not found"})

		return
	}

	product.sourceIDs = sourceIDs
	s.setSources++
	writeJSON(w, http.StatusOK, map[string]any{"id": id})
}

func (s *DataProductServer) listDataSources(w http.ResponseWriter, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	product, ok := s.products[id]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Data product not found"})

		return
	}

	dataSources := make([]map[string]any, 0, len(product.sourceIDs))
	for _, sourceID := range slices.Backward(product.sourceIDs) {
		dataSources = append(dataSources, map[string]any{"sourceId": sourceID, "name": "data source " + sourceID})
	}
	writeJSON(w, http.StatusOK, map[string]any{"dataSources": dataSources, "count": len(dataSources)})
}

func (s *DataProductServer) listDomainDataSources(w http.ResponseWriter, r *http.Request, domainID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	dataSources, ok := s.domainSources[domainID]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Domain not found"})

		return
	}

	searchText := strings.ToLower(r.URL.Query().Get("searchText"))
	var matching []map[string]any
	for _, dataSource := range dataSources {
		if strings.Contains(strings.ToLower(dataSource.Name), searchText) {
			matching = append(matching, map[string]any{
				"dataSourceId": dataSource.ID, "name": dataSource.Name, "platform": "Snowflake", "tags": []any{},
			})
		}
	}

	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	size, err := strconv.Atoi(r.URL.Query().Get("size"))
	if err != nil || size < 1 {
		size = 25
	}
	page := matching[min(offset, len(matching)):min(offset+size, len(matching))]

	writeJSON(w, http.StatusOK, map[string]any{"data": append([]map[string]any{}, page...), "total": len(matching)})
}

func (s *DataProductServer) delete(w http.ResponseWriter, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.products[id]; !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Data product not found"})

		return
	}

	delete(s.products, id)
	writeJSON(w, http.StatusOK, map[string]any{"id": id})
}

// renderDataProduct returns a data product as the API does: without data sources,
// legacy review flows or request form ids, with the review flow graphs saved
// with the review flow endpoint, and with fields of its own.
func renderDataProduct(product *storedDataProduct) map[string]any {
	encoded, _ := json.Marshal(product.definition)

	var rendered map[string]any
	_ = json.Unmarshal(encoded, &rendered)

	for _, key := range dataProductUnreadableKeys {
		delete(rendered, key)
	}
	rendered["createdBy"] = "5b0c1f0e-1111-2222-3333-444455556666"
	rendered["createdAt"] = "2026-10-07T00:00:00Z"
	rendered["updatedAt"] = "2026-10-07T00:00:00Z"
	rendered["stats"] = map[string]any{"dataSourceCount": len(product.sourceIDs)}
	for requestType, graph := range product.reviewFlows {
		rendered[reviewFlowFields[requestType]] = renderReviewFlowGraph(graph)
	}

	return rendered
}

// searchUsers serves GET /api/metadata/users: a partial, case-insensitive
// match on username, name and email, capped at limit (default 100), in the
// shape the documentation shows, including fields the provider ignores.
func (s *DataProductServer) searchUsers(w http.ResponseWriter, r *http.Request) {
	filter := strings.ToLower(r.URL.Query().Get("filter"))
	limit := 100
	if value := r.URL.Query().Get("limit"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid limit"})

			return
		}
		limit = parsed
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	users := []map[string]any{}
	for i, user := range s.users {
		if len(users) == limit {
			break
		}
		if !strings.Contains(strings.ToLower(user.Username+"\n"+user.Name+"\n"+user.Email), filter) {
			continue
		}
		var email any
		if user.Email != "" {
			email = user.Email
		}
		users = append(users, map[string]any{
			"id":             i + 1,
			"iamId":          user.IAMID,
			"globalUserId":   user.GlobalUserID,
			"trackingId":     fmt.Sprintf("tracking-%d", i+1),
			"username":       user.Username,
			"name":           user.Name,
			"email":          email,
			"authorizations": map[string][]string{},
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

// searchRequestForms serves GET /api/request-form: nameFilter matches
// partially, or exactly and case-sensitively with exactMatch=true, in the
// shape the documentation shows, including fields the provider ignores.
func (s *DataProductServer) searchRequestForms(w http.ResponseWriter, r *http.Request) {
	nameFilter := r.URL.Query().Get("nameFilter")
	exactMatch := r.URL.Query().Get("exactMatch") == "true"

	s.mu.Lock()
	defer s.mu.Unlock()

	forms := []map[string]any{}
	for _, form := range s.requestForms {
		if exactMatch && form.Name != nameFilter {
			continue
		}
		if !exactMatch && !strings.Contains(strings.ToLower(form.Name), strings.ToLower(nameFilter)) {
			continue
		}
		forms = append(forms, map[string]any{
			"id":                form.ID,
			"name":              form.Name,
			"description":       nil,
			"associatedDomains": []string{},
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"data": forms,
		"meta": map[string]any{"total": len(forms), "offset": 0, "limit": 100},
	})
}
