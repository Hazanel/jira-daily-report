# Quick Deploy to OpenShift

## Prerequisites

- OpenShift CLI (`oc`) installed and logged in
- Docker installed and running
- Quay.io account
- Slack bot token with scopes: `chat:write`, `users:read`, `commands`
- JIRA API token

---

## Step 1: Build and Push Image

```bash
# Login to Quay.io
docker login quay.io

# Build and push (replace 'username' with your Quay.io username)
docker build -t quay.io/username/jira-slash-command:latest .
docker push quay.io/username/jira-slash-command:latest
```

---

## Step 2: Update Deployment YAML

Edit `openshift-deployment.yaml` line 31:

```yaml
image: quay.io/username/jira-slash-command:latest
```

Replace `username` with your Quay.io username.

---

## Step 3: Create Secret with Real Tokens

```bash
# Delete old secret if exists
oc delete secret jira-slash-secrets --ignore-not-found

# Create new secret with REAL values
oc create secret generic jira-slash-secrets \
  --from-literal=JIRA_URL=https://issues.redhat.com \
  --from-literal=JIRA_TOKEN=NEW_TOKEN \
  --from-literal=SLACK_BOT_TOKEN=NEW_TOKEN
```

---

## Step 4: Deploy to OpenShift

```bash
# Apply deployment (skip Secret section since we created it manually)
oc apply -f openshift-deployment.yaml

# Wait for rollout
oc rollout status deployment/jira-slash-command

# Get your public URL
oc get route jira-slash-command -o jsonpath='{.spec.host}'
```

**Your URL will look like:**
```
https://jira-slash-command-NAMESPACE.apps.CLUSTER.com
```

---

## Step 5: Configure Slack Slash Command

1. Go to: https://api.slack.com/apps
2. Select your app → **Slash Commands**
3. Click **Create New Command** or edit existing:
   - **Command:** `/issues`
   - **Request URL:** `https://YOUR-ROUTE-URL/slack/issues`
   - **Short Description:** Query your JIRA issues
4. Click **Save**
5. **Reinstall your app** (important!)

---

## Step 6: Test

```bash
# Check health
curl -k https://$(oc get route jira-slash-command -o jsonpath='{.spec.host}')/health

# Check logs
oc logs -f deployment/jira-slash-command
```

In Slack:
```
/issues              → Your issues (auto-detect)
/issues John Doe     → Specific user's issues
```

---

## Quick Update After Code Changes

```bash
# Rebuild, push, and restart
docker build -t quay.io/username/jira-slash-command:latest . && \
docker push quay.io/username/jira-slash-command:latest && \
oc rollout restart deployment/jira-slash-command

# Watch deployment
oc rollout status deployment/jira-slash-command
```

---

## Architecture

```
Slack (/issues command)
    ↓
Route (public HTTPS URL)
    ↓
Service (internal routing)
    ↓
Pod (your Go app)
    ↓
JIRA API + Slack API
```

---


