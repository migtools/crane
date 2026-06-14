#!/bin/bash
# Install the native messaging host for the Slack Token Extractor extension.
#
# This registers the native host with Chrome so the extension can
# communicate with the save_tokens.py script.

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
HOST_NAME="com.slack.token_saver"
NATIVE_HOST_SCRIPT="$SCRIPT_DIR/save_tokens.py"
CHROME_NATIVE_DIR="$HOME/.config/google-chrome/NativeMessagingHosts"
MANIFEST_PATH="$CHROME_NATIVE_DIR/$HOST_NAME.json"

# Make save_tokens.py executable
chmod +x "$NATIVE_HOST_SCRIPT"

# Create Chrome native messaging hosts directory
mkdir -p "$CHROME_NATIVE_DIR"

# Get the extension ID - user needs to provide it after loading the extension
EXTENSION_ID="${1:-}"

if [ -z "$EXTENSION_ID" ]; then
    echo "Usage: $0 <extension-id>"
    echo ""
    echo "To find the extension ID:"
    echo "  1. Open chrome://extensions"
    echo "  2. Enable Developer mode"
    echo "  3. Load the extension (Load unpacked -> select the extension folder)"
    echo "  4. Copy the ID shown under the extension name"
    echo "  5. Run: $0 <that-id>"
    exit 1
fi

# Write the native messaging host manifest
cat > "$MANIFEST_PATH" <<EOF
{
  "name": "$HOST_NAME",
  "description": "Save Slack tokens to ~/.config/slack/",
  "path": "$NATIVE_HOST_SCRIPT",
  "type": "stdio",
  "allowed_origins": [
    "chrome-extension://$EXTENSION_ID/"
  ]
}
EOF

echo "Native messaging host installed:"
echo "  Manifest: $MANIFEST_PATH"
echo "  Script:   $NATIVE_HOST_SCRIPT"
echo ""
echo "The extension can now save tokens to ~/.config/slack/"
