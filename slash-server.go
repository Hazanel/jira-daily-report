// Slack Slash Command Server
//
// This server handles Slack slash commands to filter JIRA issues by user.
// Users can run commands like:
//   /issues                  - Shows YOUR OWN issues (auto-detected from Slack)

// The server fetches fresh JIRA data for each request and returns
// an ephemeral message (only visible to the user who invoked it).
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

// SlackSlashCommand represents the payload Slack sends to slash command endpoints
type SlackSlashCommand struct {
	Token       string `json:"token"`
	TeamID      string `json:"team_id"`
	TeamDomain  string `json:"team_domain"`
	ChannelID   string `json:"channel_id"`
	ChannelName string `json:"channel_name"`
	UserID      string `json:"user_id"`
	UserName    string `json:"user_name"`
	Command     string `json:"command"`
	Text        string `json:"text"`
	ResponseURL string `json:"response_url"`
}

// SlackSlashResponse represents the response sent back to Slack
type SlackSlashResponse struct {
	ResponseType string                   `json:"response_type"` // "ephemeral" or "in_channel"
	Text         string                   `json:"text,omitempty"`
	Blocks       []map[string]interface{} `json:"blocks,omitempty"`
}

// SlackUserInfoResponse represents the response from Slack's users.info API
type SlackUserInfoResponse struct {
	OK   bool `json:"ok"`
	User struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		RealName string `json:"real_name"`
		Profile  struct {
			DisplayName string `json:"display_name"`
			RealName    string `json:"real_name"`
			Email       string `json:"email"`
		} `json:"profile"`
	} `json:"user"`
	Error string `json:"error,omitempty"`
}

// startSlashCommandServer starts an HTTP server to handle Slack slash commands
func startSlashCommandServer() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	slackSigningSecret := os.Getenv("SLACK_SIGNING_SECRET")
	if slackSigningSecret == "" {
		fmt.Println("⚠️  Warning: SLACK_SIGNING_SECRET not set. Request verification disabled.")
		fmt.Println("   For production, set this to verify requests are from Slack.")
	}

	http.HandleFunc("/slack/issues", handleMyIssuesCommand)
	http.HandleFunc("/health", handleHealthCheck)

	fmt.Printf("🚀 Slash command server starting on port %s...\n", port)
	fmt.Printf("📍 Endpoint: http://localhost:%s/slack/issues\n", port)
	fmt.Println("✅ Ready to receive Slack commands!")

	if err := http.ListenAndServe(":"+port, nil); err != nil {
		fmt.Printf("❌ Server error: %v\n", err)
		os.Exit(1)
	}
}

// handleHealthCheck provides a simple health check endpoint
func handleHealthCheck(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

// handleMyIssuesCommand processes the /issues slash command
func handleMyIssuesCommand(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse the form data from Slack
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Failed to parse form", http.StatusBadRequest)
		return
	}

	cmd := SlackSlashCommand{
		Token:       r.FormValue("token"),
		TeamID:      r.FormValue("team_id"),
		TeamDomain:  r.FormValue("team_domain"),
		ChannelID:   r.FormValue("channel_id"),
		ChannelName: r.FormValue("channel_name"),
		UserID:      r.FormValue("user_id"),
		UserName:    r.FormValue("user_name"),
		Command:     r.FormValue("command"),
		Text:        r.FormValue("text"),
		ResponseURL: r.FormValue("response_url"),
	}

	fmt.Printf("📨 Received command from @%s: %s %s\n", cmd.UserName, cmd.Command, cmd.Text)

	// Send immediate acknowledgment to Slack (required within 3 seconds)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(SlackSlashResponse{
		ResponseType: "ephemeral",
		Text:         "🔍 Fetching your JIRA issues...",
	})

	// Process the request asynchronously
	go processSlashCommand(cmd)
}

// processSlashCommand fetches JIRA data and sends the filtered response
func processSlashCommand(cmd SlackSlashCommand) {
	jiraURL := os.Getenv("JIRA_URL")
	jiraToken := os.Getenv("JIRA_TOKEN")
	slackBotToken := os.Getenv("SLACK_BOT_TOKEN")

	if jiraURL == "" || jiraToken == "" {
		sendErrorResponse(cmd.ResponseURL, "Configuration error: JIRA_URL or JIRA_TOKEN not set")
		return
	}

	// Parse the username from the command text
	username := strings.TrimSpace(cmd.Text)

	// If no username provided, fetch the user's real name from Slack
	if username == "" {
		if slackBotToken == "" {
			sendErrorResponse(cmd.ResponseURL, "Please specify a name: `/issues John Doe`")
			return
		}

		realName, err := getSlackUserRealName(slackBotToken, cmd.UserID)
		if err != nil {
			sendErrorResponse(cmd.ResponseURL, fmt.Sprintf("Failed to auto-detect your name.\n\nPlease specify a name: `/issues John Doe`"))
			return
		}

		username = realName
		fmt.Printf("   Auto-detected user: %s (Slack: @%s, ID: %s)\n", username, cmd.UserName, cmd.UserID)
	}

	// Fetch JIRA issues
	jql := `project = MTV AND (status IN (POST, ON_QA, MODIFIED) OR (type = Epic AND status != Closed)) ORDER BY assignee`
	issues, err := fetchJiraIssues(jiraURL, jiraToken, jql)
	if err != nil {
		sendErrorResponse(cmd.ResponseURL, fmt.Sprintf("Failed to fetch JIRA issues: %v", err))
		return
	}

	// Filter issues for the specified user
	userIssues := filterIssuesByUser(issues, username)

	if len(userIssues) == 0 {
		sendErrorResponse(cmd.ResponseURL, fmt.Sprintf("No issues found for: *%s*\n\nMake sure the name matches exactly as it appears in JIRA.", username))
		return
	}

	// Build and send the response
	blocks := buildUserIssuesBlocks(jiraURL, username, userIssues)
	sendSlackResponse(cmd.ResponseURL, SlackSlashResponse{
		ResponseType: "ephemeral",
		Blocks:       blocks,
	})

	fmt.Printf("✅ Sent %d issues for %s to @%s\n", len(userIssues), username, cmd.UserName)
}

