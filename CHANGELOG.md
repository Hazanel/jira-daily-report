# Changelog

All notable changes to the JIRA Daily Report Generator will be documented in this file.

## [1.0.0] - 2025-11-09

### Added
- Initial release of JIRA Daily Report Generator
- Fetch issues from Red Hat JIRA based on status (POST, ON_QA, MODIFIED)
- Fetch non-closed Epics with Pull Requests
- Filter out UI-related issues (component or label based)
- Group issues by person:
  - ON_QA and MODIFIED issues grouped by QA Contact
  - Other issues grouped by Assignee
- Generate Markdown report with formatted tables
- Send formatted updates to Slack using Block Kit
- Support for multiple Pull Request links per issue
- Escape special characters in Slack messages
- Respect Slack's 50 block limit
- Comprehensive documentation and code comments
- README with setup and usage instructions
- Example configuration file
- .gitignore for security

### Features
- **Smart Grouping**: Automatically groups issues by the right person based on status
- **PR Tracking**: Shows all associated Pull Requests for each issue
- **Dual Output**: Both human-readable Markdown and Slack-formatted messages
- **Filtering**: Excludes UI-related issues and Epics without PRs
- **Custom Fields**: Supports Red Hat JIRA custom fields (QA Contact, Git Pull Request)

### Configuration
- Configurable JIRA URL (defaults to Red Hat JIRA)
- Bearer token authentication for JIRA API
- Slack incoming webhook integration
- JQL query customization support

### Technical Details
- Written in Go with no external dependencies (uses only standard library)
- Fetches up to 500 issues per run
- Truncates long summaries in Slack (200 chars max)
- Handles both single and multiple PR links per issue
- Proper error handling and reporting

