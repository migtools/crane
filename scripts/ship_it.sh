# #!/bin/bash

# # Stop the script if any command fails
# set -e

# # ======= CONFIGURATION =======
# TARGET_ID="C0B9JT8Q066"  # Private Slack ID for testing
# CREDS_DIR="$HOME/.config/slack"
# REPO_ORG="migtools"
# REPO_NAME="crane"
# MY_GITHUB_USER="Tamar-Dinavetsky"
# # =============================

# # Get commit message and branch name from arguments
# COMMIT_MSG=$1
# BRANCH_NAME=$2

# if [ -z "$COMMIT_MSG" ] || [ -z "$BRANCH_NAME" ]; then
#     echo "❌ Error: Missing commit message or branch name."
#     echo "Usage: ./scripts/ship_it.sh 'your commit message' 'your-branch-name'"
#     exit 1
# fi

# # Ensure we have personal remote configured
# if ! git remote | grep -q "^personal$"; then
#     git remote add personal "https://github.com/$MY_GITHUB_USER/$REPO_NAME.git"
# fi

# echo "🚀 Checking status for branch: $BRANCH_NAME..."
# # מעבר לבראנץ' המבוקש; אם הוא לא קיים מקומית, ניצור אותו
# git checkout "$BRANCH_NAME" 2>/dev/null || git checkout -b "$BRANCH_NAME"

# # 1. Git Commit
# echo "💾 Committing changes..."
# git add .
# git commit -m "$COMMIT_MSG" --allow-empty

# # 2. Check if branch already exists on remote to determine mode (New vs Update)
# if git ls-remote --heads personal "$BRANCH_NAME" | grep -q "$BRANCH_NAME"; then
#     IS_UPDATE_MODE=true
#     echo "🔄 Mode: Updating an existing branch."
# else
#     IS_UPDATE_MODE=false
#     echo "🌿 Mode: New branch detected."
# fi

# # 3. Push code to personal fork
# echo "📦 Pushing code to personal remote branch..."
# git push personal "$BRANCH_NAME" --force
# echo "✅ Code successfully pushed!"

# # 4. Handle PR and Slack based on your logic
# if [ "$IS_UPDATE_MODE" = true ]; then
#     # סוג א': רק עדכון - אין צורך ב-PR חדש ואין צורך לשלוח הודעה בסלאק
#     echo "⏭️ Branch updated successfully. Skipping Slack notification (team will see updates in GitHub automatically)."
# else
#     # סוג ב': בראנץ' חדש - פותחים PR אוטומטי ושולחים הודעה לסלאק רק אחרי שיש קישור חי
#     echo "🌿 Opening an official Pull Request on $REPO_ORG/$REPO_NAME via GitHub API..."
    
#     PR_URL=$(gh pr create \
#         --repo "$REPO_ORG/$REPO_NAME" \
#         --head "$MY_GITHUB_USER:$BRANCH_NAME" \
#         --base "main" \
#         --title "$COMMIT_MSG" \
#         --body "Automated PR created by crane pipeline." 2>/dev/null || echo "")

#     # אם ה-PR כבר היה פתוח במקרה, נשלוף את הקישור הקיים שלו
#     if [ -z "$PR_URL" ]; then
#         PR_URL=$(gh pr view --repo "$REPO_ORG/$REPO_NAME" --json url --jq .url 2>/dev/null || echo "")
#     fi

#     if [ -n "$PR_URL" ]; then
#         echo "🔗 Real PR Created successfully! URL: $PR_URL"
#         echo "💬 Preparing Slack message with the live link..."
        
#         SLACK_MESSAGE="📢 *New PR is ready for Review!*

# • *Topic:* $COMMIT_MSG
# • *Link:* <$PR_URL|Click here to view the PR on $REPO_ORG/$REPO_NAME>"
        
#         export SLACK_MESSAGE
#         export TARGET_ID
#         export CREDS_DIR

