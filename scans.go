package Cx1ClientGo

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/google/go-querystring/query"
)

func (c Cx1Client) GetScanByID(scanID string) (Scan, error) {
	var scan Scan

	data, err := c.sendRequest(http.MethodGet, fmt.Sprintf("/scans/%v", scanID), nil, nil)
	if err != nil {
		c.logger.Tracef("Failed to fetch scan with ID %v: %s", scanID, err)
		return scan, fmt.Errorf("failed to fetch scan with ID %v: %s", scanID, err)
	}

	json.Unmarshal([]byte(data), &scan)
	return scan, nil
}
func (c Cx1Client) DeleteScanByID(scanID string) error {
	_, err := c.sendRequest(http.MethodDelete, fmt.Sprintf("/scans/%v", scanID), nil, nil)
	if err != nil {
		return fmt.Errorf("failed to delete scan with ID %v: %s", scanID, err)
	}

	return nil
}
func (c Cx1Client) CancelScanByID(scanID string) error {
	var body struct {
		Status string `json:"status"`
	}
	body.Status = "Canceled"
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return err
	}
	_, err = c.sendRequest(http.MethodPatch, fmt.Sprintf("/scans/%v", scanID), bytes.NewReader(jsonBody), nil)
	if err != nil {
		return fmt.Errorf("failed to delete scan with ID %v: %s", scanID, err)
	}

	return nil
}

func (c Cx1Client) GetAllScans() ([]Scan, error) {
	_, scans, err := c.GetAllScansFiltered(ScanFilter{
		BaseFilter: BaseFilter{Limit: c.pagination.Scans},
	})
	return scans, err
}

func (c Cx1Client) GetScansByProjectIDAndBranch(projectID string, branch string) ([]Scan, error) {
	filter := ScanFilter{
		BaseFilter: BaseFilter{Limit: c.pagination.Scans},
		ProjectID:  projectID,
		Branches:   []string{branch},
	}
	_, scans, err := c.GetAllScansFiltered(filter)
	return scans, err
}

func (c Cx1Client) GetLastScansByStatus(status []string) ([]Scan, error) {
	filter := ScanFilter{
		BaseFilter: BaseFilter{Limit: c.pagination.Scans},
		Statuses:   status,
		Sort:       []string{"-created_at"},
	}
	_, scans, err := c.GetAllScansFiltered(filter)
	return scans, err
}

func (c Cx1Client) GetScansByStatus(status []string) ([]Scan, error) {
	filter := ScanFilter{
		BaseFilter: BaseFilter{Limit: c.pagination.Scans},
		Statuses:   status,
	}
	_, scans, err := c.GetAllScansFiltered(filter)
	return scans, err
}

func (c Cx1Client) GetLastScansByID(projectID string, limit uint64) ([]Scan, error) {
	_, scans, err := c.GetXScansFiltered(ScanFilter{
		BaseFilter: BaseFilter{Limit: c.pagination.Scans},
		ProjectID:  projectID,
		Sort:       []string{"-created_at"},
	}, limit)
	return scans, err
}

// function will be deprecated, use Get*ScansFiltered
func (c Cx1Client) GetLastScansByIDFiltered(projectID string, filter ScanFilter) ([]Scan, error) {
	c.depwarn("GetLastScansByIDFiltered", "GetScansFiltered")
	if filter.Limit == 0 {
		filter.Limit = c.pagination.Scans
	}
	filter.Sort = append(filter.Sort, "-created_at")
	filter.ProjectID = projectID

	_, scans, err := c.GetScansFiltered(filter)
	return scans, err
}

func (c Cx1Client) GetLastScansByStatusAndID(projectID string, limit uint64, status []string) ([]Scan, error) {
	_, scans, err := c.GetXScansFiltered(ScanFilter{
		BaseFilter: BaseFilter{Limit: c.pagination.Scans},
		ProjectID:  projectID,
		Statuses:   status,
		Sort:       []string{"-created_at"},
	}, limit)
	return scans, err
}

func (c Cx1Client) GetLastScansFiltered(filter ScanFilter) ([]Scan, error) {
	c.depwarn("GetLastScansFiltered", "GetScansFiltered")
	filter.Sort = append(filter.Sort, "-created_at")
	_, scans, err := c.GetAllScansFiltered(filter)
	return scans, err
}

