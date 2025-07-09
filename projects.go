package Cx1ClientGo

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/go-querystring/query"
	"golang.org/x/exp/slices"
)

// Projects
func (c Cx1Client) CreateProject(projectname string, cx1_group_ids []string, tags map[string]string) (Project, error) {
	c.logger.Debugf("Create Project: %v", projectname)
	data := map[string]interface{}{
		"name":        projectname,
		"groups":      []string{},
		"tags":        map[string]string{},
		"criticality": 3,
		"origin":      "",
	}

	if len(tags) > 0 {
		data["tags"] = tags
	}
	if len(cx1_group_ids) > 0 {
		data["groups"] = cx1_group_ids
	}

	jsonBody, err := json.Marshal(data)
	if err != nil {
		return Project{}, err
	}

	var project Project
	response, err := c.sendRequest(http.MethodPost, "/projects", bytes.NewReader(jsonBody), nil)
	if err != nil {
		c.logger.Tracef("Error while creating project %v: %s", projectname, err)
		return project, err
	}

	err = json.Unmarshal(response, &project)

	return project, err
}

func (c Cx1Client) CreateProjectInApplicationWOPolling(projectname string, cx1_group_ids []string, tags map[string]string, applicationId string) (Project, error) {
	c.logger.Debugf("Create Project %v in applicationId %v", projectname, applicationId)
	data := map[string]interface{}{
		"name":        projectname,
		"groups":      []string{},
		"tags":        map[string]string{},
		"criticality": 3,
		"origin":      cxOrigin,
	}

	if len(tags) > 0 {
		data["tags"] = tags
	}
	if len(cx1_group_ids) > 0 {
		data["groups"] = cx1_group_ids
	}

	jsonBody, err := json.Marshal(data)
	if err != nil {
		return Project{}, err
	}

	var project Project
	var response []byte
	if check, _ := c.version.CheckCxOne("3.16.0"); check >= 0 {
		data["applicationIds"] = []string{applicationId}
		jsonBody, err = json.Marshal(data)
		if err != nil {
			return Project{}, err
		}
		response, err = c.sendRequest(http.MethodPost, "/projects", bytes.NewReader(jsonBody), nil)
	} else {
		response, err = c.sendRequest(http.MethodPost, fmt.Sprintf("/projects/application/%v", applicationId), bytes.NewReader(jsonBody), nil)

		if err != nil && err.Error()[0:8] == "HTTP 404" { // At some point, the api /projects/applications will be removed and instead the normal /projects API will do the job.
			data["applicationIds"] = []string{applicationId}
			jsonBody, err = json.Marshal(data)
			if err != nil {
				return Project{}, err
			}
			response, err = c.sendRequest(http.MethodPost, "/projects", bytes.NewReader(jsonBody), nil)
		}
	}

	if err != nil {
		c.logger.Tracef("Error while creating project %v: %s", projectname, err)
		return project, err
	}

	err = json.Unmarshal(response, &project)
	if err != nil {
		return Project{}, err
	}

	return project, err
}

func (c Cx1Client) CreateProjectInApplication(projectname string, cx1_group_ids []string, tags map[string]string, applicationId string) (Project, error) {
	project, err := c.CreateProjectInApplicationWOPolling(projectname, cx1_group_ids, tags, applicationId)
	if err != nil {
		return project, err
	}
	time.Sleep(time.Second)
	return c.ProjectInApplicationPollingByID(project.ProjectID, applicationId)
}

func (c Cx1Client) ProjectInApplicationPollingByID(projectId, applicationId string) (Project, error) {
	return c.ProjectInApplicationPollingByIDWithTimeout(projectId, applicationId, c.consts.ProjectApplicationLinkPollingDelaySeconds, c.consts.ProjectApplicationLinkPollingMaxSeconds)
}

