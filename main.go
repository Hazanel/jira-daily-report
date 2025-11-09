// JIRA Daily Report Generator
//
// This tool fetches JIRA issues from Red Hat JIRA and sends a formatted
// daily report to a Slack channel via webhook.
//
// Issues are grouped by person (Assignee or QA Contact depending on status)
// and filtered based on specific criteria.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

// JiraSearchResponse represents the response from JIRA's search API.
// It contains a list of issues with their relevant fields.
type JiraSearchResponse struct {
	Issues []struct {
		Key    string `json:"key"`
		Fields struct {
			Summary string `json:"summary"`
			Status  struct {
				Name string `json:"name"`
			} `json:"status"`
			Assignee *struct {
				DisplayName string `json:"displayName"`
			} `json:"assignee"`
			// QAContact maps to customfield_12315948 in Red Hat JIRA
			QAContact *struct {
				DisplayName string `json:"displayName"`
			} `json:"customfield_12315948"`
			IssueType struct {
				Name string `json:"name"`
			} `json:"issuetype"`
			Components []struct {
				Name string `json:"name"`
			} `json:"components"`
			Labels []string `json:"labels"`
			// GitPullRequest maps to customfield_12310220 in Red Hat JIRA
			// Can be either a string or an array of strings
			GitPullRequest interface{} `json:"customfield_12310220"`
		} `json:"fields"`
	} `json:"issues"`
}

// IssueItem represents a simplified JIRA issue used for grouping and display.
type IssueItem struct {
	Key            string
	Summary        string
	Status         string
	GitPullRequest []string
}

func main() {
	// Configuration: Load from environment variables or use defaults
	jiraURL := os.Getenv("JIRA_URL")
	jiraToken := os.Getenv("JIRA_TOKEN")
	slackWebhook := os.Getenv("SLACK_WEBHOOK")

	// Validate required credentials
	if jiraURL == "" || jiraToken == "" || slackWebhook == "" {
		fmt.Println("❌ Missing required credentials")
		fmt.Println("Please set environment variables: JIRA_URL, JIRA_TOKEN, SLACK_WEBHOOK")
		os.Exit(1)
	}

	// JQL Query fetches:
	// 1. Issues with status: POST, ON_QA, or MODIFIED
	// 2. Epics that are not Closed (will be filtered for PRs later)
	// Excludes UI-related issues (filtered in code)
	jql := `project = MTV AND (status IN (POST, ON_QA, MODIFIED) OR (type = Epic AND status != Closed)) ORDER BY assignee`

	issues, err := fetchJiraIssues(jiraURL, jiraToken, jql)
	if err != nil {
		fmt.Printf("❌ Failed to fetch JIRA issues: %v\n", err)
		os.Exit(1)
	}

	// Send report to Slack
	fmt.Printf("📤 Sending report to Slack at %s...\n", time.Now().Format("15:04:05"))
	slackPayload := buildSlackBlocks(jiraURL, issues)
	err = sendToSlackWebhook(slackWebhook, slackPayload)
	if err != nil {
		fmt.Printf("❌ Failed to send to Slack: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✅ Successfully sent report with %d issues to Slack at %s\n", countTotalIssues(issues), time.Now().Format("15:04:05"))
}

// countTotalIssues returns the total number of issues across all responses.
func countTotalIssues(responses []JiraSearchResponse) int {
	count := 0
	for _, resp := range responses {
		count += len(resp.Issues)
	}
	return count
}

// sendToSlackWebhook sends a formatted message to Slack using an incoming webhook.
// The payload should contain Slack Block Kit formatted blocks.
func sendToSlackWebhook(webhookURL string, payload map[string]interface{}) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	resp, err := http.Post(webhookURL, "application/json", bytes.NewBuffer(data))
	if err != nil {
		return fmt.Errorf("failed to post to Slack: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Slack webhook returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}
	return nil
}

// extractPRs extracts Pull Request URLs from JIRA's Git Pull Request custom field.
// The field can be either a single string or an array of strings.
func extractPRs(prField interface{}) []string {
	if prField == nil {
		return nil
	}

	switch v := prField.(type) {
	case string:
		if v != "" {
			return []string{v}
		}
	case []interface{}:
		var prs []string
		for _, item := range v {
			if str, ok := item.(string); ok && str != "" {
				prs = append(prs, str)
			}
		}
		return prs
	}
	return nil
}

// isUIRelated checks if an issue is UI-related based on its component or labels.
// UI-related issues are filtered out from the reports.
func isUIRelated(components []struct {
	Name string `json:"name"`
}, labels []string) bool {
	for _, comp := range components {
		if comp.Name == "User Interface" {
			return true
		}
	}
	for _, label := range labels {
		if label == "user-interface" {
			return true
		}
	}
	return false
}

// fetchJiraIssues queries JIRA API and returns matching issues.
// Parameters:
//   - jiraURL: Base URL of the JIRA instance (e.g., https://issues.redhat.com)
//   - jiraToken: Bearer token for authentication
//   - jql: JQL query string to filter issues
//
// Returns up to 500 issues matching the query.
func fetchJiraIssues(jiraURL, jiraToken, jql string) ([]JiraSearchResponse, error) {
	// Prepare the search request
	requestBody := map[string]interface{}{
		"jql":        jql,
		"maxResults": 500,
		"fields": []string{
			"summary",
			"status",
			"assignee",
			"customfield_12315948", // QA Contact
			"issuetype",
			"components",
			"labels",
			"customfield_12310220", // Git Pull Request
		},
	}

	body, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", fmt.Sprintf("%s/rest/api/2/search", jiraURL), bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", jiraToken))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("JIRA API returned %d: %s", resp.StatusCode, string(responseBody))
	}

	var result JiraSearchResponse
	if err := json.Unmarshal(responseBody, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return []JiraSearchResponse{result}, nil
}