// returns the number of scans matching the filter and an array of those scans
// returns one page of data (from filter.Offset to filter.Offset+filter.Limit)
func (c Cx1Client) GetScansFiltered(filter ScanFilter) (uint64, []Scan, error) {
	params, _ := query.Values(filter)

	var scanResponse struct {
		BaseFilteredResponse
		Scans []Scan
	}

	data, err := c.sendRequest(http.MethodGet, fmt.Sprintf("/scans?%v", params.Encode()), nil, nil)
	if err != nil {
		err = fmt.Errorf("failed to fetch scans matching filter %v: %s", params, err)
		c.logger.Tracef("Error: %s", err)
		return scanResponse.FilteredTotalCount, scanResponse.Scans, err
	}

	err = json.Unmarshal(data, &scanResponse)
	return scanResponse.FilteredTotalCount, scanResponse.Scans, err
}

func (c Cx1Client) GetAllScansFiltered(filter ScanFilter) (uint64, []Scan, error) {
	var scans []Scan

	count, ss, err := c.GetScansFiltered(filter)
	scans = ss

	for err == nil && count > filter.Offset+filter.Limit && filter.Limit > 0 {
		filter.Bump()
		_, ss, err = c.GetScansFiltered(filter)
		scans = append(scans, ss...)
	}

	return count, scans, err
}

func (c Cx1Client) GetXScansFiltered(filter ScanFilter, count uint64) (uint64, []Scan, error) {
	var scans []Scan

	_, ss, err := c.GetScansFiltered(filter)
	scans = ss

	for err == nil && count > filter.Offset+filter.Limit && filter.Limit > 0 {
		filter.Bump()
		_, ss, err = c.GetScansFiltered(filter)
		scans = append(scans, ss...)
	}

	if uint64(len(scans)) > count {
		return count, scans[:count], err
	}

	return count, scans, err
}

func (s ScanSummary) TotalCount() uint64 {
	var count uint64
	count = 0
	count += s.SASTCounters.TotalCounter
	count += s.SCACounters.TotalCounter
	count += s.SCAPackagesCounters.TotalCounter
	count += s.KICSCounters.TotalCounter
	count += s.APISecCounters.TotalCounter
	count += s.ContainersCounters.TotalCounter
	count += s.SCAContainersCounters.TotalPackagesCounters

	return count
}

func (s ScanSummary) String() string {
	return fmt.Sprintf("Scan Summary with: %d SAST, %d SCA, %d SCA Packages, %d SCA Container, %d KICS, %d API Security, and %d Containers results",
		s.SASTCounters.TotalCounter,
		s.SCACounters.TotalCounter,
		s.SCAPackagesCounters.TotalCounter,
		s.SCAContainersCounters.TotalPackagesCounters,
		s.KICSCounters.TotalCounter,
		s.APISecCounters.TotalCounter,
		s.ContainersCounters.TotalCounter,
	)
}

func (c Cx1Client) GetScanCount() (uint64, error) {
	c.logger.Debug("Get scan count")
	count, _, err := c.GetScansFiltered(ScanFilter{BaseFilter: BaseFilter{Limit: 1}})
	return count, err
}

func (c Cx1Client) GetScanCountFiltered(filter ScanFilter) (uint64, error) {
	filter.Limit = 1
	params, _ := query.Values(filter)
	c.logger.Debugf("Get scan count matching filter: %v", params.Encode())
	count, _, err := c.GetScansFiltered(filter)
	return count, err
}

func (c Cx1Client) GetScanMetadataByID(scanID string) (ScanMetadata, error) {
	var scanmeta ScanMetadata

	data, err := c.sendRequest(http.MethodGet, fmt.Sprintf("/sast-metadata/%v", scanID), nil, http.Header{})
	if err != nil {
		c.logger.Tracef("Failed to fetch metadata for scan with ID %v: %s", scanID, err)
		return scanmeta, fmt.Errorf("failed to fetch metadata for scan with ID %v: %s", scanID, err)
	}

	json.Unmarshal(data, &scanmeta)
	return scanmeta, nil
}

func (c Cx1Client) GetScanMetricsByID(scanID string) (ScanMetrics, error) {
	c.logger.Debugf("Getting scan metrics for scan %v", scanID)

	var metrics ScanMetrics
	data, err := c.sendRequest(http.MethodGet, fmt.Sprintf("/sast-metadata/%v/metrics", scanID), nil, nil)

	if err != nil {
		c.logger.Tracef("Failed to get scan metrics for scan ID %v: %s", scanID, err)
		return metrics, err
	}

	err = json.Unmarshal([]byte(data), &metrics)
	return metrics, err
}

