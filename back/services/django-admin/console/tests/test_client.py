from unittest.mock import MagicMock, patch
from urllib.error import HTTPError

import pytest

from console.client import ServiceError, call


@pytest.mark.parametrize("body", [b"invalid JSON", b"\xff"])
def test_invalid_service_json_is_reported_as_a_service_error(
    monkeypatch, body
):
    monkeypatch.setenv("IDENTITY_SVC_URL", "https://identity.example.test")
    monkeypatch.setenv("SERVICE_TOKEN_SECRET", "test-service-signing-key")
    response = MagicMock()
    response.__enter__.return_value.read.return_value = body
    with patch("console.client.urllib.request.urlopen", return_value=response):
        with pytest.raises(ServiceError):
            call("IDENTITY_SVC_URL", "GET", "/internal/users")


@pytest.mark.parametrize("body", [b"[]", b"null", b"invalid JSON"])
def test_unexpected_error_body_does_not_crash_client(monkeypatch, body):
    monkeypatch.setenv("IDENTITY_SVC_URL", "https://identity.example.test")
    monkeypatch.setenv("SERVICE_TOKEN_SECRET", "test-service-signing-key")
    error = HTTPError(
        "https://identity.example.test", 502, "Bad Gateway", {}, None
    )
    error.read = lambda: body
    with patch("console.client.urllib.request.urlopen", side_effect=error):
        with pytest.raises(ServiceError):
            call("IDENTITY_SVC_URL", "GET", "/internal/users")