func (c Cx1Client) ProjectInApplicationPollingByIDWithTimeout(projectId, applicationId string, delaySeconds, maxSeconds int) (Project, error) {
	project, err := c.GetProjectByID(projectId)
	pollingCounter := 0
	for err != nil || !slices.Contains(project.Applications, applicationId) {
		if pollingCounter > maxSeconds {
			return project, fmt.Errorf("project %v is not assigned to application ID %v after %d seconds, aborting", projectId, applicationId, maxSeconds)
		}
		c.logger.Debugf("Project is not yet assigned to the application, polling")
		time.Sleep(time.Duration(delaySeconds) * time.Second)
		project, err = c.GetProjectByID(projectId)
		pollingCounter += delaySeconds
	}
	return project, nil
}

// Get up to count # of projects
// behind the scenes this will use the configured pagination (Get/SetPaginationSettings)
func (c Cx1Client) GetProjects(count uint64) ([]Project, error) {
	c.logger.Debugf("Get %d Cx1 Projects", count)
	_, projects, err := c.GetXProjectsFiltered(ProjectFilter{
		BaseFilter: BaseFilter{Limit: c.pagination.Projects},
	}, count)

	return projects, err
}

// Get all of the projects
// behind the scenes this will use the configured pagination (Get/SetPaginationSettings)
// behaves the same as GetProjects(# of projects in the environment)
func (c Cx1Client) GetAllProjects() ([]Project, error) {
	c.logger.Debugf("Get All Cx1 Projects")
	_, projects, err := c.GetAllProjectsFiltered(ProjectFilter{
		BaseFilter: BaseFilter{Limit: c.pagination.Projects},
	})
	return projects, err
}

func (c Cx1Client) GetProjectByID(projectID string) (Project, error) {
	c.logger.Debugf("Getting Project with ID %v...", projectID)
	var project Project

	data, err := c.sendRequest(http.MethodGet, fmt.Sprintf("/projects/%v", projectID), nil, nil)
	if err != nil {
		return project, fmt.Errorf("failed to fetch project %v: %s", projectID, err)
	}

	err = json.Unmarshal([]byte(data), &project)
	if err != nil {
		return project, err
	}

	err = c.GetProjectConfiguration(&project)
	return project, err
}

// case-sensitive exact match for a project name
func (c Cx1Client) GetProjectByName(name string) (Project, error) {
	_, projects, err := c.GetAllProjectsFiltered(ProjectFilter{
		BaseFilter: BaseFilter{Limit: c.pagination.Projects},
		Names:      []string{name},
	})

	if err != nil {
		return Project{}, err
	}

	for _, p := range projects {
		if p.Name == name {
			err = c.GetProjectConfiguration(&p)
			return p, err
		}
	}

	return Project{}, fmt.Errorf("no project matching %v found", name)
}

// Get all projects with names matching the search 'name'
// As of 2024-10-17 this function no longer takes a specific limit as a parameter
// To set limits, offsets, and other parameters directly, use GetProjectsFiltered
func (c Cx1Client) GetProjectsByName(name string) ([]Project, error) {
	c.logger.Debugf("Get Cx1 Projects By Name: %v", name)

	_, projects, err := c.GetAllProjectsFiltered(ProjectFilter{
		BaseFilter: BaseFilter{Limit: c.pagination.Projects},
		Name:       name,
	})

	return projects, err
}

// Get all projects in the group 'groupID' with names matching the search 'name'
func (c Cx1Client) GetProjectsByNameAndGroupID(projectName string, groupID string) ([]Project, error) {
	c.logger.Debugf("Getting projects with name %v of group ID %v...", projectName, groupID)

	_, projects, err := c.GetAllProjectsFiltered(ProjectFilter{
		BaseFilter: BaseFilter{Limit: c.pagination.Projects},
		Name:       projectName,
		Groups:     []string{groupID},
	})

	return projects, err
}