func (c Cx1Client) GetScanConfigurationByID(projectID, scanID string) ([]ConfigurationSetting, error) {
	c.logger.Debugf("Getting scan configuration for project %v, scan %v", projectID, scanID)
	var scanConfigurations []ConfigurationSetting
	params := url.Values{
		"project-id": {projectID},
		"scan-id":    {scanID},
	}
	data, err := c.sendRequest(http.MethodGet, fmt.Sprintf("/configuration/scan?%v", params.Encode()), nil, nil)

	if err != nil {
		c.logger.Tracef("Failed to get scan configuration for project ID %v, scan ID %v: %s", projectID, scanID, err)
		return scanConfigurations, err
	}

	err = json.Unmarshal([]byte(data), &scanConfigurations)
	return scanConfigurations, err
}

// returns the SAST Aggregate Summaries for a specific scan
// by default this function will group the results by Language
// Use GetAllScanSASTAggregateSummaryFiltered with a custom filter for different groupings and filters
func (c Cx1Client) GetScanSASTAggregateSummaryByID(scanId string) ([]SASTAggregateSummary, error) {
	_, summary, err := c.GetScanSASTAggregateSummaryFiltered(SASTAggregateSummaryFilter{
		BaseFilter: BaseFilter{Limit: c.pagination.SASTAggregate},
		ScanID:     scanId,
		GroupBy:    []string{"LANGUAGE"},
	})
	return summary, err
}

// returns one page of summaries, from filter.Offset to filter.Offset+filter.Limit
// At least that's how it should work, but it seems to ignore paging and just return everything regardless?
func (c Cx1Client) GetScanSASTAggregateSummaryFiltered(filter SASTAggregateSummaryFilter) (uint64, []SASTAggregateSummary, error) {
	params, _ := query.Values(filter)
	var SASTAggregateResponse struct {
		BaseFilteredResponse
		Summaries []SASTAggregateSummary
	}

	c.logger.Debugf("GetScanSASTAggregateSummaryFiltered matching: %v", params.Encode())
	data, err := c.sendRequest(http.MethodGet, fmt.Sprintf("/sast-scan-summary/aggregate?%v", params.Encode()), nil, nil)
	if err != nil {
		return SASTAggregateResponse.TotalCount, SASTAggregateResponse.Summaries, err
	}

	err = json.Unmarshal(data, &SASTAggregateResponse)
	return SASTAggregateResponse.TotalCount, SASTAggregateResponse.Summaries, err
}

/* This endpoint does not actually seem to use paging
// returns all summaries, using paging
func (c Cx1Client) GetAllScanSASTAggregateSummaryFiltered(filter SASTAggregateSummaryFilter) (uint64, []SASTAggregateSummary, error) {
	count, ss, err := c.GetScanSASTAggregateSummaryFiltered(filter)
	summary := ss
	totalcount := count
	for err == nil && count == filter.Limit && filter.Limit > 0 {
		filter.Bump()
		count, ss, err = c.GetScanSASTAggregateSummaryFiltered(filter)
		totalcount += count
		summary = append(summary, ss...)
	}

	return totalcount, summary, err
}

// returns at least desiredcount summaries, using paging
func (c Cx1Client) GetXScanSASTAggregateSummaryFiltered(filter SASTAggregateSummaryFilter, desiredcount uint64) (uint64, []SASTAggregateSummary, error) {
	count, ss, err := c.GetScanSASTAggregateSummaryFiltered(filter)
	summary := ss
	totalcount := count
	for err == nil && count == filter.Limit && totalcount < desiredcount && filter.Limit > 0 {
		filter.Bump()
		count, ss, err = c.GetScanSASTAggregateSummaryFiltered(filter)
		totalcount += count
		summary = append(summary, ss...)
	}

	return totalcount, summary, err
}
*/

func (c Cx1Client) GetScansSummary() (ScanStatusSummary, error) {
	var summaryResponse struct {
		Status ScanStatusSummary
	}

	data, err := c.sendRequest(http.MethodGet, "/scans/summary", nil, http.Header{})
	if err != nil {
		return summaryResponse.Status, err
	}

	err = json.Unmarshal(data, &summaryResponse)
	return summaryResponse.Status, err
}

func (c Cx1Client) GetScanSummaryByID(scanID string) (ScanSummary, error) {
	summaries, err := c.GetScanSummariesByID([]string{scanID})
	if err != nil {
		return ScanSummary{}, err
	}
	if len(summaries) != 1 {
		return ScanSummary{}, fmt.Errorf("error getting scan summaries")
	}
	return summaries[0], nil
}

