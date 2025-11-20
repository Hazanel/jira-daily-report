// JIRA Daily Report Generator
//
// This tool fetches JIRA issues from Red Hat JIRA and sends a formatted
// daily report to a Slack channel as a threaded message.
//
// Issues are grouped by person (Assignee or QA Contact depending on status)
// and filtered based on specific criteria. The report uses Slack threads to
// keep the channel clean - showing only the header in the channel with all
// issue details in thread replies.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
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
	Total      int `json:"total"`
	StartAt    int `json:"startAt"`
	MaxResults int `json:"maxResults"`
	Issues     []struct {
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
	// Command-line flags
	serverMode := flag.Bool("server", false, "Run as slash command server instead of daily report")
	flag.Parse()

	// Server mode: Start HTTP server for slash commands
	if *serverMode {
		startSlashCommandServer()
		return
	}

	// Daily report mode: Run once and exit
	runDailyReport()
}

// runDailyReport executes the daily JIRA report and sends to Slack
func runDailyReport() {
	// Configuration: Load from environment variables or use defaults
	jiraURL := os.Getenv("JIRA_URL")
	jiraToken := os.Getenv("JIRA_TOKEN")
	slackBotToken := os.Getenv("SLACK_BOT_TOKEN")
	slackChannel := os.Getenv("SLACK_CHANNEL")

	// Validate required credentials
	if jiraURL == "" || jiraToken == "" || slackBotToken == "" || slackChannel == "" {
		fmt.Println("❌ Missing required credentials")
		fmt.Println("Please set environment variables: JIRA_URL, JIRA_TOKEN, SLACK_BOT_TOKEN, SLACK_CHANNEL")
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

	// Group issues by person and status
	personStatusGroups := buildPersonStatusGroups(issues)

	// Send messages as a thread
	fmt.Printf("📤 Sending report to Slack at %s...\n", time.Now().Format("15:04:05"))

	// Send header as main message to create the thread
	date := time.Now().Format("Jan 2, 2006")
	headerBlocks := []map[string]interface{}{
		{"type": "header", "text": map[string]string{"type": "plain_text", "text": "🧾 Daily JIRA Summary — " + date}},
		{"type": "divider"},
	}

	fmt.Printf("   Creating thread with header...\n")
	threadTS, err := sendToSlackAPI(slackBotToken, slackChannel, "", headerBlocks)
	if err != nil {
		fmt.Printf("❌ Failed to send initial message: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("   ✓ Thread created\n")

	// Send each person's issues organized by status
	err = sendDailyReportThreaded(slackBotToken, slackChannel, threadTS, jiraURL, personStatusGroups)
	if err != nil {
		fmt.Printf("❌ Failed to send threaded report: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\n✅ Successfully sent daily report with %d issues\n", countTotalIssues(issues))
}

// countTotalIssues returns the total number of issues across all responses.
func countTotalIssues(responses []JiraSearchResponse) int {
	count := 0
	for _, resp := range responses {
		count += len(resp.Issues)
	}
	return count
}

// SlackMessageResponse represents the response from Slack's chat.postMessage API
type SlackMessageResponse struct {
	OK      bool   `json:"ok"`
	Error   string `json:"error"`
	TS      string `json:"ts"`      // Thread timestamp
	Channel string `json:"channel"` // Channel ID
}

// sendToSlackAPI sends a message to Slack using the chat.postMessage API.
// Returns the thread timestamp (ts) for threading subsequent messages.
func sendToSlackAPI(botToken, channel, threadTS string, blocks []map[string]interface{}) (string, error) {
	payload := map[string]interface{}{
		"channel":      channel,
		"blocks":       blocks,
		"unfurl_links": false, // Disable automatic link unfurling
		"unfurl_media": false, // Disable automatic media unfurling
	}

	// If threadTS is provided, send as a thread reply
	if threadTS != "" {
		payload["thread_ts"] = threadTS
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequest("POST", "https://slack.com/api/chat.postMessage", bytes.NewBuffer(data))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", botToken))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to post to Slack: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	var slackResp SlackMessageResponse
	if err := json.Unmarshal(bodyBytes, &slackResp); err != nil {
		return "", fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if !slackResp.OK {
		return "", fmt.Errorf("Slack API error: %s", slackResp.Error)
	}

	return slackResp.TS, nil
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
	var allResults []JiraSearchResponse
	startAt := 0
	maxResults := 100 // Fetch in batches of 100

	for {
		// Prepare the search request with pagination
		requestBody := map[string]interface{}{
			"jql":        jql,
			"startAt":    startAt,
			"maxResults": maxResults,
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

		responseBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
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

		allResults = append(allResults, result)

		// Check if we've fetched all results
		if startAt+len(result.Issues) >= result.Total {
			fmt.Printf("      Fetched all %d issues from JIRA\n", result.Total)
			break
		}

		fmt.Printf("      Fetched %d/%d issues, continuing...\n", startAt+len(result.Issues), result.Total)
		startAt += maxResults
	}

	return allResults, nil
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

// PersonStatusGroup represents issues for one person, grouped by status
type PersonStatusGroup struct {
	Person       string
	StatusGroups map[string][]IssueItem
	TotalIssues  int
}

// buildPersonStatusGroups groups issues by person, then by status
func buildPersonStatusGroups(responses []JiraSearchResponse) []PersonStatusGroup {
	// First group by person
	personIssues := make(map[string][]IssueItem)

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

			personIssues[assignee] = append(personIssues[assignee], IssueItem{
				Key:            issue.Key,
				Summary:        issue.Fields.Summary,
				Status:         issue.Fields.Status.Name,
				GitPullRequest: prs,
			})
		}
	}

	// Sort people alphabetically
	var people []string
	for person := range personIssues {
		people = append(people, person)
	}
	sort.Strings(people)

	// Group each person's issues by status
	var result []PersonStatusGroup
	for _, person := range people {
		issues := personIssues[person]
		statusGroups := make(map[string][]IssueItem)

		for _, issue := range issues {
			statusGroups[issue.Status] = append(statusGroups[issue.Status], issue)
		}

		result = append(result, PersonStatusGroup{
			Person:       person,
			StatusGroups: statusGroups,
			TotalIssues:  len(issues),
		})
	}

	return result
}

// sendDailyReportThreaded sends the daily report as threaded messages per person/status
func sendDailyReportThreaded(botToken, channel, threadTS, jiraURL string, personGroups []PersonStatusGroup) error {
	statusOrder := []string{"In Progress", "Modified", "POST", "ON_QA", "MODIFIED", "Open", "Closed", "Archived"}

	messageCount := 0
	separator := "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

	for _, group := range personGroups {
		// Build ONE message with person header + all their statuses
		blocks := []map[string]interface{}{}

		// Add top separator for first person only
		if messageCount == 0 {
			blocks = append(blocks, map[string]interface{}{
				"type": "section",
				"text": map[string]string{
					"type": "mrkdwn",
					"text": separator,
				},
			})
		}

		// Add person header with bottom separator
		blocks = append(blocks, map[string]interface{}{
			"type": "section",
			"text": map[string]string{
				"type": "mrkdwn",
				"text": fmt.Sprintf("*👤 %s* (%d issue(s))\n%s", group.Person, group.TotalIssues, separator),
			},
		})
		// Add all statuses and their issues to the blocks
		for _, status := range statusOrder {
			issues, exists := group.StatusGroups[status]
			if !exists {
				continue
			}

			// Add status header (indented with non-breaking spaces)
			blocks = append(blocks, map[string]interface{}{
				"type": "section",
				"text": map[string]string{
					"type": "mrkdwn",
					"text": fmt.Sprintf("\n\u00A0\u00A0\u00A0📂 *%s* (%d)", status, len(issues)),
				},
			})

			// Add issues for this status (more indented with non-breaking spaces)
			for _, issue := range issues {
				pr := "–"
				if len(issue.GitPullRequest) > 0 {
					var prLinks []string
					for i, prURL := range issue.GitPullRequest {
						prLinks = append(prLinks, fmt.Sprintf("<%s|PR%d>", prURL, i+1))
					}
					pr = strings.Join(prLinks, " ")
				}

				summary := escapeSlackText(issue.Summary)
				if len(summary) > 65 {
					summary = summary[:65] + "..."
				}

				text := fmt.Sprintf("\u00A0\u00A0\u00A0\u00A0\u00A0\u00A0• <%s/browse/%s|*%s*> — %s\n\u00A0\u00A0\u00A0\u00A0\u00A0\u00A0\u00A0\u00A0*Status:* %s  |  *PR:* %s",
					jiraURL, issue.Key, issue.Key, summary, issue.Status, pr)

				blocks = append(blocks, map[string]interface{}{
					"type": "section",
					"text": map[string]string{
						"type": "mrkdwn",
						"text": text,
					},
				})
			}
		}

		// Add any statuses not in predefined order
		for status, issues := range group.StatusGroups {
			found := false
			for _, s := range statusOrder {
				if s == status {
					found = true
					break
				}
			}
			if found {
				continue
			}

			// Add status header (indented with non-breaking spaces)
			blocks = append(blocks, map[string]interface{}{
				"type": "section",
				"text": map[string]string{
					"type": "mrkdwn",
					"text": fmt.Sprintf("\n\u00A0\u00A0\u00A0📂 *%s* (%d)", status, len(issues)),
				},
			})

			// Add issues for this status (more indented with non-breaking spaces)
			for _, issue := range issues {
				pr := "–"
				if len(issue.GitPullRequest) > 0 {
					var prLinks []string
					for i, prURL := range issue.GitPullRequest {
						prLinks = append(prLinks, fmt.Sprintf("<%s|PR%d>", prURL, i+1))
					}
					pr = strings.Join(prLinks, " ")
				}

				summary := escapeSlackText(issue.Summary)
				if len(summary) > 65 {
					summary = summary[:65] + "..."
				}

				text := fmt.Sprintf("\u00A0\u00A0\u00A0\u00A0\u00A0\u00A0• <%s/browse/%s|*%s*> — %s\n\u00A0\u00A0\u00A0\u00A0\u00A0\u00A0\u00A0\u00A0*Status:* %s  |  *PR:* %s",
					jiraURL, issue.Key, issue.Key, summary, issue.Status, pr)

				blocks = append(blocks, map[string]interface{}{
					"type": "section",
					"text": map[string]string{
						"type": "mrkdwn",
						"text": text,
					},
				})
			}
		}

		// Add closing separator
		blocks = append(blocks, map[string]interface{}{
			"type": "section",
			"text": map[string]string{
				"type": "mrkdwn",
				"text": fmt.Sprintf("\n%s", separator),
			},
		})

		// Send the complete message for this person
		messageCount++
		fmt.Printf("   Sending reply %d/%d: %s with all statuses...\n", messageCount, len(personGroups), group.Person)
		_, err := sendToSlackAPI(botToken, channel, threadTS, blocks)
		if err != nil {
			return fmt.Errorf("failed to send message for %s: %w", group.Person, err)
		}
		fmt.Printf("   ✓ Reply %d/%d sent\n", messageCount, len(personGroups))

		// Small delay between people
		if messageCount < len(personGroups) {
			time.Sleep(500 * time.Millisecond)
		}
	}

	return nil
}

// escapeSlackText escapes special characters that have meaning in Slack's mrkdwn format.
// This prevents issues with < and > characters in issue summaries breaking Slack links.
func escapeSlackText(text string) string {
	text = strings.ReplaceAll(text, "&", "&amp;")
	text = strings.ReplaceAll(text, "<", "&lt;")
	text = strings.ReplaceAll(text, ">", "&gt;")
	return text
}
