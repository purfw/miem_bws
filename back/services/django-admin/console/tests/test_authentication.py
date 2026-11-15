from concurrent.futures import ThreadPoolExecutor
from dataclasses import replace
from pathlib import Path
from unittest.mock import patch
import os
import uuid

import pyotp
import pytest
from django.core.cache import cache
from django.test import Client

from console import authentication
from console.credentials import (
    change_account,
    get_credentials,
    save_credentials,
)


def login(client, credentials, otp=None, password="Test-password-2026"):
    return client.post(
        "/admin/login/",
        {
            "username": credentials.username,
            "password": password,
            "otp": otp or pyotp.TOTP(credentials.totp_secret).now(),
        },
    )


def test_code_can_only_be_used_once(credentials):
    code = pyotp.TOTP(credentials.totp_secret).now()
    assert login(Client(), credentials, code).status_code == 302
    repeated = login(Client(), credentials, code)
    assert repeated.status_code == 200
    assert "Неверный" in repeated.content.decode()


def test_only_one_concurrent_login_consumes_code(credentials):
    code = pyotp.TOTP(credentials.totp_secret).now()
    with ThreadPoolExecutor(max_workers=2) as executor:
        statuses = list(
            executor.map(
                lambda _: login(Client(), credentials, code).status_code,
                range(2),
            )
        )
    assert sorted(statuses) == [200, 302]


def test_wrong_password_does_not_consume_code(credentials):
    code = pyotp.TOTP(credentials.totp_secret).now()
    assert (
        login(Client(), credentials, code, password="wrong").status_code == 200
    )
    assert login(Client(), credentials, code).status_code == 302


def test_expired_code_is_rejected(credentials):
    with patch("console.authentication.time.time", return_value=120):
        code = pyotp.TOTP(credentials.totp_secret).at(30)
        assert not authentication.verify_credentials(
            credentials.username, "Test-password-2026", code
        )


def test_lockout_is_not_bypassed_by_changing_forwarded_ip(credentials):
    client = Client()
    for _ in range(5):
        assert login(client, credentials, password="wrong").status_code == 200
    response = client.post(
        "/admin/login/",
        {
            "username": "another-admin",
            "password": "wrong",
            "otp": "000000",
        },
        HTTP_X_FORWARDED_FOR="203.0.113.9",
    )
    assert "Слишком много попыток" in response.content.decode()


def test_logout_revokes_a_copied_session(credentials):
    client = Client()
    assert login(client, credentials).status_code == 302
    stolen = Client()
    stolen.cookies = client.cookies.copy()
    assert client.post("/admin/logout/").status_code == 302
    response = stolen.get("/admin/")
    assert response.status_code == 302
    assert response.url == "/admin/login/"


def test_rotation_revokes_existing_sessions(credentials):
    client = Client()
    assert login(client, credentials).status_code == 302
    changed = replace(
        credentials, version="dcfaafc7-b155-47d4-8260-c4f9d2ba43de"
    )
    with patch("console.authentication.get_credentials", return_value=changed):
        response = client.get("/admin/")
    assert response.status_code == 302
    assert response.url == "/admin/login/"


def test_csrf_is_required_for_login(credentials):
    client = Client(enforce_csrf_checks=True)
    assert login(client, credentials).status_code == 403


def test_cache_unavailability_fails_closed(credentials):
    with patch.object(
        cache, "add", side_effect=ConnectionError("redis unavailable")
    ):
        try:
            authentication.reserve_login_attempt()
        except ConnectionError:
            pass
        else:
            raise AssertionError(
                "Authentication must not bypass a failed replay "
                "or rate-limit store"
            )


def test_loaded_credentials_are_cached(credentials):
    assert get_credentials() == credentials


@pytest.mark.parametrize("change", ["rotate", "disable"])
def test_account_change_during_login_does_not_grant_a_new_session(
    credentials, change
):
    directory = Path(os.environ["ADMIN_AUTH_FILE"]).parent
    verify = authentication.verify_credentials

    def verify_then_change(*args):
        verified = verify(*args)
        assert verified
        if change == "rotate":
            save_credentials(
                directory,
                replace(
                    credentials,
                    totp_secret=pyotp.random_base32(),
                    version=str(uuid.uuid4()),
                ),
            )
        else:
            change_account(directory, credentials.username, active=False)
        return verified

    if change == "disable":
        from .test_roles import second_account

        second_account(credentials, "superadmin")
    client = Client()
    with patch(
        "console.views.verify_credentials", side_effect=verify_then_change
    ):
        response = login(client, credentials)
    assert response.status_code == 200
    assert "Неверный" in response.content.decode()
    assert not client.session.get("admin_2fa")