func (c Cx1Client) GetScanSummariesByID(scanIDs []string) ([]ScanSummary, error) {
	scanIdsString := strings.Join(scanIDs, ",")
	return c.GetScanSummariesFiltered(ScanSummaryFilter{
		ScanIDs: scanIdsString,
		Status:  true,
	})
}

func (c Cx1Client) GetScanSummariesFiltered(filter ScanSummaryFilter) ([]ScanSummary, error) {
	var ScansSummaries struct {
		BaseFilteredResponse
		ScanSum []ScanSummary `json:"scansSummaries"`
	}

	params, _ := query.Values(filter)
	c.logger.Debugf("GetScanSummariesFiltered for scan IDs %v: %v", filter.ScanIDs, params.Encode())
	data, err := c.sendRequest(http.MethodGet, fmt.Sprintf("/scan-summary/?%v", params.Encode()), nil, http.Header{})
	if err != nil {
		c.logger.Tracef("Failed to fetch metadata for scans with IDs %v: %s", filter.ScanIDs, err)
		return []ScanSummary{}, fmt.Errorf("failed to fetch metadata for scans with IDs %v: %s", filter.ScanIDs, err)
	}

	err = json.Unmarshal(data, &ScansSummaries)

	if err != nil {
		return []ScanSummary{}, err
	}
	if ScansSummaries.TotalCount == 0 {
		return []ScanSummary{}, fmt.Errorf("failed to retrieve scan summaries for scans with IDs: %v", filter.ScanIDs)
	}

	if len(ScansSummaries.ScanSum) == 0 {
		c.logger.Tracef("Failed to parse data, 0-len ScanSum.\n%v", string(data))
		return []ScanSummary{}, fmt.Errorf("failed to parse data")
	}

	return ScansSummaries.ScanSum, nil
}

func (c Cx1Client) GetScanLogsByID(scanID, engine string) ([]byte, error) {
	c.logger.Debugf("Fetching scan logs for scan %v", scanID)

	response, err := c.sendRequestRawCx1(http.MethodGet, fmt.Sprintf("/logs/%v/%v", scanID, engine), nil, nil)

	if err != nil {
		c.logger.Tracef("Error retrieving scanlog url: %s", err)
		return []byte{}, err
	}

	enginelogURL := response.Header.Get("Location")
	if enginelogURL == "" {
		return []byte{}, fmt.Errorf("expected location header response not found")
	}

	//c.logger.Tracef("Retrieved url: %v", enginelogURL)
	data, err := c.sendRequestInternal(http.MethodGet, enginelogURL, nil, nil)
	if err != nil {
		c.logger.Tracef("Failed to download logs from %v: %s", enginelogURL, err)
		return []byte{}, nil
	}

	return data, nil
}

func (c Cx1Client) GetScanSourcesByID(scanID string) ([]byte, error) {
	c.logger.Debugf("Fetching scan sources for scan %v", scanID)

	//c.logger.Tracef("Retrieved url: %v", enginelogURL)
	data, err := c.sendRequestInternal(http.MethodGet, fmt.Sprintf("%v/api/repostore/code/%v", c.baseUrl, scanID), nil, nil)
	if err != nil {
		c.logger.Tracef("Failed to download sources from scan %v: %s", scanID, err)
		return []byte{}, nil
	}

	return data, nil
}

func (c Cx1Client) GetScanWorkflowByID(scanID string) ([]WorkflowLog, error) {
	var workflow []WorkflowLog

	data, err := c.sendRequest(http.MethodGet, fmt.Sprintf("/scans/%v/workflow", scanID), nil, http.Header{})
	if err != nil {
		c.logger.Errorf("Failed to fetch workflow for scan with ID %v: %s", scanID, err)
		return []WorkflowLog{}, fmt.Errorf("failed to fetch workflow for scan with ID %v: %s", scanID, err)
	}

	err = json.Unmarshal(data, &workflow)
	return workflow, err
}

func (c Cx1Client) scanProject(scanConfig map[string]interface{}) (Scan, error) {
	scan := Scan{}

	jsonBody, err := json.Marshal(scanConfig)
	if err != nil {
		return scan, err
	}

	data, err := c.sendRequest(http.MethodPost, "/scans", bytes.NewReader(jsonBody), nil)
	if err != nil {
		return scan, err
	}

	err = json.Unmarshal(data, &scan)
	return scan, err
}