// Underlying function used by many GetApplications* calls
// Returns the total number of matching results plus an array of projects with
// one page of results (from filter.Offset to filter.Offset+filter.Limit)
func (c Cx1Client) GetProjectsFiltered(filter ProjectFilter) (uint64, []Project, error) {
	params, _ := query.Values(filter)

	var ProjectResponse struct {
		BaseFilteredResponse
		Projects []Project
	}

	response, err := c.sendRequest(http.MethodGet, fmt.Sprintf("/projects?%v", params.Encode()), nil, nil)

	if err != nil {
		return ProjectResponse.FilteredTotalCount, ProjectResponse.Projects, err
	}

	err = json.Unmarshal(response, &ProjectResponse)
	return ProjectResponse.FilteredTotalCount, ProjectResponse.Projects, err
}

// Retrieves all projects matching the filter
func (c Cx1Client) GetAllProjectsFiltered(filter ProjectFilter) (uint64, []Project, error) {
	var projects []Project

	count, err := c.GetProjectCountFiltered(filter)
	if err != nil {
		return count, projects, err
	}
	_, projects, err = c.GetXProjectsFiltered(filter, count)
	return count, projects, err
}

// Retrieves the top 'count' projects matching the filter
func (c Cx1Client) GetXProjectsFiltered(filter ProjectFilter, count uint64) (uint64, []Project, error) {
	var projects []Project

	_, projs, err := c.GetProjectsFiltered(filter)
	projects = projs

	for err == nil && count > filter.Offset+filter.Limit && filter.Limit > 0 && uint64(len(projects)) < count {
		filter.Bump()
		_, projs, err = c.GetProjectsFiltered(filter)
		projects = append(projects, projs...)
	}

	if uint64(len(projects)) > count {
		return count, projects[:count], err
	}

	return count, projects, err
}

// convenience
func (p *Project) IsInGroupID(groupId string) bool {
	for _, g := range p.Groups {
		if g == groupId {
			return true
		}
	}
	return false
}

func (p *Project) IsInGroup(group *Group) bool {
	return p.IsInGroupID(group.GroupID)
}

func (c Cx1Client) GetProjectConfiguration(project *Project) error {
	configurations, err := c.GetProjectConfigurationByID(project.ProjectID)
	project.Configuration = configurations
	return err
}

func (c Cx1Client) GetProjectConfigurationByID(projectID string) ([]ConfigurationSetting, error) {
	c.logger.Debugf("Getting project configuration for project %v", projectID)
	var projectConfigurations []ConfigurationSetting
	params := url.Values{
		"project-id": {projectID},
	}
	data, err := c.sendRequest(http.MethodGet, fmt.Sprintf("/configuration/project?%v", params.Encode()), nil, nil)

	if err != nil {
		c.logger.Tracef("Failed to get project configuration for project ID %v: %s", projectID, err)
		return projectConfigurations, err
	}

	err = json.Unmarshal([]byte(data), &projectConfigurations)
	return projectConfigurations, err
}

// UpdateProjectConfiguration updates the configuration of the project addressed by projectID
func (c Cx1Client) UpdateProjectConfiguration(project *Project, settings []ConfigurationSetting) error {
	project.Configuration = settings
	return c.UpdateProjectConfigurationByID(project.ProjectID, settings)
}

func (c Cx1Client) UpdateProjectConfigurationByID(projectID string, settings []ConfigurationSetting) error {
	if len(settings) == 0 {
		return fmt.Errorf("empty list of settings provided")
	}

	params := url.Values{
		"project-id": {projectID},
	}

	jsonBody, err := json.Marshal(settings)
	if err != nil {
		return err
	}

	_, err = c.sendRequest(http.MethodPatch, fmt.Sprintf("/configuration/project?%v", params.Encode()), bytes.NewReader(jsonBody), nil)
	if err != nil {
		c.logger.Tracef("Failed to update project %v configuration: %s", projectID, err)
		return err
	}

	return nil
}

func (c Cx1Client) SetProjectBranchByID(projectID, branch string, allowOverride bool) error {
	var setting ConfigurationSetting
	setting.Key = "scan.handler.git.branch"
	setting.Value = branch
	setting.AllowOverride = allowOverride

	return c.UpdateProjectConfigurationByID(projectID, []ConfigurationSetting{setting})
}