// buildSlackBlocks creates a Slack Block Kit payload for the daily report.
//
// Filtering rules:
//   - UI-related issues are excluded
//   - Epics without PRs are excluded
//   - ON_QA and MODIFIED issues are grouped by QA Contact (if available)
//   - Other issues are grouped by Assignee
//
// Slack limits messages to 50 blocks, so we cap at 48 to leave margin.
func buildSlackBlocks(jiraURL string, responses []JiraSearchResponse) map[string]interface{} {
	// Group issues by person (assignee or QA contact)
	grouped := make(map[string][]IssueItem)
	for _, resp := range responses {
		for _, issue := range resp.Issues {
			if isUIRelated(issue.Fields.Components, issue.Fields.Labels) {
				continue
			}

			prs := extractPRs(issue.Fields.GitPullRequest)

			if issue.Fields.IssueType.Name == "Epic" && len(prs) == 0 {
				continue
			}

			assignee := "Unassigned"
			if (issue.Fields.Status.Name == "ON_QA" || issue.Fields.Status.Name == "MODIFIED") && issue.Fields.QAContact != nil {
				assignee = issue.Fields.QAContact.DisplayName
			} else if issue.Fields.Assignee != nil {
				assignee = issue.Fields.Assignee.DisplayName
			}

			grouped[assignee] = append(grouped[assignee], IssueItem{
				Key:            issue.Key,
				Summary:        issue.Fields.Summary,
				Status:         issue.Fields.Status.Name,
				GitPullRequest: prs,
			})
		}
	}

	// Sort assignees alphabetically
	var assignees []string
	for a := range grouped {
		assignees = append(assignees, a)
	}
	sort.Strings(assignees)

	// Build Slack blocks
	date := time.Now().Format("Jan 2, 2006")
	blocks := []map[string]interface{}{
		{"type": "header", "text": map[string]string{"type": "plain_text", "text": "🧾 Daily JIRA Summary — " + date}},
		{"type": "divider"},
	}

	// Track block count to respect Slack's 50 block limit
	blockCount := 2
	const maxBlocks = 48

	for _, assignee := range assignees {
		if blockCount >= maxBlocks {
			break
		}

		// Add assignee header
		blocks = append(blocks, map[string]interface{}{
			"type": "section",
			"text": map[string]string{"type": "mrkdwn", "text": "*👤 " + assignee + "*"},
		})
		blockCount++

		// Add each issue
		for _, issue := range grouped[assignee] {
			if blockCount >= maxBlocks {
				break
			}

			// Format PR links for Slack
			pr := "–"
			if len(issue.GitPullRequest) > 0 {
				var prLinks []string
				for i, prURL := range issue.GitPullRequest {
					prLinks = append(prLinks, fmt.Sprintf("<%s|PR%d>", prURL, i+1))
				}
				pr = strings.Join(prLinks, " ")
			}

			// Escape special characters and truncate long summaries
			summary := escapeSlackText(issue.Summary)
			if len(summary) > 200 {
				summary = summary[:200] + "..."
			}

			text := fmt.Sprintf("<%s/browse/%s|*%s*> — %s\nStatus: *%s* | PR: %s",
				jiraURL, issue.Key, issue.Key, summary, issue.Status, pr)

			blocks = append(blocks, map[string]interface{}{
				"type": "section",
				"text": map[string]string{"type": "mrkdwn", "text": text},
			})
			blockCount++
		}

		// Add divider after each assignee
		blocks = append(blocks, map[string]interface{}{"type": "divider"})
		blockCount++
	}

	return map[string]interface{}{"blocks": blocks}
}

// escapeSlackText escapes special characters that have meaning in Slack's mrkdwn format.
// This prevents issues with < and > characters in issue summaries breaking Slack links.
func escapeSlackText(text string) string {
	text = strings.ReplaceAll(text, "&", "&amp;")
	text = strings.ReplaceAll(text, "<", "&lt;")
	text = strings.ReplaceAll(text, ">", "&gt;")
	return text
}
