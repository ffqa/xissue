#!/usr/bin/env python3
"""No-op lifecycle handler for the UI demo plugin."""
import json
import sys

json.load(sys.stdin)
print(json.dumps({"actions": []}))
