package Cx1ClientGo

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

/*
	This is separate from audit.go to split the functions that require a Web-Audit Session from those that do not.
	This file contains the generic query-related functions that do not need a valid audit session.
*/

// this struct is used specifically for the to-be-deprecated /cx-audit/queries endpoint
type AuditQuery_v312 struct {
	QueryID            uint64 `json:"Id,string"`
	Level              string
	LevelID            string `json:"-"`
	Path               string
	Modified           string
	Source             string
	Name               string
	Group              string
	Language           string `json:"lang"`
	Severity           string
	Cwe                int64
	IsExecutable       bool
	CxDescriptionId    int64
	QueryDescriptionId string
	Key                string
	Title              string
}

func (q AuditQuery_v312) ToQuery() SASTQuery {
	return SASTQuery{
		QueryID:            q.QueryID,
		Level:              q.Level,
		LevelID:            q.LevelID,
		Path:               q.Path,
		Modified:           q.Modified,
		Source:             q.Source,
		Name:               q.Name,
		Group:              q.Group,
		Language:           q.Language,
		Severity:           q.Severity,
		CweID:              q.Cwe,
		IsExecutable:       q.IsExecutable,
		QueryDescriptionId: q.CxDescriptionId,
		Custom:             q.Level != AUDIT_QUERY_PRODUCT,
		EditorKey:          q.Key,
		SastID:             0,
	}
}

func (c Cx1Client) GetQueriesByLevelID(level, levelId string) (SASTQueryCollection, error) {
	c.depwarn("GetQueriesByLevelID", "GetAuditQueriesByLevelID")
	c.logger.Debugf("Get all queries for %v", level)

	var url string
	collection := SASTQueryCollection{}
	var queries_v312 []AuditQuery_v312
	var queries []SASTQuery
	switch level {
	case AUDIT_QUERY_TENANT:
		url = "/cx-audit/queries"
	case AUDIT_QUERY_PROJECT:
		url = fmt.Sprintf("/cx-audit/queries?projectId=%v", levelId)
	default:
		return collection, fmt.Errorf("invalid level %v, options are currently: Corp or Project", level)
	}

	response, err := c.sendRequest(http.MethodGet, url, nil, nil)
	if err != nil {
		return collection, err
	}

	err = json.Unmarshal(response, &queries_v312)
	if err != nil {
		return collection, err
	}

	applicationId := ""

	for id := range queries_v312 {
		switch queries_v312[id].Level {
		case AUDIT_QUERY_TENANT:
			queries_v312[id].LevelID = c.QueryTypeTenant()
		case AUDIT_QUERY_PROJECT:
			queries_v312[id].LevelID = levelId
		case AUDIT_QUERY_APPLICATION:
			if applicationId == "" {
				project, err := c.GetProjectByID(levelId)
				if err != nil {
					return collection, fmt.Errorf("failed to retrieve project with ID %v", levelId)
				}
				if len(project.Applications) == 0 {
					return collection, fmt.Errorf("project %v has an application-level query defined, but has no application associated", project.String())
				} else if len(project.Applications) > 1 {
					return collection, fmt.Errorf("project %v has an application-level query defined, but has multiple application associated", project.String())
				}
				applicationId = project.Applications[0]
			}
			queries_v312[id].LevelID = applicationId
		case AUDIT_QUERY_PRODUCT:
			queries_v312[id].LevelID = c.QueryTypeProduct()
		}

		queries = append(queries, queries_v312[id].ToQuery())
	}

	collection.AddQueries(&queries)

	return collection, nil
}

func (c Cx1Client) GetQueries() (SASTQueryCollection, error) {
	c.depwarn("GetQueries", "Get(SAST|IAC)QueryCollection")
	return c.GetSASTQueryCollection()
}

func (c Cx1Client) GetPresetQueries() (SASTQueryCollection, error) {
	//c.depwarn("GetPresetQueries", "Get(SAST|IAC)PresetQueries")
	queries := []SASTQuery{}

	collection := SASTQueryCollection{}
	response, err := c.sendRequest(http.MethodGet, "/presets/queries", nil, nil)
	if err != nil {
		return collection, err
	}

	err = json.Unmarshal(response, &queries)
	if err != nil {
		c.logger.Tracef("Failed to parse %v", string(response))
	}

	for i := range queries {
		queries[i].IsExecutable = true // all queries in the preset are executable

		if queries[i].Custom {
			queries[i].Level = c.QueryTypeTenant()
			queries[i].LevelID = c.QueryTypeTenant()
		} else {
			queries[i].Level = c.QueryTypeProduct()
			queries[i].LevelID = c.QueryTypeProduct()
		}
	}
	collection.AddQueries(&queries)

	return collection, err
}

