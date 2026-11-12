package testutil

import (
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

// DataSource is a data source served by the DomainServer's documented
// GET /dataSource endpoints.
type DataSource struct {
	ID            int
	Name          string
	ConnectionKey string
	NamePath      []string
	// Tags are the tag names of the data source, which put it in a domain
	// assigned dynamically by any of them.
	Tags []string
}

type storedDataSource struct {
	DataSource

	domain string
	// pendingChecks is how many more domain membership checks miss the
	// data source, the way Immuta's background job would.
	pendingChecks int
}

// AddDataSource stores a data source, without a domain.
func (s *DomainServer) AddDataSource(dataSource DataSource) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.dataSources == nil {
		s.dataSources = map[int]*storedDataSource{}
	}
	s.dataSources[dataSource.ID] = &storedDataSource{DataSource: dataSource}
}

// DataSourceDomain returns the domain a data source is in, or "".
func (s *DomainServer) DataSourceDomain(id int) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	if dataSource, ok := s.dataSources[id]; ok {
		return dataSource.domain
	}

	return ""
}

// SetDataSourceDomain moves a data source out of band; "" removes it from
// its domain.
func (s *DomainServer) SetDataSourceDomain(id int, domainID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if dataSource, ok := s.dataSources[id]; ok {
		dataSource.domain = domainID
	}
}

// DelayDomainDataSourceJobs makes data sources added to a domain, manually
// or by a dynamic assignment, miss the next membership checks and domain
// data source listings, the way Immuta's background job would.
func (s *DomainServer) DelayDomainDataSourceJobs(checks int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.jobDelay = checks
}

// handleDataSources serves the documented data source endpoints. It reports
// false for other requests.
func (s *DomainServer) handleDataSources(w http.ResponseWriter, r *http.Request, segments []string) bool {
	switch {
	case len(segments) == 1 && segments[0] == "dataSource" && r.Method == http.MethodGet:
		s.searchDataSources(w, r.URL.Query())
	case len(segments) == 3 && segments[0] == "dataSource" && segments[1] == "name" && r.Method == http.MethodGet:
		name, _ := url.PathUnescape(segments[2])
		s.getDataSourceByName(w, name)
	case len(segments) == 3 && segments[0] == "domain" && segments[1] != "name" && segments[2] == "datasources" && r.Method == http.MethodGet:
		s.listDomainDataSources(w, r.URL.Query(), segments[1])
	case len(segments) == 3 && segments[0] == "domain" && segments[2] == "datasources" && r.Method == http.MethodPost:
		s.addDomainDataSources(w, r, segments[1])
	case len(segments) == 4 && segments[0] == "domain" && segments[2] == "datasources" && r.Method == http.MethodDelete:
		s.removeDomainDataSource(w, segments[1], segments[3])
	case len(segments) == 4 && segments[0] == "collection" && segments[2] == "datasources" && r.Method == http.MethodDelete:
		s.removeCollectionDataSource(w, segments[1], segments[3])
	default:
		return false
	}

	return true
}

func (s *DomainServer) searchDataSources(w http.ResponseWriter, query url.Values) {
	searchText := strings.ToLower(query.Get("searchText"))
	ids := query["dataSourceIds"]
	domainID := query.Get("domainId")

	s.mu.Lock()
	defer s.mu.Unlock()

	var matching []*storedDataSource
	for _, dataSource := range s.dataSources {
		if !strings.Contains(strings.ToLower(dataSource.Name), searchText) ||
			(len(ids) > 0 && !slices.Contains(ids, strconv.Itoa(dataSource.ID))) {
			continue
		}
		if domainID != "" {
			if dataSource.domain != domainID {
				continue
			}
			if dataSource.pendingChecks > 0 {
				dataSource.pendingChecks--

				continue
			}
		}
		matching = append(matching, dataSource)
	}
	slices.SortFunc(matching, func(a, b *storedDataSource) int { return a.ID - b.ID })

	offset, _ := strconv.Atoi(query.Get("offset"))
	size, err := strconv.Atoi(query.Get("size"))
	if err != nil || size < 1 {
		size = 25
	}
	hits := []any{}
	for _, dataSource := range matching[min(offset, len(matching)):min(offset+size, len(matching))] {
		hits = append(hits, dataSourcePayload(dataSource))
	}

	writeJSON(w, http.StatusOK, map[string]any{"count": len(matching), "hits": hits})
}

