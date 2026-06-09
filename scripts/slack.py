"""Slack message posting via xoxc token + browser cookie auth.

Posts messages to Slack channels using the undocumented xoxc/cookie
authentication method, matching the pattern used by daily-status.
"""

import json
import logging
import urllib.parse
import urllib.request
from pathlib import Path

_SLACK_API = "https://slack.com/api/chat.postMessage"

log = logging.getLogger(__name__)


def post_message(channel: str, text: str, creds_dir: str) -> None:
    """Post a message to a Slack channel.

    Args:
        channel: Slack channel ID (e.g. C08ESMFV85Q).
        text: Message text (Slack mrkdwn format).
        creds_dir: Directory containing ``xoxc_token`` and ``d_cookie`` files.

    Raises:
        SystemExit: On missing credential files or Slack API auth errors.
    """
    creds_path = Path(creds_dir)
    token_file = creds_path / "xoxc_token"
    cookie_file = creds_path / "d_cookie"

    for path, name in [(token_file, "xoxc_token"), (cookie_file, "d_cookie")]:
        if not path.is_file():
            raise SystemExit(
                f"Missing Slack credential file: {path}\n"
                f"Create '{name}' in {creds_dir} with the appropriate value."
            )

    xoxc_token = token_file.read_text().strip()
    d_cookie = cookie_file.read_text().strip()

    data = urllib.parse.urlencode(
        {
            "token": xoxc_token,
            "channel": channel,
            "text": text,
            "unfurl_links": "false",
        }
    ).encode()

    req = urllib.request.Request(
        _SLACK_API,
        data=data,
        headers={
            "Content-Type": "application/x-www-form-urlencoded",
            "Cookie": f"d={d_cookie}",
        },
    )

    with urllib.request.urlopen(req, timeout=30) as resp:
        result = json.loads(resp.read().decode())

    if not result.get("ok"):
        error = result.get("error", "unknown")
        if error in ("invalid_auth", "token_revoked", "not_authed"):
            raise SystemExit(
                f"Slack auth error: {error}\n"
                "The xoxc token or d cookie has expired.\n"
                f"Refresh the credentials in {creds_dir}."
            )
        raise SystemExit(f"Slack API error: {error}")

    ts = result.get("ts", "")
    log.info("Posted to Slack (channel=%s, ts=%s)", channel, ts)
