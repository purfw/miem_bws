import hashlib
import hmac
import json
import os
import ssl
import time
import urllib.error
import urllib.request
import uuid


class ServiceError(RuntimeError):
    pass


def _token(method: str, path: str, body: bytes) -> str:
    timestamp = str(int(time.time()))
    nonce = str(uuid.uuid4())
    body_hash = hashlib.sha256(body).hexdigest()
    message = f"v2\n{method}\n{path}\n{timestamp}\n{nonce}\n{body_hash}"
    digest = hmac.new(
        os.environ["SERVICE_TOKEN_SECRET"].encode(),
        message.encode(),
        hashlib.sha256,
    ).hexdigest()
    return f"v2.{timestamp}.{nonce}.{digest}"


def call(base_env: str, method: str, path: str, payload=None):
    body = (
        b""
        if payload is None
        else json.dumps(
            payload, ensure_ascii=False, separators=(",", ":")
        ).encode()
    )
    request = urllib.request.Request(
        os.environ[base_env].rstrip("/") + path,
        data=body if method != "GET" else None,
        method=method,
        headers={
            "X-Service-Token": _token(method, path, body),
            "Content-Type": "application/json",
        },
    )
    try:
        context = None
        if os.getenv("MTLS_CA_FILE"):
            if not request.full_url.startswith("https://"):
                raise ServiceError("plaintext internal request refused")
            context = ssl.create_default_context(
                cafile=os.environ["MTLS_CA_FILE"]
            )
            context.minimum_version = ssl.TLSVersion.TLSv1_3
            context.load_cert_chain(
                os.environ["MTLS_CERT_FILE"], os.environ["MTLS_KEY_FILE"]
            )
        with urllib.request.urlopen(
            request, timeout=10, context=context
        ) as response:
            raw = response.read()
            try:
                return json.loads(raw) if raw else None
            except ValueError as exc:
                raise ServiceError(
                    "invalid internal service response"
                ) from exc
    except urllib.error.HTTPError as exc:
        raw = exc.read()
        try:
            problem = json.loads(raw)
            detail = (
                problem.get("detail") or problem.get("title")
                if isinstance(problem, dict)
                else None
            )
        except (ValueError, TypeError):
            detail = raw.decode(errors="replace")
        raise ServiceError(f"{exc.code}: {detail or 'service error'}") from exc
    except OSError as exc:
        raise ServiceError("internal service unavailable") from exc