// retrieves all branches for a project
func (c Cx1Client) GetProjectBranchesByID(projectID string) ([]string, error) {
	return c.GetAllProjectBranchesFiltered(ProjectBranchFilter{
		BaseFilter: BaseFilter{Limit: c.pagination.Branches},
		ProjectID:  projectID,
	})
}

// retrieves a page (filter.Offset to filter.Offset+filter.Limit) of branches for a project
func (c Cx1Client) GetProjectBranchesFiltered(filter ProjectBranchFilter) ([]string, error) {
	params, _ := query.Values(filter)
	branches := []string{}

	data, err := c.sendRequest(http.MethodGet, fmt.Sprintf("/projects/branches?%v", params.Encode()), nil, nil)
	if err != nil {
		err = fmt.Errorf("failed to fetch branches matching filter %v: %s", params, err)
		c.logger.Tracef("Error: %s", err)
		return branches, err
	}

	err = json.Unmarshal(data, &branches)
	return branches, err
}

// returns all of a project's branches matching a filter
func (c Cx1Client) GetAllProjectBranchesFiltered(filter ProjectBranchFilter) ([]string, error) {
	var branches []string

	bs, err := c.GetProjectBranchesFiltered(filter)
	branches = bs

	for err == nil && filter.Limit == uint64(len(bs)) && filter.Limit > 0 {
		filter.Bump()
		bs, err = c.GetProjectBranchesFiltered(filter)
		branches = append(branches, bs...)
	}

	return branches, err
}

// retrieves the first X of a project's branches matching a filter
func (c Cx1Client) GetXProjectBranchesFiltered(filter ProjectBranchFilter, count uint64) ([]string, error) {
	var branches []string

	bs, err := c.GetProjectBranchesFiltered(filter)
	branches = bs

	for err == nil && filter.Limit == uint64(len(bs)) && filter.Limit > 0 && uint64(len(branches)) < count {
		filter.Bump()
		bs, err = c.GetProjectBranchesFiltered(filter)
		branches = append(branches, bs...)
	}

	if uint64(len(branches)) > count {
		return branches[:count], err
	}

	return branches, err
}

func (c Cx1Client) GetProjectCount() (uint64, error) {
	c.logger.Debugf("Get Cx1 Projects Count")
	count, _, err := c.GetProjectsFiltered(ProjectFilter{BaseFilter: BaseFilter{Limit: 1}})
	return count, err
}

// returns the number of projects with names matching a search string 'name'
func (c Cx1Client) GetProjectCountByName(name string) (uint64, error) {
	c.logger.Debugf("Get Cx1 Project count by name: %v", name)
	count, _, err := c.GetProjectsFiltered(ProjectFilter{
		BaseFilter: BaseFilter{Limit: 1},
		Name:       name,
	})
	return count, err
}

func (c Cx1Client) GetProjectCountFiltered(filter ProjectFilter) (uint64, error) {
	params, _ := query.Values(filter)
	filter.Limit = 1
	c.logger.Debugf("Get Cx1 Project count matching filter: %v", params.Encode())
	count, _, err := c.GetProjectsFiltered(filter)
	return count, err
}

func (c Cx1Client) ProjectLink(p *Project) string {
	return fmt.Sprintf("%v/projects/%v/overview", c.baseUrl, p.ProjectID)
}

func (c Cx1Client) SetProjectRepositoryByID(projectID, repository string, allowOverride bool) error {
	var setting ConfigurationSetting
	setting.Key = "scan.handler.git.repository"
	setting.Value = repository
	setting.AllowOverride = allowOverride

	return c.UpdateProjectConfigurationByID(projectID, []ConfigurationSetting{setting})
}

func (c Cx1Client) SetProjectPresetByID(projectID, presetName string, allowOverride bool) error {
	var setting ConfigurationSetting
	setting.Key = "scan.config.sast.presetName"
	setting.Value = presetName
	setting.AllowOverride = allowOverride

	return c.UpdateProjectConfigurationByID(projectID, []ConfigurationSetting{setting})
}