#         echo "📡 Triggering local Slack post tool..."
#         python3 -c "
# import os
# import sys
# sys.path.append('scripts') 
# from slack import post_message
# msg = os.environ.get('SLACK_MESSAGE')
# target = os.environ.get('TARGET_ID')
# creds = os.environ.get('CREDS_DIR')
# post_message(target, msg, creds)
# "
#         echo "🎉 Live PR link successfully sent to Slack!"
#     else
#         echo "⚠️ Could not open automated PR via 'gh CLI'. Falling back to safe simulation link."
#         PR_URL="https://github.com/$REPO_ORG/$REPO_NAME/compare/main...$MY_GITHUB_USER:$BRANCH_NAME"
#         echo "🔗 Simulated Link: $PR_URL"
#     fi
# fi

# echo "🏁 Process completed successfully!"

#!/bin/bash

# Stop the script if any command fails
set -e

# ======= CONFIGURATION =======
TARGET_ID="C0B9JT8Q066"  # Private Slack ID for testing
CREDS_DIR="$HOME/.config/slack"
REPO_ORG="migtools"
REPO_NAME="crane"
MY_GITHUB_USER="Tamar-Dinavetsky"
# =============================

# Get commit message and branch name from arguments
COMMIT_MSG=$1
BRANCH_NAME=$2

if [ -z "$COMMIT_MSG" ] || [ -z "$BRANCH_NAME" ]; then
    echo "❌ Error: Missing commit message or branch name."
    echo "Usage: ./scripts/ship_it.sh 'your commit message' 'your-branch-name'"
    exit 1
fi

# Ensure we have personal remote configured
if ! git remote | grep -q "^personal$"; then
    git remote add personal "https://github.com/$MY_GITHUB_USER/$REPO_NAME.git"
fi

echo "🚀 Checking status for branch: $BRANCH_NAME..."
git checkout "$BRANCH_NAME" 2>/dev/null || git checkout -b "$BRANCH_NAME"

# 1. Git Commit
echo "💾 Committing changes..."
git add .
git commit -m "$COMMIT_MSG" --allow-empty

# 2. Check if branch already exists on remote to determine mode (New vs Update)
if git ls-remote --heads personal "$BRANCH_NAME" | grep -q "$BRANCH_NAME"; then
    IS_UPDATE_MODE=true
    echo "🔄 Mode: Updating an existing branch."
else
    IS_UPDATE_MODE=false
    echo "🌿 Mode: New branch detected."
fi

# 3. Push code to personal fork (100% safe, only visible on your private GitHub page)
echo "📦 Pushing code to personal remote branch..."
git push personal "$BRANCH_NAME" --force
echo "✅ Code successfully pushed to personal fork!"

# 4. Handle PR Simulation and Slack based on your logic
if [ "$IS_UPDATE_MODE" = true ]; then
    # סוג א': רק עדכון - הדפסה בלבד בטרמינל, בלי הודעה בסלאק!
    echo "⏭️ Branch updated successfully. Skipping Slack notification (team will see updates in GitHub automatically)."
else
    # סוג ב': בראנץ' חדש - מייצרים קישור השוואה בטוח ושולחים הודעה חגיגית לסלאק!
    echo "🔍 Simulating automated PR path (Safe mode - no real PR created on GitHub)..."
    
    # הקישור הבא הוא קישור השוואה (Compare) - הוא מציג את הקוד שלך מול ה-main של העבודה,
    # אבל הוא לא פותח PR רשמי ולא שולח התראות לאף אחד!
    PR_URL="https://github.com/$REPO_ORG/$REPO_NAME/compare/main...$MY_GITHUB_USER:$BRANCH_NAME"

    echo "🔗 Simulated PR Link Generated: $PR_URL"
    echo "💬 Preparing Slack message..."
    
    SLACK_MESSAGE="📢 *New PR is ready for Review! (Simulation)*

• *Topic:* $COMMIT_MSG
• *Branch:* \`$BRANCH_NAME\`
• *Link:* <$PR_URL|Click here to view the changes on $REPO_ORG/$REPO_NAME>"
    
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
    echo "🎉 Simulation PR link successfully sent to your private Slack channel!"
fi

echo "🏁 Process completed successfully!"