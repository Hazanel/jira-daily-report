// Slack Slash Command Server
//
// This server handles Slack slash commands to filter JIRA issues by user.
// Users can run commands like:
//
//	/issues                  - Shows YOUR OWN open issues (auto-detected from Slack)
//	/issues John Doe         - Shows John Doe's open issues
//	/issues --all            - Shows ALL your issues (including closed)
//	/issues John Doe --all   - Shows ALL John Doe's issues
//	/issues --all John Doe   - Same as above (order doesn't matter)
//
// Results are organized in threaded messages:
// - Main message: Summary with issue counts per status
// - Thread replies: One reply per status (Open, In Progress, Modified, Closed, Archived)
//
// The server fetches fresh JIRA data for each request and posts results
// to the configured SLACK_CHANNEL (visible to all channel members).
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
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

	if slackBotToken == "" {
		sendErrorResponse(cmd.ResponseURL, "Configuration error: SLACK_BOT_TOKEN not set")
		return
	}

	// Parse the command text for --all flag and username
	text := strings.TrimSpace(cmd.Text)
	includeAll := strings.Contains(text, "--all")

	// Remove --all from text to get username
	username := strings.TrimSpace(strings.ReplaceAll(text, "--all", ""))

	// If no username provided, fetch the user's real name from Slack
	if username == "" {
		realName, err := getSlackUserRealName(slackBotToken, cmd.UserID)
		if err != nil {
			sendErrorResponse(cmd.ResponseURL, "Failed to auto-detect your name.\n\nPlease specify a name: `/issues John Doe`")
			return
		}

		username = realName
		fmt.Printf("   Auto-detected user: %s (Slack: @%s, ID: %s)\n", username, cmd.UserName, cmd.UserID)
	}

	if includeAll {
		fmt.Printf("   Fetching ALL issues (including closed) for %s...\n", username)
	} else {
		fmt.Printf("   Fetching open issues for %s...\n", username)
	}

	// Build JQL based on --all flag
	jql := buildJQLQuery(username, includeAll)
	fmt.Printf("   JQL: %s\n", jql)
	issues, err := fetchJiraIssues(jiraURL, jiraToken, jql)
	if err != nil {
		fmt.Printf("   ❌ JIRA fetch error: %v\n", err)
		sendErrorResponse(cmd.ResponseURL, fmt.Sprintf("Failed to fetch JIRA issues: %v", err))
		return
	}
	fmt.Printf("   ✓ Fetched JIRA responses\n")

	// Filter issues for the specified user
	// For slash commands, show ALL user issues (skipFilters=true)
	userIssues := filterIssuesByUser(issues, username, true)
	fmt.Printf("   ✓ Found %d issues for %s\n", len(userIssues), username)

	if len(userIssues) == 0 {
		sendErrorResponse(cmd.ResponseURL, fmt.Sprintf("No issues found for: *%s*\n\nMake sure the name matches exactly as it appears in JIRA.", username))
		return
	}

	// Group issues by status
	statusGroups := groupIssuesByStatus(userIssues)

	// Build ephemeral response (private, only visible to user)
	blocks := buildEphemeralStatusBlocks(jiraURL, username, statusGroups, includeAll)

	err = sendSlackResponse(cmd.ResponseURL, SlackSlashResponse{
		ResponseType: "ephemeral",
		Blocks:       blocks,
	})
	if err != nil {
		fmt.Printf("   ❌ ERROR sending ephemeral response: %v\n", err)
		return
	}

	fmt.Printf("✅ Sent %d issues for %s to @%s (ephemeral)\n", len(userIssues), username, cmd.UserName)
}

// buildJQLQuery constructs the JQL query based on user and --all flag
// NOTE: User filtering is done in Go code, not in JQL, to support display names
func buildJQLQuery(username string, includeAll bool) string {
	jql := "project = MTV"

	if includeAll {
		// Include all statuses - filtering by user happens in filterIssuesByUser()
		jql += " ORDER BY status ASC, updated DESC"
	} else {
		// Only open/active statuses
		jql += " AND (status IN (POST, ON_QA, MODIFIED) OR (type = Epic AND status != Closed))"
		jql += " ORDER BY status ASC"
	}

	return jql
}

// groupIssuesByStatus groups issues by their status
func groupIssuesByStatus(issues []IssueItem) map[string][]IssueItem {
	groups := make(map[string][]IssueItem)
	for _, issue := range issues {
		groups[issue.Status] = append(groups[issue.Status], issue)
	}
	return groups
}

