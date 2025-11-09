# JIRA Daily Report Generator

A Go tool that fetches JIRA issues and sends automated daily reports to Slack.

## Features

- Fetches issues from Red Hat JIRA based on status and type
- Sends formatted daily updates to Slack via webhook
- Groups issues by person (Assignee or QA Contact depending on status)
- Includes Pull Request links when available
- Filters out UI-related issues
- Shows only Epics that have associated Pull Requests
- Can be automated to run daily via cron

## Requirements

- Go 1.16 or later
- JIRA API token with read access
- Slack incoming webhook URL

## Installation

```bash
# Clone the repository
git clone <repository-url>
cd jira_update

# Build the binary
go build -o jira_update main.go
```

## Configuration

The tool requires three configuration values:

### 1. JIRA URL
Default: `https://issues.redhat.com`

Set via environment variable (optional):
```bash
export JIRA_URL="https://issues.redhat.com"
```

### 2. JIRA Token
Get your token at: https://issues.redhat.com/secure/ViewProfile.jspa

**Production setup** (recommended):
```bash
export JIRA_TOKEN="your-token-here"
```

### 3. Slack Webhook
Create an incoming webhook at: https://api.slack.com/apps

**Production setup** (recommended):
```bash
export SLACK_WEBHOOK="https://hooks.slack.com/services/YOUR/WEBHOOK/URL"
```

## Usage

```bash
# Run the tool
./jira_update

# Or run directly with Go
go run main.go
```

### Output

The tool sends a formatted message to your Slack channel via webhook.

## Automating Daily Reports

### GitHub Actions (Recommended - No Server Needed!)

The easiest way to run this daily is using GitHub Actions.

#### Setup Steps:

**1. Add GitHub Secrets**

Go to your repository: `Settings` → `Secrets and variables` → `Actions` → `New repository secret`

Add these two secrets:
- **Name:** `JIRA_TOKEN` | **Value:** Your JIRA API token
- **Name:** `SLACK_WEBHOOK` | **Value:** Your Slack webhook URL

**2. Workflow is already configured!**

The workflow file `.github/workflows/jira-report.yml` will automatically:
- ✅ Run Monday-Friday at 9:00 AM UTC
- ✅ Use your secrets securely (never exposed in logs)
- ✅ Send reports to Slack
- ✅ Email you if it fails

**3. Customize the schedule (optional)**

Edit `.github/workflows/jira-report.yml` to change when it runs:

```yaml
schedule:
  - cron: '0 9 * * 1-5'   # 9 AM UTC, Mon-Fri (default)
  # - cron: '30 8 * * 1-5'   # 8:30 AM UTC, Mon-Fri  
  # - cron: '0 14 * * 1-5'   # 2 PM UTC, Mon-Fri
```

**Cron format:** `minute hour day month day_of_week` (always UTC timezone)

**4. Test it manually**

Go to `Actions` tab → `Daily JIRA Report` → `Run workflow` button

**5. Monitor runs**

Check the `Actions` tab to see execution history and logs.

---

## What Issues Are Included?

The tool fetches issues matching these criteria:

### By Status
- **POST**: Regular issues in POST status
- **ON_QA**: Issues currently being tested
- **MODIFIED**: Issues that have been modified and have a QA Contact

### By Type
- **Epics**: Non-closed Epics that have at least one Pull Request

### Exclusions
- Issues with component "User Interface"
- Issues with label "user-interface"
- Epics without any Pull Requests

## Grouping Logic

Issues are grouped by person based on their status:

| Status | Grouped By |
|--------|-----------|
| ON_QA | QA Contact (if available), otherwise Assignee |
| MODIFIED | QA Contact (if available), otherwise Assignee |
| All others | Assignee |


## Example Output

The tool sends a formatted Slack message with:
- **Header**: "🧾 Daily JIRA Summary — [Date]"
- **Grouping**: Issues grouped by person (Assignee or QA Contact)
- **Issue Details**: Clickable links to JIRA issues, status, and PR links
- **Clean Formatting**: Using Slack Block Kit for professional appearance

**Example Slack message:**

```
🧾 Daily JIRA Summary — Nov 9, 2025
─────────────────────────────────

👤 Jane Doe
MTV-1234 — Fix migration timeout issue
Status: POST | PR: PR1

MTV-5678 — Update API endpoints
Status: MODIFIED | PR: PR1 PR2

👤 John Smith
MTV-2894 — Investigate OCP hooks
Status: ON_QA | PR: PR1
```

## Troubleshooting

### "Missing required credentials"
Ensure all three configuration values are set (JIRA_URL, JIRA_TOKEN, SLACK_WEBHOOK).

### "JIRA API returned 401"
Your JIRA token may be invalid or expired. Generate a new token.

### "Slack webhook returned status 400"
- Check that your webhook URL is correct
- Verify the webhook hasn't been revoked
- The message might exceed Slack's block limit (tool caps at 48 blocks)

### "Field 'customfield_XXXXX' does not exist"
You're using a different JIRA instance. Update the custom field IDs as described in the Custom Fields section.


### Building for Production
```bash
# Build with optimizations
go build -ldflags="-s -w" -o jira_update main.go

# Make it executable
chmod +x jira_update
```
