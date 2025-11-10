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

// Filtering configuration - add or remove items to customize what issues are excluded from reports
var (
	// Components to exclude from the report (case-sensitive)
	excludedComponents = []string{
		"User Interface",
	}

	// Labels to exclude from the report (case-sensitive)
	excludedLabels = []string{
		"user-interface",
		"mtv-storage-offload",
		"mtv-copy-offload",
	}
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

	fmt.Printf("📊 Fetched %d total issues from JIRA\n", countTotalIssues(issues))

	// Build and send Slack messages (may create multiple if too many issues)
	slackPayloads := buildSlackBlocks(jiraURL, issues)

	fmt.Printf("📤 Sending %d message(s) to Slack at %s...\n", len(slackPayloads), time.Now().Format("15:04:05"))
	for i, payload := range slackPayloads {
		blocks := payload["blocks"].([]map[string]interface{})
		fmt.Printf("   Message %d/%d: %d blocks\n", i+1, len(slackPayloads), len(blocks))
		err = sendToSlackWebhook(slackWebhook, payload)
		if err != nil {
			fmt.Printf("❌ Failed to send message %d/%d to Slack: %v\n", i+1, len(slackPayloads), err)
			os.Exit(1)
		}
		fmt.Printf("   ✓ Message %d/%d sent successfully\n", i+1, len(slackPayloads))
		// Small delay between messages to ensure proper ordering
		if i < len(slackPayloads)-1 {
			time.Sleep(500 * time.Millisecond)
		}
	}

	if len(slackPayloads) > 1 {
		fmt.Printf("\n✅ Successfully sent %d messages with %d issues to Slack at %s\n", len(slackPayloads), countTotalIssues(issues), time.Now().Format("15:04:05"))
	} else {
		fmt.Printf("\n✅ Successfully sent report with %d issues to Slack at %s\n", countTotalIssues(issues), time.Now().Format("15:04:05"))
	}
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

// shouldFilterOut checks if an issue should be excluded from the report.
// Uses the global excludedComponents and excludedLabels variables defined at the top of the file.
func shouldFilterOut(components []struct {
	Name string `json:"name"`
}, labels []string) bool {
	// Check if any component matches excluded list
	for _, comp := range components {
		for _, excluded := range excludedComponents {
			if comp.Name == excluded {
				return true
			}
		}
	}

	// Check if any label matches excluded list
	for _, label := range labels {
		for _, excluded := range excludedLabels {
			if label == excluded {
				return true
			}
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

// buildSlackBlocks creates Slack Block Kit payloads for the daily report.
// Returns multiple payloads if the report is too large for a single message.
//
// Filtering rules:
//   - UI-related issues are excluded
//   - Epics without PRs are excluded
//   - ON_QA and MODIFIED issues are grouped by QA Contact (if available)
//   - Other issues are grouped by Assignee
//
// Slack limits messages to 50 blocks, so we cap at 48 per message.
func buildSlackBlocks(jiraURL string, responses []JiraSearchResponse) []map[string]interface{} {
	// Group issues by person (assignee or QA contact)
	grouped := make(map[string][]IssueItem)
	for _, resp := range responses {
		for _, issue := range resp.Issues {
			if shouldFilterOut(issue.Fields.Components, issue.Fields.Labels) {
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

	// Build Slack blocks (may create multiple messages)
	date := time.Now().Format("Jan 2, 2006")
	var allPayloads []map[string]interface{}
	var currentBlocks []map[string]interface{}
	blockCount := 0
	const maxBlocks = 48
	messageNum := 1
	assigneeIdx := 0

	// Helper function to start a new message
	startNewMessage := func(partNum int, totalParts int) {
		if partNum == 1 {
			// First message gets the full header
			currentBlocks = []map[string]interface{}{
				{"type": "header", "text": map[string]string{"type": "plain_text", "text": "🧾 Daily JIRA Summary — " + date}},
				{"type": "divider"},
			}
			blockCount = 2
		} else {
			// Subsequent messages have no header, just continue
			currentBlocks = []map[string]interface{}{}
			blockCount = 0
		}
	}

	// Helper function to finalize current message
	finalizeMessage := func() {
		if len(currentBlocks) > 0 { // Only save if there's any content
			allPayloads = append(allPayloads, map[string]interface{}{"blocks": currentBlocks})
		}
	}

	// Start first message
	startNewMessage(1, 1)

	for assigneeIdx < len(assignees) {
		assignee := assignees[assigneeIdx]
		issues := grouped[assignee]

		// Check if we have room for at least the assignee header + one issue + divider (3 blocks)
		if blockCount+3 > maxBlocks {
			// Finalize current message and start a new one
			finalizeMessage()
			messageNum++
			startNewMessage(messageNum, 1)
		}

		// Add assignee header
		currentBlocks = append(currentBlocks, map[string]interface{}{
			"type": "section",
			"text": map[string]string{"type": "mrkdwn", "text": "*👤 " + assignee + "*"},
		})
		blockCount++

		// Add issues for this assignee
		issueIdx := 0
		for issueIdx < len(issues) {
			if blockCount+1 > maxBlocks {
				// No room for more issues, finalize and start new message
				// Add divider before finalizing
				currentBlocks = append(currentBlocks, map[string]interface{}{"type": "divider"})
				finalizeMessage()
				messageNum++
				startNewMessage(messageNum, 1)

				// Add assignee header again in new message
				currentBlocks = append(currentBlocks, map[string]interface{}{
					"type": "section",
					"text": map[string]string{"type": "mrkdwn", "text": "*👤 " + assignee + "* (continued)"},
				})
				blockCount++
			}

			issue := issues[issueIdx]

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

			currentBlocks = append(currentBlocks, map[string]interface{}{
				"type": "section",
				"text": map[string]string{"type": "mrkdwn", "text": text},
			})
			blockCount++
			issueIdx++
		}

		// Add divider after each assignee
		currentBlocks = append(currentBlocks, map[string]interface{}{"type": "divider"})
		blockCount++

		assigneeIdx++
	}

	// Finalize the last message
	finalizeMessage()

	return allPayloads
}

// escapeSlackText escapes special characters that have meaning in Slack's mrkdwn format.
// This prevents issues with < and > characters in issue summaries breaking Slack links.
func escapeSlackText(text string) string {
	text = strings.ReplaceAll(text, "&", "&amp;")
	text = strings.ReplaceAll(text, "<", "&lt;")
	text = strings.ReplaceAll(text, ">", "&gt;")
	return text
}