// buildEphemeralStatusBlocks creates a flat ephemeral message organized by status
// Respects Slack's 50 block limit by truncating if needed
func buildEphemeralStatusBlocks(jiraURL, username string, statusGroups map[string][]IssueItem, includeAll bool) []map[string]interface{} {
	// Status order
	statusOrder := []string{"Open", "In Progress", "Modified", "Closed", "Archived", "POST", "ON_QA", "MODIFIED", "Verified", "Done"}

	// Calculate total issues
	totalIssues := 0
	for _, issues := range statusGroups {
		totalIssues += len(issues)
	}

	// Build summary lines
	summaryLines := []string{}
	for _, status := range statusOrder {
		if issues, exists := statusGroups[status]; exists {
			summaryLines = append(summaryLines, fmt.Sprintf("• *%s:* %d", status, len(issues)))
		}
	}

	// Add any statuses not in predefined order
	for status, issues := range statusGroups {
		found := false
		for _, s := range statusOrder {
			if s == status {
				found = true
				break
			}
		}
		if !found {
			summaryLines = append(summaryLines, fmt.Sprintf("• *%s:* %d", status, len(issues)))
		}
	}

	title := fmt.Sprintf("🔍 Issues for %s", username)
	if includeAll {
		title = fmt.Sprintf("🔍 All Issues for %s", username)
	}

	blocks := []map[string]interface{}{
		{
			"type": "header",
			"text": map[string]string{
				"type": "plain_text",
				"text": title,
			},
		},
		{
			"type": "section",
			"text": map[string]string{
				"type": "mrkdwn",
				"text": fmt.Sprintf("Found *%d* issue(s) across *%d* status(es)\n\n📊 *Summary:*\n%s",
					totalIssues, len(statusGroups), strings.Join(summaryLines, "\n")),
			},
		},
		{"type": "divider"},
	}

	const maxBlocks = 48 // Leave room for header/summary/dividers
	currentBlocks := 3   // Header + summary + divider
	issuesShown := 0     // Track how many issues displayed
	truncated := false   // Track if we've added truncation message

	// Add issues by status
	for _, status := range statusOrder {
		issues, exists := statusGroups[status]
		if !exists {
			continue
		}

		// Check if we have room for at least the status header + 1 issue
		if currentBlocks+2 > maxBlocks {
			if !truncated {
				remainingIssues := totalIssues - issuesShown
				blocks = append(blocks, map[string]interface{}{
					"type": "section",
					"text": map[string]string{
						"type": "mrkdwn",
						"text": fmt.Sprintf("\n_...and %d more issue(s) not shown_", remainingIssues),
					},
				})
				truncated = true
			}
			break
		}

		// Add status header
		blocks = append(blocks, map[string]interface{}{
			"type": "section",
			"text": map[string]string{
				"type": "mrkdwn",
				"text": fmt.Sprintf("\n📂 *%s* (%d)", status, len(issues)),
			},
		})
		currentBlocks++

		// Add issues for this status
		for i, issue := range issues {
			if currentBlocks >= maxBlocks {
				if !truncated {
					remainingInStatus := len(issues) - i
					remainingTotal := totalIssues - issuesShown
					blocks = append(blocks, map[string]interface{}{
						"type": "section",
						"text": map[string]string{
							"type": "mrkdwn",
							"text": fmt.Sprintf("_...and %d more in this status (%d total remaining)_",
								remainingInStatus, remainingTotal),
						},
					})
					truncated = true
				}
				break
			}

			// Format PR links
			pr := "–"
			if len(issue.GitPullRequest) > 0 {
				var prLinks []string
				for j, prURL := range issue.GitPullRequest {
					prLinks = append(prLinks, fmt.Sprintf("<%s|PR%d>", prURL, j+1))
				}
				pr = strings.Join(prLinks, " ")
			}

			// Escape and truncate summary
			summary := escapeSlackText(issue.Summary)
			if len(summary) > 100 {
				summary = summary[:100] + "..."
			}

			text := fmt.Sprintf("• <%s/browse/%s|*%s*> — %s\n   *Status:* %s  |  *PR:* %s",
				jiraURL, issue.Key, issue.Key, summary, issue.Status, pr)

			blocks = append(blocks, map[string]interface{}{
				"type": "section",
				"text": map[string]string{
					"type": "mrkdwn",
					"text": text,
				},
			})
			currentBlocks++
			issuesShown++
		}
	}

	return blocks
}

