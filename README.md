# JIRA Daily Report Generator

A Go tool that fetches JIRA issues and sends automated daily reports to Slack, plus an on-demand slash command for personal issue queries.

## Features

### 📊 Daily Reports (Automated)
- Fetches issues from Red Hat JIRA based on status and type
- Sends formatted daily updates to Slack as threaded messages
- Groups issues by person (Assignee or QA Contact depending on status)
- Includes Pull Request links when available
- Filters out UI-related issues
- Shows only Epics that have associated Pull Requests
- Uses Slack threads to keep the channel clean (header + threaded replies)
- Can be automated to run daily via GitHub Actions

### 🔍 Slash Command (On-Demand)
- Type `/issues` to see YOUR OWN issues instantly
- Type `/issues Isr Itl` to see someone else's issues
- Auto-detects your name from Slack profile
- Results shown only to you (ephemeral messages)
- Always fetches fresh data from JIRA

## Requirements

- Go 1.16 or later
- JIRA API token with read access
- Slack Bot Token with `chat:write` scope
- Slack Channel ID where reports will be posted

## Installation

```bash
# Clone the repository
git clone <repository-url>
cd jira_update

# Build the binary
go build -o jira_update main.go slash-server.go
```

## Configuration

The tool requires four configuration values:

### 1. JIRA URL
Default: `https://issues.redhat.com`

Set via environment variable:
```bash
export JIRA_URL="https://issues.redhat.com"
```

### 2. JIRA Token
Get your token at: https://issues.redhat.com/secure/ViewProfile.jspa

```bash
export JIRA_TOKEN="your-token-here"
```

### 3. Slack Bot Token
Create a Slack App at https://api.slack.com/apps with the `chat:write` scope.

```bash
export SLACK_BOT_TOKEN="xoxb-your-bot-token-here"
```

### 4. Slack Channel ID
Find your channel ID by right-clicking the channel → View channel details → Copy ID (bottom of the modal).

```bash
export SLACK_CHANNEL="C09RAMA1YFR"
```

## Usage

### Daily Report Mode (Default)

```bash
# Run the daily report once
./jira_update

# Or run directly with Go
go run main.go slash-server.go
```

The tool sends a formatted message to your Slack channel via webhook.

### Slash Command Server Mode

```bash
# Start the server for slash commands
./jira_update -server

# The server will run continuously and handle incoming Slack commands
```

Then in Slack, users can type:
- `/issues` - See your own issues
- `/issues Isr Itl` - See someone else's issues

**📖 For deployment instructions, see the guides below**

## Automating Daily Reports

### GitHub Actions (Recommended - No Server Needed!)

The easiest way to run this daily is using GitHub Actions.

#### Setup Steps:

**1. Add GitHub Secrets**

Go to your repository: `Settings` → `Secrets and variables` → `Actions` → `New repository secret`

Add these three secrets:
- **Name:** `JIRA_TOKEN` | **Value:** Your JIRA API token
- **Name:** `SLACK_BOT_TOKEN` | **Value:** Your Slack bot token (starts with `xoxb-`)
- **Name:** `SLACK_CHANNEL` | **Value:** Your Slack channel ID (e.g., `C09RAMA1YFR`)

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

The tool sends a formatted Slack message as a **thread**:
- **Main message**: "🧾 Daily JIRA Summary — [Date]" (visible in channel)
- **Thread replies**: All issue details grouped by person
- **Grouping**: Issues grouped by person (Assignee or QA Contact)
- **Issue Details**: Clickable links to JIRA issues, status, and PR links
- **Clean Formatting**: Using Slack Block Kit with threading for professional appearance

**Example Slack output:**

**Main message (visible in channel):**
```
🧾 Daily JIRA Summary — Nov 12, 2025
```

**Thread replies (click to expand):**
```
👤 Jane Doe
MTV-1234 — Fix migration timeout issue
Status: POST | PR: PR1

MTV-5678 — Update API endpoints
Status: MODIFIED | PR: PR1 PR2

👤 John Smith
MTV-2894 — Investigate OCP hooks
Status: ON_QA | PR: PR1
```

This keeps your channel clean while maintaining all the detailed information in threads!

## Troubleshooting

### "Missing required credentials"
Ensure all four configuration values are set: `JIRA_URL`, `JIRA_TOKEN`, `SLACK_BOT_TOKEN`, `SLACK_CHANNEL`.

### "JIRA API returned 401"
Your JIRA token may be invalid or expired. Generate a new token at https://issues.redhat.com/secure/ViewProfile.jspa

### "Slack API error: invalid_auth"
- Check that your Slack bot token is correct (starts with `xoxb-`)
- Verify the token hasn't been revoked
- Ensure the Slack app is installed to your workspace

### "Slack API error: missing_scope"
Your Slack app needs the `chat:write` scope. Go to https://api.slack.com/apps → Your App → OAuth & Permissions → Add the `chat:write` scope → Reinstall the app.

### "Slack API error: channel_not_found"
- Verify the channel ID is correct (e.g., `C09RAMA1YFR`)
- Ensure the bot is invited to the channel (type `/invite @YourBotName` in the channel)

### "Field 'customfield_XXXXX' does not exist"
You're using a different JIRA instance. Update the custom field IDs in `main.go`.


### Building for Production
```bash
# Build with optimizations
go build -ldflags="-s -w" -o jira_update main.go slash-server.go

# Make it executable
chmod +x jira_update
```

## Slash Command Feature

The tool includes an optional slash command server that allows team members to query their JIRA issues on-demand directly from Slack.

### Quick Start

1. **Build and run the server:**
   ```bash
   export JIRA_URL=https://issues.redhat.com
   export JIRA_TOKEN=your-token
   export SLACK_BOT_TOKEN=xoxb-your-token
   ./jira_update -server
   ```

2. **In Slack, type:**
   ```
   /issues
   ```

3. **See your issues instantly!**

### Deployment Options

The slash command requires a **publicly accessible web server** that runs continuously.

**📖 Deployment Guides:**
- **[DEPLOY_RENDER.md](docs/DEPLOY_RENDER.md)** - Deploy to Render.com (free tier, recommended for persistent hosting)
- **[DEPLOY_QUICK_openshift.md](docs/DEPLOY_QUICK_openshift.md)** - Quick deployment to OpenShift


