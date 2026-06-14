#!/usr/bin/env python3
"""Native messaging host for Slack Token Extractor Chrome extension.

Receives xoxc token and d cookie from the extension and saves them
to ~/.config/slack/ with restricted permissions.
"""

import json
import os
import struct
import sys
from pathlib import Path

CONFIG_DIR = Path.home() / ".config" / "slack"


def read_message():
    """Read a native messaging message from stdin."""
    raw_length = sys.stdin.buffer.read(4)
    if not raw_length:
        sys.exit(0)
    length = struct.unpack("@I", raw_length)[0]
    data = sys.stdin.buffer.read(length)
    return json.loads(data.decode("utf-8"))


def send_message(msg):
    """Send a native messaging message to stdout."""
    encoded = json.dumps(msg).encode("utf-8")
    sys.stdout.buffer.write(struct.pack("@I", len(encoded)))
    sys.stdout.buffer.write(encoded)
    sys.stdout.buffer.flush()


def main():
    msg = read_message()

    xoxc_token = msg.get("xoxc_token", "")
    d_cookie = msg.get("d_cookie", "")

    if not xoxc_token or not d_cookie:
        send_message({"success": False, "message": "Missing token or cookie"})
        return

    try:
        CONFIG_DIR.mkdir(parents=True, exist_ok=True)

        token_file = CONFIG_DIR / "xoxc_token"
        cookie_file = CONFIG_DIR / "d_cookie"

        token_file.write_text(xoxc_token)
        cookie_file.write_text(d_cookie)

        os.chmod(token_file, 0o600)
        os.chmod(cookie_file, 0o600)

        send_message({
            "success": True,
            "message": f"Tokens saved to {CONFIG_DIR}/",
        })
    except Exception as e:
        send_message({"success": False, "message": str(e)})


if __name__ == "__main__":
    main()
