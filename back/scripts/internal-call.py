#!/usr/bin/env python3
"""Usage: internal-call.py METHOD https://SERVICE:8443/internal/PATH [< body.json]"""

import hashlib
import hmac
import os
import sys
import time
import urllib.error
import urllib.request
import uuid
from pathlib import Path
from urllib.parse import urlsplit

from ops_transport import SERVICES, internal_opener

if len(sys.argv) != 3:
    sys.exit(__doc__)
root = Path(__file__).resolve().parent.parent
values = dict(
    line.split("=", 1)
    for line in (root / ".env.prod").read_text().splitlines()
    if "=" in line and not line.startswith("#")
)
secret = os.getenv("SERVICE_TOKEN_SECRET") or values["SERVICE_TOKEN_SECRET"]
method, url = sys.argv[1].upper(), sys.argv[2]
parsed = urlsplit(url)
if (
    parsed.scheme != "https"
    or parsed.hostname not in SERVICES
    or parsed.port != 8443
    or not parsed.path.startswith("/internal/")
):
    sys.exit("A known mTLS internal service URL is required")
body = b"" if sys.stdin.isatty() else sys.stdin.buffer.read()
timestamp, nonce = str(int(time.time())), str(uuid.uuid4())
target = parsed.path + ("?" + parsed.query if parsed.query else "")
message = (
    f"v2\n{method}\n{target}\n{timestamp}\n{nonce}\n{hashlib.sha256(body).hexdigest()}"
)
digest = hmac.new(secret.encode(), message.encode(), hashlib.sha256).hexdigest()
request = urllib.request.Request(
    url,
    data=body or None,
    method=method,
    headers={
        "Content-Type": "application/json",
        "X-Service-Token": f"v2.{timestamp}.{nonce}.{digest}",
    },
)
try:
    with internal_opener(root).open(request, timeout=20) as response:
        sys.stdout.buffer.write(response.read())
except urllib.error.HTTPError as error:
    sys.stderr.write(f"Internal API returned HTTP {error.code}\n")
    sys.exit(1)