func (c Cx1Client) GetQueryMappings() (map[uint64]uint64, error) {
	var mapping map[uint64]uint64 = make(map[uint64]uint64)
	var responsemap struct {
		Mappings []struct {
			AstId  uint64 `json:"astId,string"`
			SastId uint64 `json:"sastId,string"`
		} `json:"mappings"`
	}

	response, err := c.sendRequest(http.MethodGet, "/queries/mappings", nil, nil)
	if err != nil {
		return mapping, err
	}

	err = json.Unmarshal(response, &responsemap)
	if err != nil {
		return mapping, err
	}

	for _, qm := range responsemap.Mappings {
		mapping[qm.SastId] = qm.AstId
	}

	return mapping, nil

}

// convenience
func (c Cx1Client) GetSeverityID(severity string) uint {
	return GetSeverityID(severity)
}

func GetSeverityID(severity string) uint {
	switch strings.ToUpper(severity) {
	case "INFO":
		return 0
	case "INFORMATION":
		return 0
	case "LOW":
		return 1
	case "MEDIUM":
		return 2
	case "HIGH":
		return 3
	case "CRITICAL":
		return 4
	}
	return 0
}

func (c Cx1Client) GetSeverity(severity uint) string {
	return GetSeverity(severity)
}

func (c Cx1Client) GetCx1QueryFromSAST(sastId uint64, language, group, name string, mapping *map[uint64]uint64, qc *SASTQueryCollection) *SASTQuery {
	if cx1id, ok := (*mapping)[sastId]; ok {
		return qc.GetQueryByID(cx1id)
	}
	return qc.GetQueryByName(language, group, name)
}

func GetSeverity(severity uint) string {
	switch severity {
	case 0:
		return "Info"
	case 1:
		return "Low"
	case 2:
		return "Medium"
	case 3:
		return "High"
	case 4:
		return "Critical"
	}
	return "Unknown"
}

func (q *SASTQuery) MergeQuery(nq SASTQuery) {
	if q.QueryID == 0 && nq.QueryID != 0 {
		q.QueryID = nq.QueryID
	}
	if q.Path == "" && nq.Path != "" {
		q.Path = nq.Path
	}
	if q.EditorKey == "" && nq.EditorKey != "" {
		q.EditorKey = nq.EditorKey
	}
	if q.Level == "" && nq.Level != "" {
		q.Level = nq.Level
	}
	if q.LevelID == "" && nq.LevelID != "" {
		q.LevelID = nq.LevelID
	}
	if q.Source == "" && nq.Source != "" {
		q.Source = nq.Source
	}
}

func (q *IACQuery) MergeQuery(nq IACQuery) {
	if q.QueryID == "" && nq.QueryID != "" {
		q.QueryID = nq.QueryID
	}
	if q.Path == "" && nq.Path != "" {
		q.Path = nq.Path
	}
	if q.Level == "" && nq.Level != "" {
		q.Level = nq.Level
	}
	if q.LevelID == "" && nq.LevelID != "" {
		q.LevelID = nq.LevelID
	}
	if q.Source == "" && nq.Source != "" {
		q.Source = nq.Source
	}
}

func (q SASTQuery) StringDetailed() string {
	var scope string
	switch q.Level {
	case AUDIT_QUERY_PRODUCT:
		scope = "Product"
	case AUDIT_QUERY_TENANT:
		scope = "Tenant"
	default:
		scope = fmt.Sprintf("%v %v", q.Level, ShortenGUID(q.LevelID))
	}
	return fmt.Sprintf("%v: %v -> %v -> %v, %v risk [ID %v, Key %v]", scope, q.Language, q.Group, q.Name, q.Severity, ShortenGUID(strconv.FormatUint(q.QueryID, 10)), ShortenGUID(q.EditorKey))
}

func (q SASTQuery) String() string {
	return fmt.Sprintf("[%d] %v -> %v -> %v", q.QueryID, q.Language, q.Group, q.Name)
}
func (q IACQuery) String() string {
	return fmt.Sprintf("[%v] %v -> %v -> %v", ShortenGUID(q.QueryID), q.Technology, q.Group, q.Name)
}
func (q IACQuery) StringDetailed() string {
	var scope string
	switch q.Level {
	case AUDIT_QUERY_PRODUCT:
		scope = "Product"
	case AUDIT_QUERY_TENANT:
		scope = "Tenant"
	default:
		scope = fmt.Sprintf("%v %v", q.Level, ShortenGUID(q.LevelID))
	}
	return fmt.Sprintf("%v: %v -> %v -> %v, %v risk [ID %v]", scope, q.Technology, q.Group, q.Name, q.Severity, ShortenGUID(q.QueryID))
}

func (q SASTQuery) GetMetadata() AuditQueryMetadata {
	return AuditQueryMetadata{
		Cwe:             q.CweID,
		IsExecutable:    q.IsExecutable,
		CxDescriptionID: q.QueryDescriptionId,
		Language:        q.Language,
		Group:           q.Group,
		Severity:        q.Severity,
		SastID:          q.SastID,
		Name:            q.Name,
	}
}

func (c Cx1Client) QueryLink(q *SASTQuery) string {
	return fmt.Sprintf("%v/audit/?queryid=%d", c.baseUrl, q.QueryID)
}