// filterIssuesByUser returns issues assigned to or QA'd by the specified user
func filterIssuesByUser(responses []JiraSearchResponse, username string) []IssueItem {
	var filtered []IssueItem

	// Normalize username for case-insensitive matching
	usernameLower := strings.ToLower(username)

	for _, resp := range responses {
		for _, issue := range resp.Issues {
			// Skip filtered issues
			if shouldFilterOut(issue.Fields.Components, issue.Fields.Labels) {
				continue
			}

			prs := extractPRs(issue.Fields.GitPullRequest)

			// Skip Epics without PRs
			if issue.Fields.IssueType.Name == "Epic" && len(prs) == 0 {
				continue
			}

			// Check if this issue belongs to the user
			var assigneeName string
			var qaContactName string

			if issue.Fields.Assignee != nil {
				assigneeName = issue.Fields.Assignee.DisplayName
			}
			if issue.Fields.QAContact != nil {
				qaContactName = issue.Fields.QAContact.DisplayName
			}

			// Match by assignee or QA contact (case-insensitive, partial match)
			if strings.Contains(strings.ToLower(assigneeName), usernameLower) ||
				strings.Contains(strings.ToLower(qaContactName), usernameLower) {

				filtered = append(filtered, IssueItem{
					Key:            issue.Key,
					Summary:        issue.Fields.Summary,
					Status:         issue.Fields.Status.Name,
					GitPullRequest: prs,
				})
			}
		}
	}

	return filtered
}

// buildUserIssuesBlocks creates Slack blocks for a user's filtered issues
func buildUserIssuesBlocks(jiraURL, username string, issues []IssueItem) []map[string]interface{} {
	blocks := []map[string]interface{}{
		{
			"type": "header",
			"text": map[string]string{
				"type": "plain_text",
				"text": fmt.Sprintf("📋 Issues for %s", username),
			},
		},
		{
			"type": "section",
			"text": map[string]string{
				"type": "mrkdwn",
				"text": fmt.Sprintf("Found *%d* issue(s) — %s", len(issues), time.Now().Format("Jan 2, 2006 15:04")),
			},
		},
		{"type": "divider"},
	}

	// Sort issues by status
	sort.SliceStable(issues, func(i, j int) bool {
		return issues[i].Status < issues[j].Status
	})

	for _, issue := range issues {
		// Format PR links
		pr := "–"
		if len(issue.GitPullRequest) > 0 {
			var prLinks []string
			for i, prURL := range issue.GitPullRequest {
				prLinks = append(prLinks, fmt.Sprintf("<%s|PR%d>", prURL, i+1))
			}
			pr = strings.Join(prLinks, " ")
		}

		// Escape and truncate summary
		summary := escapeSlackText(issue.Summary)
		if len(summary) > 150 {
			summary = summary[:150] + "..."
		}

		text := fmt.Sprintf("<%s/browse/%s|*%s*> — %s\n*Status:* %s  |  *PR:* %s",
			jiraURL, issue.Key, issue.Key, summary, issue.Status, pr)

		blocks = append(blocks, map[string]interface{}{
			"type": "section",
			"text": map[string]string{
				"type": "mrkdwn",
				"text": text,
			},
		})
	}

	return blocks
}

// sendSlackResponse sends a response to Slack's response_url
func sendSlackResponse(responseURL string, response SlackSlashResponse) error {
	data, err := json.Marshal(response)
	if err != nil {
		return fmt.Errorf("failed to marshal response: %w", err)
	}

	resp, err := http.Post(responseURL, "application/json", bytes.NewBuffer(data))
	if err != nil {
		return fmt.Errorf("failed to post response: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Slack returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}

// sendErrorResponse sends an error message to the user
func sendErrorResponse(responseURL, errorMsg string) {
	response := SlackSlashResponse{
		ResponseType: "ephemeral",
		Text:         "❌ " + errorMsg,
	}

	if err := sendSlackResponse(responseURL, response); err != nil {
		fmt.Printf("❌ Failed to send error response: %v\n", err)
	}
}

// getSlackUserRealName fetches a user's real name from Slack using their user ID
func getSlackUserRealName(botToken, userID string) (string, error) {
	url := fmt.Sprintf("https://slack.com/api/users.info?user=%s", userID)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", botToken))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to call Slack API: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	var userInfo SlackUserInfoResponse
	if err := json.Unmarshal(bodyBytes, &userInfo); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if !userInfo.OK {
		return "", fmt.Errorf("Slack API error: %s", userInfo.Error)
	}

	// Try display name first, then real name, then fall back to username
	if userInfo.User.Profile.DisplayName != "" {
		return userInfo.User.Profile.DisplayName, nil
	}
	if userInfo.User.RealName != "" {
		return userInfo.User.RealName, nil
	}
	if userInfo.User.Profile.RealName != "" {
		return userInfo.User.Profile.RealName, nil
	}

	return userInfo.User.Name, nil
}