func (c Cx1Client) SetProjectLanguageModeByID(projectID, languageMode string, allowOverride bool) error {
	var setting ConfigurationSetting
	setting.Key = "scan.config.sast.languageMode"
	setting.Value = languageMode
	setting.AllowOverride = allowOverride

	return c.UpdateProjectConfigurationByID(projectID, []ConfigurationSetting{setting})
}

func (c Cx1Client) SetProjectFileFilterByID(projectID, filter string, allowOverride bool) error {
	var setting ConfigurationSetting
	setting.Key = "scan.config.sast.filter"
	setting.Value = filter
	setting.AllowOverride = allowOverride

	// TODO - apply the filter across all languages? set up separate calls per engine? engine as param?

	return c.UpdateProjectConfigurationByID(projectID, []ConfigurationSetting{setting})
}

func (c Cx1Client) UpdateProject(project *Project) error {
	c.logger.Debugf("Updating project %v", project.String())

	jsonBody, err := json.Marshal(project)
	if err != nil {
		return err
	}

	_, err = c.sendRequest(http.MethodPut, fmt.Sprintf("/projects/%v", project.ProjectID), bytes.NewReader(jsonBody), nil)
	return err
}

func (c Cx1Client) DeleteProject(p *Project) error {
	c.logger.Debugf("Deleting Project %v", p.String())

	_, err := c.sendRequest(http.MethodDelete, fmt.Sprintf("/projects/%v", p.ProjectID), nil, nil)
	if err != nil {
		return fmt.Errorf("deleting project %v failed: %s", p.String(), err)
	}

	return nil
}

// Get scan schedules for project p, or get all scan schedules if p == nil
func (c Cx1Client) GetScanSchedules(project *Project) ([]ProjectScanSchedule, error) {
	schedules := []ProjectScanSchedule{}

	if project == nil {
		response, err := c.sendRequest(http.MethodGet, "/projects/schedules", nil, nil)
		if err != nil {
			return schedules, err
		}
		err = json.Unmarshal(response, &schedules)
		if err != nil {
			return schedules, err
		}

		for id := range schedules {
			schedules[id].StartTime = schedules[id].NextStartTime.Format("HH:MM")
		}

		return schedules, nil
	}

	return c.GetScanSchedulesByID(project.ProjectID)
}
func (c Cx1Client) GetScanSchedulesByID(projectId string) ([]ProjectScanSchedule, error) {
	schedules := []ProjectScanSchedule{}
	response, err := c.sendRequest(http.MethodGet, fmt.Sprintf("/projects/schedules/%v", projectId), nil, nil)
	if err != nil {
		return schedules, err
	}

	err = json.Unmarshal(response, &schedules)
	if err != nil {
		return schedules, err
	}

	for id := range schedules { // should only be one
		schedules[id].StartTime = schedules[id].NextStartTime.Format("HH:MM")
	}

	return schedules, nil
}

// helper
func prepareScanScheduleBody(s ProjectScanSchedule) ([]byte, error) {
	type ProjectScanScheduleBody struct {
		StartTime string            `json:"start_time"`
		Frequency string            `json:"frequency"`
		Days      []string          `json:"days,omitempty"`
		Active    bool              `json:"active"`
		Engines   []string          `json:"engines"`
		Branch    string            `json:"branch"`
		Tags      map[string]string `json:"tags"`
	}

	schedule := ProjectScanScheduleBody{
		StartTime: s.StartTime,
		Frequency: s.Frequency,
		Days:      s.Days,
		Active:    s.Active,
		Engines:   s.Engines,
		Branch:    s.Branch,
		Tags:      s.Tags,
	}
	return json.Marshal(schedule)
}

func (c Cx1Client) CreateScanSchedule(project *Project, s ProjectScanSchedule) error {
	if project == nil {
		return fmt.Errorf("project cannot be nil")
	}
	return c.CreateScanScheduleByID(project.ProjectID, s)
}
func (c Cx1Client) CreateScanScheduleByID(projectId string, s ProjectScanSchedule) error {
	jsonBody, err := prepareScanScheduleBody(s)
	if err != nil {
		return err
	}
	_, err = c.sendRequest(http.MethodPost, fmt.Sprintf("/projects/schedules/%v", projectId), bytes.NewReader(jsonBody), nil)
	return err
}

