PYTHON_BIN=$(which python3 || echo "/usr/bin/python3")
exec "$PYTHON_BIN" "/Users/tdinavet/Documents/Crane-migration/crane/slack-creds-extractor-main/native-host/save_tokens.py" "$@"
