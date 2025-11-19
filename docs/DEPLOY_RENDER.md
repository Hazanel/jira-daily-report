# Deploy Slash Command to Render.com

**Perfect for persistent, free slash command hosting with auto-wake!**

---

## Why Render.com?

✅ **Free forever** (no credit card needed)  
✅ **Auto-deploys** from GitHub  
✅ **Auto-wakes** on request (~30 seconds cold start)  
✅ **Never expires** (unlike OpenShift Sandbox)  
✅ **Zero maintenance** required  

**Trade-off:** Sleeps after 15 minutes of inactivity (wakes automatically on next use)

---

## Step 1: Sign Up (2 minutes)

1. Go to: **https://render.com**
2. Click **Get Started** (free, no credit card)
3. Sign up with GitHub

---

## Step 2: Create Web Service (3 minutes)

### Option A: Blueprint (Easiest - One Click!)

1. Click: **https://dashboard.render.com/select-repo**
2. Connect your repo: **`username/jira_update`**
3. Render detects `render.yaml` automatically
4. Click **Apply**
5. Skip to **Step 3** below! 

### Option B: Manual Setup

1. Dashboard → **New** → **Web Service**
2. Connect your GitHub account and select: **`username/jira_update`**
3. Configure:
   - **Name:** `jira-slash-command`
   - **Region:** Oregon (US West) or closest to you
   - **Branch:** `main`
   - **Runtime:** Docker
   - **Instance Type:** Free
4. Click **Create Web Service**

---

## Step 3: Add Environment Variables

In your Render dashboard:

1. Go to your service → **Environment** tab
2. Add these variables:

| Key | Value |
|-----|-------|
| `JIRA_URL` | `https://issues.redhat.com` |
| `JIRA_TOKEN` | Your actual JIRA token |
| `SLACK_BOT_TOKEN` | `xoxb-YOUR-ACTUAL-TOKEN` |
| `PORT` | `8080` |

3. Click **Save Changes**

**Render will automatically redeploy!**

---

## Step 4: Get Your URL

1. Wait for deployment to complete (~2-3 minutes)
2. Your URL will be:
   ```
   https://jira-slash-command.onrender.com
   ```
   (Check the top of your service dashboard)

---

## Step 5: Update Slack Slash Command

1. Go to: **https://api.slack.com/apps**
2. Select your app → **Slash Commands**
3. Edit `/issues` command:
   - **Request URL:** `https://jira-slash-command.onrender.com/slack/issues`
4. Click **Save**

**No need to reinstall the app!**

---

## Step 6: Test

### Test health endpoint:
```bash
curl https://jira-slash-command.onrender.com/health
```

Expected response:
```json
{"status": "ok"}
```

### Test in Slack:
```
/issues              → Your issues
/issues John Doe     → Specific user's issues
```

**First request after sleep:** ~30 seconds (Render wakes up)  
**Subsequent requests:** Instant!

---

## 🔄 Auto-Deploy on Push

**Already configured!** 🎉

Every time you push to `main` branch:
1. Render detects the change
2. Rebuilds the Docker image
3. Deploys automatically
4. Zero downtime!

**View deployment logs:** Dashboard → **Logs** tab

---

## 📊 Monitoring

### View Logs:
```
Dashboard → Your Service → Logs tab
```

### Check Status:
```
Dashboard → Your Service → Events tab
```

### Metrics (free tier includes):
- Request count
- Response times
- Memory/CPU usage

---


## 💡 Pro Tips

### Keep Service Warm (Recommended):
Render free tier **spins down after 15 minutes of inactivity**. Keep it warm during work hours:

**GitHub Actions Usage:**
- Pings every 14 minutes (Mon-Thu, 7 AM - 7 PM UTC)
- Uses **~146 min/month** of GitHub Actions
- Combined with daily report: **~178 min/month total**
- Well within free tier limit (2,000 min/month for private repos, unlimited for public)

**Create `.github/workflows/keep-render-warm.yml`:**

```yaml
name: Keep Render Warm
on:
  schedule:
    # Every 14 minutes, Monday-Thursday, 7 AM - 7 PM UTC
    - cron: '0,14,28,42,56 7-18 * * 1-4'
jobs:
  ping:
    runs-on: ubuntu-latest
    steps:
      - name: Ping health endpoint
        run: |
          echo "🏓 Pinging Render service to keep it warm..."
          curl -f https://YOUR-APP-NAME.onrender.com/health || echo "⚠️ Ping failed"
          echo "✅ Ping complete"
```

**Replace `YOUR-APP-NAME.onrender.com`** with your actual Render URL

### Monitor uptime:
Use **UptimeRobot** (free) to monitor your endpoint and get alerts if it's down.

### Custom domain (optional):
Render free tier doesn't support custom domains. Upgrade to paid plan ($7/month) if needed.

---





**Free Tier Limits:**
- ✅ 750 hours/month runtime
- ✅ Unlimited deploys
- ✅ Automatic HTTPS
- ❌ Sleeps after 15 min inactivity
- ❌ No custom domain



---