func (c Cx1Client) ScanProjectZipByID(projectID, sourceUrl, branch string, settings []ScanConfiguration, tags map[string]string) (Scan, error) {
	jsonBody := map[string]interface{}{
		"project": map[string]interface{}{"id": projectID},
		"type":    "upload",
		"tags":    tags,
		"handler": map[string]interface{}{
			"uploadurl": sourceUrl,
			"branch":    branch,
		},
		"config": settings,
	}

	scan, err := c.scanProject(jsonBody)
	if err != nil {
		return scan, fmt.Errorf("failed to start a zip scan for project %v: %s", projectID, err)
	}
	return scan, err
}

func (c Cx1Client) ScanProjectGitByID(projectID, repoUrl, branch string, settings []ScanConfiguration, tags map[string]string) (Scan, error) {
	jsonBody := map[string]interface{}{
		"project": map[string]interface{}{"id": projectID},
		"type":    "git",
		"tags":    tags,
		"handler": map[string]interface{}{
			"repoUrl": repoUrl,
			"branch":  branch,
		},
		"config": settings,
	}

	scan, err := c.scanProject(jsonBody)
	if err != nil {
		return scan, fmt.Errorf("failed to start a git scan for project %v: %s", projectID, err)
	}
	return scan, err
}

// convenience function
func (c Cx1Client) ScanProjectGitByIDWithHandler(projectID string, handler ScanHandler, settings []ScanConfiguration, tags map[string]string) (Scan, error) {
	jsonBody := map[string]interface{}{
		"project": map[string]interface{}{"id": projectID},
		"type":    "git",
		"tags":    tags,
		"handler": handler,
		"config":  settings,
	}

	scan, err := c.scanProject(jsonBody)
	if err != nil {
		return scan, fmt.Errorf("failed to start a git scan for project %v: %s", projectID, err)
	}
	return scan, err
}

// convenience function
func (c Cx1Client) ScanProjectByID(projectID, sourceUrl, branch, scanType string, settings []ScanConfiguration, tags map[string]string) (Scan, error) {
	if scanType == "upload" {
		return c.ScanProjectZipByID(projectID, sourceUrl, branch, settings, tags)
	} else if scanType == "git" {
		return c.ScanProjectGitByID(projectID, sourceUrl, branch, settings, tags)
	}

	return Scan{}, fmt.Errorf("invalid scanType provided, must be 'upload' or 'git'")
}

// convenience function
func (s *Scan) IsIncremental() (bool, error) {
	for _, scanconfig := range s.Metadata.Configs {
		if scanconfig.ScanType == "sast" {
			if val, ok := scanconfig.Values["incremental"]; ok {
				return val == "true", nil
			}
		}
	}
	return false, fmt.Errorf("Scan %v did not have a sast-engine incremental flag set", s.ScanID)
}

// convenience
func (c Cx1Client) ScanPolling(s *Scan) (Scan, error) {
	return c.ScanPollingWithTimeout(s, false, c.consts.ScanPollingDelaySeconds, c.consts.ScanPollingMaxSeconds)
}

func (c Cx1Client) ScanPollingDetailed(s *Scan) (Scan, error) {
	return c.ScanPollingWithTimeout(s, true, c.consts.ScanPollingDelaySeconds, c.consts.ScanPollingMaxSeconds)
}

func (c Cx1Client) ScanPollingWithTimeout(s *Scan, detailed bool, delaySeconds, maxSeconds int) (Scan, error) {
	c.logger.Infof("Polling status of scan %v", s.ScanID)
	shortId := ShortenGUID(s.ScanID)

	pollingCounter := 0
	var err error
	scan := *s
	for !(scan.Status == "Failed" || scan.Status == "Partial" || scan.Status == "Completed" || scan.Status == "Canceled") { // scan is queueing or running
		scan, err = c.GetScanByID(scan.ScanID)
		if err != nil {
			c.logger.Tracef("Failed to get scan %v status: %s", shortId, err)
			return scan, err
		}
		if detailed {
			workflow, err := c.GetScanWorkflowByID(scan.ScanID)
			if err != nil {
				c.logger.Tracef("Failed to get scan %v workflow: %s", shortId, err)
				return scan, err
			}
			status := "no details"
			if len(workflow) > 0 {
				status = workflow[len(workflow)-1].Info
			}
			c.logger.Infof(" - scan %v = %v: %v", shortId, scan.Status, status)
		} else {
			c.logger.Infof(" - %v: %v", shortId, scan.Status)
		}
		if scan.Status == "Failed" || scan.Status == "Partial" || scan.Status == "Completed" || scan.Status == "Canceled" {
			break
		}

		if maxSeconds != 0 && pollingCounter >= maxSeconds {
			return scan, fmt.Errorf("scan %v polling reached %d seconds, aborting - use cx1client.get/setclientvars to change", shortId, pollingCounter)
		}
		time.Sleep(time.Duration(delaySeconds) * time.Second)
		pollingCounter += delaySeconds
	}
	return scan, nil
}