// sendThreadedResponse sends the main summary message and status group replies
func sendThreadedResponse(botToken, channel, jiraURL, username string, statusGroups map[string][]IssueItem, includeAll bool) error {
	// Define status order
	statusOrder := []string{"Open", "In Progress", "Modified", "Closed", "Archived", "POST", "ON_QA", "MODIFIED", "Verified", "Done"}

	// Calculate total issues
	totalIssues := 0
	for _, issues := range statusGroups {
		totalIssues += len(issues)
	}

	// Build summary for main message
	summaryLines := []string{}
	for _, status := range statusOrder {
		if issues, exists := statusGroups[status]; exists {
			summaryLines = append(summaryLines, fmt.Sprintf("• *%s:* %d issue(s)", status, len(issues)))
		}
	}

	// Add any statuses not in the predefined order
	for status, issues := range statusGroups {
		found := false
		for _, s := range statusOrder {
			if s == status {
				found = true
				break
			}
		}
		if !found {
			summaryLines = append(summaryLines, fmt.Sprintf("• *%s:* %d issue(s)", status, len(issues)))
		}
	}

	title := fmt.Sprintf("🔍 Issues for %s", username)
	if includeAll {
		title = fmt.Sprintf("🔍 All Issues for %s", username)
	}

	// Build main summary message blocks
	summaryBlocks := []map[string]interface{}{
		{
			"type": "header",
			"text": map[string]string{
				"type": "plain_text",
				"text": title,
			},
		},
		{
			"type": "section",
			"text": map[string]string{
				"type": "mrkdwn",
				"text": fmt.Sprintf("Found *%d* issue(s) across *%d* status(es)\n\n📊 *Summary:*\n%s\n\n👇 _See details in thread below_",
					totalIssues, len(statusGroups), strings.Join(summaryLines, "\n")),
			},
		},
	}

	// Send main message to create thread
	fmt.Printf("   Creating thread with summary...\n")
	threadTS, err := sendToSlackAPI(botToken, channel, "", summaryBlocks)
	if err != nil {
		return fmt.Errorf("failed to send summary message: %w", err)
	}
	fmt.Printf("   ✓ Thread created\n")

	// Send each status group as a thread reply (in order)
	// Split large groups to avoid Slack's 50 block limit
	const maxIssuesPerMessage = 15
	sentCount := 0

	for _, status := range statusOrder {
		issues, exists := statusGroups[status]
		if !exists {
			continue
		}

		sentCount++

		// Split into chunks if too many issues
		for i := 0; i < len(issues); i += maxIssuesPerMessage {
			end := i + maxIssuesPerMessage
			if end > len(issues) {
				end = len(issues)
			}
			chunk := issues[i:end]

			if len(issues) <= maxIssuesPerMessage {
				fmt.Printf("   Sending status group %d/%d: %s (%d issues)...\n", sentCount, len(statusGroups), status, len(issues))
			} else {
				fmt.Printf("   Sending status group %d/%d: %s (%d-%d of %d issues)...\n",
					sentCount, len(statusGroups), status, i+1, end, len(issues))
			}

			blocks := buildStatusGroupBlocks(jiraURL, status, chunk, i == 0)
			_, err = sendToSlackAPI(botToken, channel, threadTS, blocks)
			if err != nil {
				return fmt.Errorf("failed to send status group %s: %w", status, err)
			}

			fmt.Printf("   ✓ Status group %s sent\n", status)

			// Small delay between messages to ensure proper ordering
			time.Sleep(500 * time.Millisecond)
		}
	}

	// Send any remaining statuses not in predefined order
	for status, issues := range statusGroups {
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

		sentCount++

		// Split into chunks if too many issues
		for i := 0; i < len(issues); i += maxIssuesPerMessage {
			end := i + maxIssuesPerMessage
			if end > len(issues) {
				end = len(issues)
			}
			chunk := issues[i:end]

			if len(issues) <= maxIssuesPerMessage {
				fmt.Printf("   Sending status group %d/%d: %s (%d issues)...\n", sentCount, len(statusGroups), status, len(issues))
			} else {
				fmt.Printf("   Sending status group %d/%d: %s (%d-%d of %d issues)...\n",
					sentCount, len(statusGroups), status, i+1, end, len(issues))
			}

			blocks := buildStatusGroupBlocks(jiraURL, status, chunk, i == 0)
			_, err = sendToSlackAPI(botToken, channel, threadTS, blocks)
			if err != nil {
				return fmt.Errorf("failed to send status group %s: %w", status, err)
			}

			fmt.Printf("   ✓ Status group %s sent\n", status)
			time.Sleep(500 * time.Millisecond)
		}
	}

	return nil
}

// buildStatusGroupBlocks creates Slack blocks for a specific status group
// isFirstChunk: true if this is the first chunk of issues for this status (shows header)
func buildStatusGroupBlocks(jiraURL, status string, issues []IssueItem, isFirstChunk bool) []map[string]interface{} {
	blocks := []map[string]interface{}{}

	// Only show header on first chunk
	if isFirstChunk {
		blocks = append(blocks, map[string]interface{}{
			"type": "section",
			"text": map[string]string{
				"type": "mrkdwn",
				"text": fmt.Sprintf("📂 *%s*\n", status),
			},
		})
		blocks = append(blocks, map[string]interface{}{"type": "divider"})
	}

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

		text := fmt.Sprintf("• <%s/browse/%s|*%s*> — %s\n   *Status:* %s  |  *PR:* %s",
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

// filterIssuesByUser returns issues assigned to or QA'd by the specified user
// If skipFilters is true, shows ALL user issues (for slash commands)
// If skipFilters is false, applies daily report filters (UI issues, Epics without PRs)
func filterIssuesByUser(responses []JiraSearchResponse, username string, skipFilters bool) []IssueItem {
	var filtered []IssueItem

	// Normalize username for case-insensitive matching
	usernameLower := strings.ToLower(username)

	for _, resp := range responses {
		for _, issue := range resp.Issues {
			// Extract PRs for display
			prs := extractPRs(issue.Fields.GitPullRequest)

			// Apply filters only for daily reports, not for slash commands
			if !skipFilters {
				// Skip filtered issues (UI-related, certain labels)
				if shouldFilterOut(issue.Fields.Components, issue.Fields.Labels) {
					continue
				}

				// Skip Epics without PRs
				if issue.Fields.IssueType.Name == "Epic" && len(prs) == 0 {
					continue
				}
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
