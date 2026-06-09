#!/bin/bash

# עצירת הסקריפט במקרה של שגיאה
set -e

# ======= הגדרות לצורך הניסוי =======
# 🔴 החליפי את ה-ID הבא ב-Member ID האישי שלך (מתחיל ב-U) כדי שההודעה תגיע רק אלייך!
TARGET_ID="U0B2U4W6UE4"

# נתיב המפתחות הסודיים שחולצו בשלב 1
CREDS_DIR="$HOME/.local/share/slack-mcp"
# ==================================

# קבלת הודעת קומיט ושם הבראנץ'
COMMIT_MSG=$1
BRANCH_NAME=$2

if [ -z "$COMMIT_MSG" ] || [ -z "$BRANCH_NAME" ]; then
    echo "❌ שגיאה: יש לספק הודעת קומיט ושם בראנץ'."
    echo "שימוש: ./ship_it.sh 'הודעת קומיט' 'שם-הבראנץ'"
    exit 1
fi

echo "🚀 מתחיל תהליך אוטומציה לבראנץ': $BRANCH_NAME..."

# 1. טיפול בבראנץ' (מעבר או יצירה)
if git show-ref --verify --quiet "refs/heads/$BRANCH_NAME"; then
    echo "📝 הבראנץ' כבר קיים. עובר אליו..."
    git checkout "$BRANCH_NAME"
else
    echo "🌿 יוצר בראנץ' חדש..."
    git checkout -b "$BRANCH_NAME"
fi

# 2. גיט: הוספה וקומיט
git add .
git commit -m "$COMMIT_MSG" --allow-empty

# 3. גיט: פוש לגיטהאב
echo "📦 דוחף את הקוד לגיט..."
git push origin "$BRANCH_NAME"

# 4. גיטהאב: בדיקה אם קיים PR פתוח לבראנץ' הזה
echo "🔍 בודק סטטוס Pull Request בגיטהאב..."
EXISTING_PR=$(gh pr list --head "$BRANCH_NAME" --json url --jq '.[0].url')

IS_NEW_PR=true
if [ -z "$EXISTING_PR" ]; then
    echo "📝 מייצר Pull Request חדש..."
    PR_URL=$(gh pr create --title "$COMMIT_MSG" --body "Automated PR created via Claude Code." --base main --head "$BRANCH_NAME")
    echo "✅ ה-PR נוצר בהצלחה: $PR_URL"
else
    PR_URL=$EXISTING_PR
    IS_NEW_PR=false
    echo "🔄 ה-PR כבר קיים בכתובת: $PR_URL"
fi

# 5. השאלה הגדולה: האם לשלוח הודעה לסלאק?
echo ""
if [ "$IS_NEW_PR" = true ]; then
    read -p "❓ נוצר PR חדש! האם לשלוח הודעת עדכון לסלאק? (y/n): " ANSWER
else
    read -p "❓ ה-PR עודכן בקוד חדש! האם לשלוח הודעת עדכון על השינוי לסלאק? (y/n): " ANSWER
fi
echo ""

if [[ "$ANSWER" =~ ^[Yy]$ ]]; then
    echo "💬 מכין את ההודעה לסלאק..."
    
    # התאמת נוסח ההודעה לפי סוג הפעולה (PR חדש או עדכון)
    if [ "$IS_NEW_PR" = true ]; then
        SLACK_MESSAGE="📢 *PR חדש מוכן ל-Review!* \n\n*נושא:* $COMMIT_MSG\n*בראנץ':* \`$BRANCH_NAME\`\n🔗 *קישור:* $PR_URL"
    else
        SLACK_MESSAGE="🔄 *הקוד ב-PR עודכן!* \n\n*מה השתנה:* $COMMIT_MSG\n*בראנץ':* \`$BRANCH_NAME\`\n🔗 *קישור:* $PR_URL"
    fi
    
    # הרצת קוד הפייתון המובנה של החברה שלך כדי לשלוח את ההודעה
    python3 -c "
import sys
sys.path.append('.')
from slack_mcp_server import post_message
post_message('$TARGET_ID', '$SLACK_MESSAGE', '$CREDS_DIR')
"
    echo "🎉 הודעת הניסוי נשלחה בהצלחה לסלאק הפרטי שלך!"
else
    echo "⏭️ מדלג על שליחת ההודעה בסלאק לפי בקשתך."
fi

echo "🏁 הכל סתיים בהצלחה!"