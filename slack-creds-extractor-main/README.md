# Slack Token Extractor

Chrome extension that extracts Slack session tokens (`xoxc-` token and `d` cookie) and saves them to `~/.config/slack/` via native messaging.

## Why

Slack's web client uses session tokens for API authentication. This extension extracts them automatically so external scripts can use the Slack API authenticated as you — without needing a Slack App with workspace admin approval.

## How It Works

- **`d` cookie**: Read via Chrome's `cookies` API (scoped to `*.slack.com` only)
- **`xoxc-` token**: Read from Slack's `localStorage` by injecting a content script into the page
- **Native messaging**: Tokens are passed to a Python script that writes them to disk with `chmod 600`
- **Auto-refresh**: Background service worker re-extracts every 6 hours via `chrome.alarms`

## Setup

### 1. Load the extension

1. Open `chrome://extensions`
2. Enable **Developer mode**
3. Click **Load unpacked** → select this directory
4. Note the **extension ID**

### 2. Install the native messaging host

```bash
./native-host/install.sh <extension-id>
```

### 3. Extract tokens

- **Automatic**: Tokens are extracted every 6 hours while Chrome is running with a Slack tab open
- **Manual**: Click the extension icon → "Extract & Save Tokens"

## Token Storage

Tokens are saved to:

- `~/.config/slack/xoxc_token`
- `~/.config/slack/d_cookie`

Both files are created with `600` permissions (readable only by the owner).

## Permissions

- `cookies` — Read the `d` cookie from `*.slack.com` (HttpOnly, not accessible via JavaScript)
- `scripting` — Inject content script to read `xoxc-` token from localStorage
- `alarms` — Schedule periodic auto-extraction
- `nativeMessaging` — Communicate with the Python script that saves tokens to disk
- `host_permissions: *.slack.com` — Scopes all access to Slack domains only