func (c Cx1Client) UpdateScanSchedule(project *Project, schedule ProjectScanSchedule) error {
	if project == nil {
		return fmt.Errorf("project cannot be nil")
	}
	return c.UpdateScanScheduleByID(project.ProjectID, schedule)
}
func (c Cx1Client) UpdateScanScheduleByID(projectId string, schedule ProjectScanSchedule) error {
	jsonBody, err := prepareScanScheduleBody(schedule)
	if err != nil {
		return err
	}
	_, err = c.sendRequest(http.MethodPatch, fmt.Sprintf("/projects/schedules/%v", projectId), bytes.NewReader(jsonBody), nil)
	return err
}

func (c Cx1Client) DeleteScanSchedules(project *Project) error {
	return c.DeleteScanSchedulesByID(project.ProjectID)
}
func (c Cx1Client) DeleteScanSchedulesByID(projectId string) error {
	_, err := c.sendRequest(http.MethodDelete, fmt.Sprintf("/projects/schedules/%v", projectId), nil, nil)
	return err
}

func (s ProjectScanSchedule) String() string {
	if s.Frequency == "weekly" {
		return fmt.Sprintf("Project %v scan: weekly on %v at %v", s.ProjectID, strings.Join(s.Days, ","), s.StartTime)
	} else {
		return fmt.Sprintf("Project %v scan: daily at %v", s.ProjectID, s.StartTime)
	}
}

func (p *Project) AssignGroup(group *Group) {
	if p.IsInGroup(group) {
		return
	}
	p.Groups = append(p.Groups, group.GroupID)
}

func (c Cx1Client) GetOrCreateProjectByName(name string) (Project, error) {
	project, err := c.GetProjectByName(name)
	if err == nil {
		return project, nil
	}

	return c.CreateProject(name, []string{}, map[string]string{})
}

func (c Cx1Client) GetOrCreateProjectInApplicationByName(projectName, applicationName string) (Project, Application, error) {
	var application Application
	var project Project
	var err error
	application, err = c.GetApplicationByName(applicationName)
	if err != nil {
		application, err = c.CreateApplication(applicationName)
		if err != nil {
			return project, application, fmt.Errorf("attempt to create project %v in application %v failed, application did not exist and could not be created due to error: %s", projectName, applicationName, err)
		}
	}

	project, err = c.GetProjectByName(projectName)
	if err != nil {
		if err.Error()[:19] == "no project matching" {
			project, err = c.CreateProjectInApplication(projectName, []string{}, map[string]string{}, application.ApplicationID)
			if err != nil {
				return project, application, fmt.Errorf("attempt to create project %v in application %v failed due to error: %s", projectName, applicationName, err)
			}
			return project, application, nil
		} else {
			return project, application, err
		}
	}

	return project, application, nil
}

func (p Project) GetConfigurationByName(configKey string) *ConfigurationSetting {
	return getConfigurationByName(&p.Configuration, configKey)
}

func (c Cx1Client) GetConfigurationByName(config *[]ConfigurationSetting, configKey string) *ConfigurationSetting {
	return getConfigurationByName(config, configKey)
}

func getConfigurationByName(config *[]ConfigurationSetting, configKey string) *ConfigurationSetting {
	for id := range *config {
		if (*config)[id].Key == configKey || (*config)[id].Name == configKey {
			return &((*config)[id])
		}
	}
	return nil
}

func (p *Project) String() string {
	return fmt.Sprintf("[%v] %v", ShortenGUID(p.ProjectID), p.Name)
}

func (p *Project) GetTags() string {
	str := ""
	for key, val := range p.Tags {
		if str == "" {
			str = key + " = " + val
		} else {
			str = str + ", " + key + " = " + val
		}
	}
	return str
}
