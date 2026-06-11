import os
import sqlite3
import shutil
import urllib.parse
import urllib.request
import re
from pathlib import Path
from cryptography.hazmat.primitives.ciphers import Cipher, algorithms, modes
from cryptography.hazmat.backends import default_backend
from cryptography.hazmat.primitives.kdf.pbkdf2 import PBKDF2HMAC
from cryptography.hazmat.primitives import hashes

def get_mac_slack_key():
    cmd = ["security", "find-generic-password", "-s", "Slack Safe Storage", "-w"]
    try:
        return subprocess.check_output(cmd, stderr=subprocess.DEVNULL).strip()
    except:
        import subprocess
        try: return subprocess.check_output(cmd, stderr=subprocess.DEVNULL).strip()
        except: return None

def decrypt_payload(key, ciphertext):
    if not ciphertext or not key: return None
    kdf = PBKDF2HMAC(algorithm=hashes.SHA1(), length=16, salt=b"saltysalt", iterations=1003, backend=default_backend())
    derived_key = kdf.derive(key)
    if ciphertext.startswith(b'v10') or ciphertext.startswith(b'v11'): ciphertext = ciphertext[3:]
    try:
        iv, payload, tag = ciphertext[:12], ciphertext[12:-16], ciphertext[-16:]
        cipher = Cipher(algorithms.AES(derived_key), modes.GCM(iv, tag), backend=default_backend())
        return (cipher.decryptor().update(payload) + cipher.decryptor().finalize()).decode('utf-8', errors='ignore')
    except: return None

def fetch_xoxc_via_api(d_cookie):
    """Dynamically fetches a fresh xoxc_token using the valid d_cookie."""
    try:
        req = urllib.request.Request(
            "https://app.slack.com/client",
            headers={"Cookie": f"d={d_cookie}", "User-Agent": "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)"}
        )
        with urllib.request.urlopen(req, timeout=10) as resp:
            html = resp.read().decode('utf-8', errors='ignore')
            # Look for the xoxc token pattern inside Slack's boot config data
            match = re.search(r'"api_token"\s*:\s*"(xoxc-[^"]+)"', html)
            if match: return match.group(1)
    except: pass
    return None

def refresh_mac_slack():
    home = Path.home()
    config_dir = home / ".config/slack"
    config_dir.mkdir(parents=True, exist_ok=True)
    
    print("🔄 Dynamic Scan: Authenticating and extracting secure Mac Slack session...")
    cookie_path = home / "Library/Application Support/Slack/Cookies"
    key = get_mac_slack_key()
    d_cookie = None
    
    if cookie_path.exists() and key:
        temp_db = "/tmp/slack_cookies"
        shutil.copyfile(cookie_path, temp_db)
        try:
            conn = sqlite3.connect(temp_db)
            cursor = conn.cursor()
            cursor.execute("SELECT encrypted_value, value FROM cookies WHERE name='d' AND host_key LIKE '%slack.com%'")
            row = cursor.fetchone()
            if row:
                decrypted = decrypt_payload(key, row[0]) if row[0] else row[1]
                if decrypted:
                    d_cookie = urllib.parse.unquote(decrypted).encode('latin-1', 'ignore').decode('latin-1')
            conn.close()
        except: pass

    if d_cookie:
        d_cookie = ''.join([c for c in d_cookie if 32 <= ord(c) < 127])
        # Save d_cookie
        with open(config_dir / "d_cookie", "w") as f: 
            f.write(f"xoxd-{d_cookie}" if not d_cookie.startswith("xoxd-") else d_cookie)
        print("✅ d_cookie automatically refreshed!")
        
        # Dynamically fetch the matching xoxc_token live from Slack!
        xoxc_token = fetch_xoxc_via_api(d_cookie)
        if xoxc_token:
            with open(config_dir / "xoxc_token", "w") as f: f.write(xoxc_token.strip())
            print("✅ xoxc_token automatically fetched via secure API session!")
            print("🎉 Automation Complete: Pipeline credentials are fully ready for use!")
            return True
            
    print("❌ Session validation failed. Please verify you are signed into the Slack App.")
    return False

if __name__ == "__main__":
    refresh_mac_slack()