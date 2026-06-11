#!/bin/bash

# Stop the script if any command fails
set -e

# ======= CONFIGURATION =======
# Your private Slack Member ID - the message will go ONLY to you!
TARGET_ID="C0B9JT8Q066"
# Path to the tokens extracted from Slack
CREDS_DIR="$HOME/.config/slack"
# =============================

# Get commit message and branch name arguments
COMMIT_MSG=$1
BRANCH_NAME=$2

if [ -z "$COMMIT_MSG" ] || [ -z "$BRANCH_NAME" ]; then
    echo "❌ Error: Missing commit message or branch name."
    echo "Usage: ./scripts/ship_it.sh 'your commit message' 'your-branch-name'"
    exit 1
fi

echo "🚀 Starting automation process for branch: $BRANCH_NAME..."

# 1. Handle git branch switching or creation
if git show-ref --verify --quiet "refs/heads/$BRANCH_NAME"; then
    echo "📝 Branch already exists. Switching to it..."
    git checkout "$BRANCH_NAME"
else
    echo "🌿 Creating a new branch..."
    git checkout -b "$BRANCH_NAME"
fi

# 2. Git operations: add and commit
echo "💾 Committing changes..."
git add .
git commit -m "$COMMIT_MSG" --allow-empty

# 3. Git operations: push to remote repository (Safe)
echo "📦 Pushing code to remote branch..."
git push personal "$BRANCH_NAME"
echo "✅ Code successfully pushed to Git branch!"

# =====================================================================
# 🟢 LIVE SLACK NOTIFICATION MODE (WITH SIMULATED PR LINK)
# =====================================================================
echo "🔍 Simulating Pull Request check (Safe mode - no real PR created)..."
PR_URL="https://github.com/migtools/crane/pull/TEST-SIMULATION"
IS_NEW_PR=true 

echo ""
if [ "$IS_NEW_PR" = true ]; then
    read -p "❓ New PR simulated! Would you like to send a Slack notification? (y/n): " ANSWER
else
    read -p "❓ PR updated! Would you like to send an update notification to Slack? (y/n): " ANSWER
fi
echo ""

if [[ "$ANSWER" =~ ^[Yy]$ ]]; then
    echo "💬 Preparing Slack message..."
    
    if [ "$IS_NEW_PR" = true ]; then
        SLACK_MESSAGE="📢 *New PR is ready for Review!* \n\n*Topic:* $COMMIT_MSG\n*Branch:* \`$BRANCH_NAME\`\n🔗 *Link:* $PR_URL"
    else
        SLACK_MESSAGE="🔄 *PR Code Updated!* \n\n*Changes:* $COMMIT_MSG\n*Branch:* \`$BRANCH_NAME\`\n🔗 *Link:* $PR_URL"
    fi
    
    export SLACK_MESSAGE
    export TARGET_ID
    export CREDS_DIR

    echo "📡 Triggering local Slack post tool..."
    python3 -c "
import os
import sys

sys.path.append('scripts') 
from slack import post_message

msg = os.environ.get('SLACK_MESSAGE')
target = os.environ.get('TARGET_ID')
creds = os.environ.get('CREDS_DIR')

post_message(target, msg, creds)
"
    echo "🎉 Test message successfully sent to your private Slack channel!"
else
    echo "⏭️ Skipping Slack notification as requested."
fi

echo "🏁 Process completed successfully!"