func (c Cx1Client) GetUploadURL() (string, error) {
	c.logger.Debug("Get Cx1 Upload URL")
	response, err := c.sendRequest(http.MethodPost, "/uploads", nil, nil)

	if err != nil {
		c.logger.Tracef("Unable to get Upload URL: %s", err)
		return "", err
	}

	var jsonBody map[string]interface{}

	err = json.Unmarshal(response, &jsonBody)
	if err != nil {
		c.logger.Tracef("Error: %s", err)
		c.logger.Tracef("Input was: %s", string(response))
		return "", err
	} else {
		return jsonBody["url"].(string), nil
	}
}

func (c Cx1Client) PutFile(URL string, filename string) (string, error) {
	res, err := c.PutFileRaw(URL, filename)
	if err != nil {
		c.logger.Tracef("Error: %s", err)
		return "", err
	}
	defer res.Body.Close()

	resBody, err := io.ReadAll(res.Body)

	if err != nil {
		c.logger.Tracef("Error: %s", err)
		return "", err
	}

	return string(resBody), nil
}

func (c Cx1Client) PutFileRaw(URL string, filename string) (*http.Response, error) {
	c.logger.Tracef("Putting file %v to %v", filename, URL)

	fileContents, err := os.ReadFile(filename)
	if err != nil {
		c.logger.Tracef("Failed to Read the File %v: %s", filename, err)
		return nil, err
	}

	header := http.Header{}
	header.Add("Content-Type", "application/zip")

	cx1_req, err := c.createRequest(http.MethodPut, URL, bytes.NewReader(fileContents), &header, nil)
	if err != nil {
		return nil, err
	}
	cx1_req.ContentLength = int64(len(fileContents))

	return c.httpClient.Do(cx1_req)
}

func (c Cx1Client) UploadBytesForProjectByID(projectID string, fileContents *[]byte) (string, error) {
	// this function exists only for compatibility with a generic interface supporting both SAST and Cx1
	return c.UploadBytes(fileContents)
}

// creates upload URL, uploads, returns upload URL
func (c Cx1Client) UploadBytes(fileContents *[]byte) (string, error) {
	uploadUrl, err := c.GetUploadURL()
	if err != nil {
		return "", err
	}

	header := http.Header{}
	header.Add("Content-Type", "application/zip")

	cx1_req, err := c.createRequest(http.MethodPut, uploadUrl, bytes.NewReader(*fileContents), &header, nil)
	if err != nil {
		return "", err
	}
	cx1_req.ContentLength = int64(len(*fileContents))

	res, err := c.httpClient.Do(cx1_req)
	if err != nil {
		c.logger.Tracef("Error: %s", err)
		return "", err
	}
	defer res.Body.Close()

	return uploadUrl, nil
}

func (s *Scan) String() string {
	return fmt.Sprintf("[%v] %v", ShortenGUID(s.ScanID), s.ProjectName)
}

func (s ScanStatusSummary) String() string {
	return fmt.Sprintf("Summary of all scan statuses: %d queued, %d running, %d completed, %d partial, %d canceled, %d failed", s.Queued, s.Running, s.Completed, s.Partial, s.Canceled, s.Failed)
}

func (s ScanMetrics) HasLanguage(lang string) bool {
	for scanLang := range s.ScannedFilesPerLanguage {
		if strings.EqualFold(scanLang, lang) {
			return true
		}
	}
	return false
}

func (s ScanMetrics) GetLanguages() []string {
	langs := []string{}
	for scanLang := range s.ScannedFilesPerLanguage {
		langs = append(langs, scanLang)
	}
	return langs
}

/* misc future stuff

Listing of files in a scan:
	https://deu.ast.checkmarx.net/api/repostore/project-tree/74328f1f-94ec-452f-8f1a-047d76f6764e
*/