// listDomainDataSources implements GET /domain/{domainId}/datasources as
// documented, leaving out data sources Immuta is still adding.
func (s *DomainServer) listDomainDataSources(w http.ResponseWriter, query url.Values, domainID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.domains[domainID]; !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Domain not found"})

		return
	}

	var matching []*storedDataSource
	for _, dataSource := range s.dataSources {
		if dataSource.domain != domainID {
			continue
		}
		if dataSource.pendingChecks > 0 {
			dataSource.pendingChecks--

			continue
		}
		matching = append(matching, dataSource)
	}
	slices.SortFunc(matching, func(a, b *storedDataSource) int { return a.ID - b.ID })

	offset, _ := strconv.Atoi(query.Get("offset"))
	size, err := strconv.Atoi(query.Get("size"))
	if err != nil || size < 1 {
		size = 25
	}
	page := []any{}
	for _, dataSource := range matching[min(offset, len(matching)):min(offset+size, len(matching))] {
		page = append(page, map[string]any{"dataSourceId": dataSource.ID, "name": dataSource.Name, "platform": "Snowflake"})
	}

	writeJSON(w, http.StatusOK, map[string]any{"data": page, "total": len(matching)})
}

// assignByTags moves data sources in or out of a domain assigned
// dynamically by the tag names and returns how many were added, kept and
// removed. Added data sources are not listed until the background job is
// done.
func (s *DomainServer) assignByTags(domainID string, tagNames []string) (added, kept, removed int) {
	for _, dataSource := range s.dataSources {
		tagged := slices.ContainsFunc(dataSource.Tags, func(tag string) bool { return slices.Contains(tagNames, tag) })
		switch {
		case tagged && dataSource.domain == domainID:
			kept++
		case tagged && dataSource.domain == "":
			dataSource.domain = domainID
			dataSource.pendingChecks = s.jobDelay
			added++
		case !tagged && dataSource.domain == domainID:
			dataSource.domain = ""
			removed++
		}
	}

	return added, kept, removed
}

func (s *DomainServer) getDataSourceByName(w http.ResponseWriter, name string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, dataSource := range s.dataSources {
		if dataSource.Name == name {
			writeJSON(w, http.StatusOK, dataSourcePayload(dataSource))

			return
		}
	}

	writeJSON(w, http.StatusNotFound, map[string]string{"message": "Data source not found"})
}

// addDomainDataSources implements POST /domain/{domainId}/datasources as
// documented: manual domains only, data sources in no domain yet.
func (s *DomainServer) addDomainDataSources(w http.ResponseWriter, r *http.Request, domainID string) {
	var body []struct {
		DataSourceID *int `json:"dataSourceId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "an array of dataSourceId is required"})

		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	domain, ok := s.domains[domainID]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Domain not found"})

		return
	}
	if domain.AssignmentType != "manual" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "Data sources can only be added to domains with manual assignment"})

		return
	}
	for _, item := range body {
		if item.DataSourceID == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": "dataSourceId must be an integer"})

			return
		}
		dataSource, ok := s.dataSources[*item.DataSourceID]
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{"message": "Data source not found"})

			return
		}
		if dataSource.domain != "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": "Data source is already assigned to a domain"})

			return
		}
	}
	for _, item := range body {
		dataSource := s.dataSources[*item.DataSourceID]
		dataSource.domain = domainID
		dataSource.pendingChecks = s.jobDelay
	}

	writeJSON(w, http.StatusOK, map[string]string{"jobId": "e48e15a0-ef5c-11ed-892c-a75afcd86969"})
}

func (s *DomainServer) removeDomainDataSource(w http.ResponseWriter, domainID, rawID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	id, _ := strconv.Atoi(rawID)
	dataSource, ok := s.dataSources[id]
	if _, domainExists := s.domains[domainID]; !domainExists || !ok || dataSource.domain != domainID {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Data source not found in domain"})

		return
	}
	dataSource.domain = ""

	writeJSON(w, http.StatusOK, map[string]any{"id": id, "name": dataSource.Name})
}

// removeCollectionDataSource implements the DELETE
// /collection/{domainId}/datasources/{dataSourceId} the Immuta UI uses: it
// answers 204, whatever the domain's assignment type.
func (s *DomainServer) removeCollectionDataSource(w http.ResponseWriter, domainID, rawID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	id, _ := strconv.Atoi(rawID)
	dataSource, ok := s.dataSources[id]
	if _, domainExists := s.domains[domainID]; !domainExists || !ok || dataSource.domain != domainID {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Data source not found in domain"})

		return
	}
	dataSource.domain = ""

	w.WriteHeader(http.StatusNoContent)
}

func dataSourcePayload(dataSource *storedDataSource) map[string]any {
	namePath := dataSource.NamePath
	if namePath == nil {
		namePath = []string{}
	}

	return map[string]any{
		"id":                 dataSource.ID,
		"name":               dataSource.Name,
		"connectionKey":      dataSource.ConnectionKey,
		"namePath":           namePath,
		"dataObjectUniqueId": "4eed11f5-1368-4b75-afda-" + strings.Repeat("0", 12-len(strconv.Itoa(dataSource.ID))) + strconv.Itoa(dataSource.ID),
	}
}

// RemoveDataSourcesFromDomains removes every data source from its domain, as
// an operator does before destroying a domain: Immuta does not delete a
// domain that still has data sources.
func (s *DomainServer) RemoveDataSourcesFromDomains() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, source := range s.dataSources {
		source.domain = ""
	}
}
