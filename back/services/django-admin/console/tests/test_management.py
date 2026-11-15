import os
import uuid
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path
from urllib.parse import parse_qs, urlsplit
from unittest.mock import patch

import pyotp
import pytest
from django.core.cache import cache
from django.test import Client

from console.client import ServiceError
from console.credentials import change_account, get_credentials, load_accounts
from .test_authentication import login
from .test_roles import second_account


def invite(client, username="new-moderator", role="moderator"):
    result = client.post(
        "/admin/team/invite/", {"username": username, "role": role}
    )
    assert result.status_code == 302
    url = client.session["admin_created_invite"]["url"]
    parsed = urlsplit(url)
    return parsed.path + "?" + parsed.query


def enroll(client, url):
    assert client.get(url).status_code == 200
    seed = next(
        value
        for key, value in client.session.items()
        if key.startswith("admin_setup_")
    )
    return client.post(
        url,
        {
            "password": "New-moderator-password-2026",
            "password_confirm": "New-moderator-password-2026",
            "otp": pyotp.TOTP(seed).now(),
        },
    )


def test_invitation_requires_own_password_and_totp_and_is_single_use(
    credentials,
):
    manager = Client()
    assert login(manager, credentials).status_code == 302
    url = invite(manager)
    assert get_credentials("new-moderator") is None
    person = Client()
    response = person.get(url)
    assert response.status_code == 200
    assert response.headers["Referrer-Policy"] == "same-origin"
    seed = next(
        value
        for key, value in person.session.items()
        if key.startswith("admin_setup_")
    )
    correct = pyotp.TOTP(seed).now()
    wrong = correct[:-1] + str((int(correct[-1]) + 1) % 10)
    body = {
        "password": "New-moderator-password-2026",
        "password_confirm": "New-moderator-password-2026",
        "otp": wrong,
    }
    assert person.post(url, body).status_code == 200
    assert get_credentials("new-moderator") is None
    body["otp"] = correct
    assert person.post(url, body).status_code == 302
    account = get_credentials("new-moderator")
    assert account.roles == ("moderator",)
    assert account.totp_secret == seed != credentials.totp_secret
    assert get_credentials(credentials.username) == credentials
    assert person.get(url).status_code == 410
    assert (
        login(Client(), account, password=body["password"]).status_code == 302
    )
    cipher = Path(os.environ["ADMIN_AUTH_FILE"]).read_bytes()
    assert (
        seed.encode() not in cipher and body["password"].encode() not in cipher
    )


def test_new_invitation_revokes_previous_link_and_expired_links_fail(
    credentials,
):
    manager = Client()
    assert login(manager, credentials).status_code == 302
    old = invite(manager)
    new = invite(manager)
    assert Client().get(old).status_code == 410
    assert Client().get(new).status_code == 200
    cache.clear()
    assert Client().get(new).status_code == 410


def test_creator_demotion_or_rotation_invalidates_pending_invitation(
    credentials,
):
    manager = Client()
    assert login(manager, credentials).status_code == 302
    url = invite(manager)
    change_account(
        Path(os.environ["ADMIN_AUTH_FILE"]).parent,
        credentials.username,
        roles=credentials.roles,
    )
    assert Client().get(url).status_code == 410


def test_invitation_rate_limit_and_no_secret_leaks_on_team_page(credentials):
    manager = Client()
    assert login(manager, credentials).status_code == 302
    team = manager.get("/admin/team/").content
    assert credentials.totp_secret.encode() not in team
    assert credentials.password_hash.encode() not in team
    url = invite(manager)
    person = Client()
    for _ in range(5):
        assert person.post(url, {}).status_code == 200
    response = person.post(url, {})
    assert response.status_code == 429
    assert response.headers["Retry-After"] == "300"
    assert get_credentials("new-moderator") is None


@pytest.mark.parametrize("role", ["moderator", "operator", "auditor"])
def test_staff_cannot_manage_users_or_team(credentials, role):
    account = second_account(credentials, role)
    client = Client()
    assert login(client, account).status_code == 302
    assert client.get("/admin/team/").status_code == 403
    assert client.get("/admin/users/").status_code == 403
    assert (
        client.post(
            "/admin/team/invite/",
            {"username": "escalated", "role": "superadmin"},
        ).status_code
        == 403
    )
    with patch("console.users.call") as call:
        assert (
            client.post(
                "/admin/users/access/",
                {
                    "action": "role",
                    "user_id": str(uuid.uuid4()),
                    "role": "admin",
                },
            ).status_code
            == 403
        )
        call.assert_not_called()
    assert get_credentials("escalated") is None


def test_changes_revoke_only_target_sessions_and_reject_stale_edits(
    credentials,
):
    target = second_account(credentials)
    manager, person = Client(), Client()
    assert login(manager, credentials).status_code == 302
    assert login(person, target).status_code == 302
    path = f"/admin/team/{target.actor_id}/access/"
    assert (
        manager.post(
            path,
            {
                "action": "roles",
                "roles": ["auditor"],
                "version": target.version,
            },
        ).status_code
        == 302
    )
    assert get_credentials(target.username).roles == ("auditor",)
    revoked = person.get("/admin/")
    assert revoked.status_code == 302
    assert revoked.url == "/admin/login/"
    assert manager.get("/admin/team/").status_code == 200
    assert (
        manager.post(
            path, {"action": "disable", "version": target.version}
        ).status_code
        == 302
    )
    assert get_credentials(target.username).active
    assert manager.session.get("admin_team_error")
    updated = get_credentials(target.username)
    assert (
        manager.post(
            path, {"action": "disable", "version": updated.version}
        ).status_code
        == 302
    )
    assert get_credentials(target.username) is None
    assert (
        manager.post(
            f"/admin/team/{credentials.actor_id}/access/",
            {"action": "disable", "version": credentials.version},
        ).status_code
        == 400
    )


def test_last_superadmin_guard_is_atomic(credentials):
    other = second_account(credentials, "superadmin")
    directory = Path(os.environ["ADMIN_AUTH_FILE"]).parent

    def disable(name):
        try:
            change_account(directory, name, active=False)
            return True
        except ValueError:
            return False

    with ThreadPoolExecutor(max_workers=2) as executor:
        assert sorted(
            executor.map(disable, [credentials.username, other.username])
        ) == [False, True]
    accounts = load_accounts(directory / "credentials.enc", directory / "key")
    assert sum(a.active and "superadmin" in a.roles for a in accounts) == 1


def test_user_search_role_update_and_invalid_role_rejection(credentials):
    manager = Client()
    assert login(manager, credentials).status_code == 302
    target = str(uuid.uuid4())
    data = {
        "data": [
            {
                "id": target,
                "email": "person@example.test",
                "first_name": "Анна",
                "last_name": "Тестовая",
                "role": "user",
                "is_active": True,
                "email_verified": False,
            }
        ],
        "has_more": True,
    }
    with patch("console.users.call", return_value=data) as call:
        response = manager.get("/admin/users/?q=person%40example.test&page=2")
        assert response.status_code == 200
        assert "person@example.test" in response.content.decode()
        assert "Пригласить в модераторы" in response.content.decode()
        assert (
            call.call_args.args[2]
            == "/internal/users?q=person%40example.test&page=2"
        )
    with patch("console.users.call") as call:
        assert (
            manager.post(
                "/admin/users/access/",
                {
                    "action": "role",
                    "user_id": target,
                    "role": "verified_organizer",
                },
            ).status_code
            == 302
        )
        assert call.call_args.args[3] == {
            "role": "verified_organizer",
            "changed_by": credentials.actor_id,
        }
        call.reset_mock()
        assert (
            manager.post(
                "/admin/users/access/",
                {"action": "role", "user_id": target, "role": "superadmin"},
            ).status_code
            == 400
        )
        call.assert_not_called()


def test_audit_outage_does_not_issue_admin_invitation(credentials):
    manager = Client()
    assert login(manager, credentials).status_code == 302
    with patch("console.team.record", side_effect=ServiceError("unavailable")):
        response = manager.post(
            "/admin/team/invite/",
            {"username": "no-audit", "role": "moderator"},
        )
    assert response.status_code == 503
    assert "admin_created_invite" not in manager.session
    assert get_credentials("no-audit") is None


def test_team_and_invitation_posts_require_csrf(credentials):
    manager = Client(enforce_csrf_checks=True)
    session = manager.session
    session.update(
        {
            "admin_username": credentials.username,
            "admin_2fa": True,
            "admin_auth_version": credentials.version,
        }
    )
    session.save()
    assert (
        manager.post(
            "/admin/team/invite/", {"username": "no-csrf", "role": "moderator"}
        ).status_code
        == 403
    )

    assert (
        manager.post(
            "/admin/users/access/",
            {"action": "disable", "user_id": str(uuid.uuid4())},
        ).status_code
        == 403
    )


@pytest.mark.parametrize("action", ["role", "disable", "enable"])
def test_user_actions_keep_search_and_page(credentials, action):
    manager = Client()
    assert login(manager, credentials).status_code == 302
    with patch("console.users.call"):
        response = manager.post(
            "/admin/users/access/",
            {
                "action": action,
                "user_id": str(uuid.uuid4()),
                "role": "user",
                "q": "Иван",
                "page": "3",
                "next": "https://example.test/",
            },
        )
    assert response.status_code == 302
    assert urlsplit(response.url).path == "/admin/users/"
    assert parse_qs(urlsplit(response.url).query) == {
        "q": ["Иван"],
        "page": ["3"],
        "notice": ["updated"],
    }


@pytest.mark.parametrize("section", ["users", "team"])
def test_management_pages_ignore_unknown_notice_codes(credentials, section):
    manager = Client()
    assert login(manager, credentials).status_code == 302
    with patch("console.users.call", return_value={"data": []}):
        response = manager.get(f"/admin/{section}/?notice=unknown")
    assert b'role="status"' not in response.content


def test_account_disabled_while_loading_section_does_not_crash(credentials):
    second_account(credentials, "superadmin")
    manager = Client()
    assert login(manager, credentials).status_code == 302

    def load_then_disable(*args):
        change_account(
            Path(os.environ["ADMIN_AUTH_FILE"]).parent,
            credentials.username,
            active=False,
        )
        return {"data": []}

    with patch("console.views.call", side_effect=load_then_disable) as call:
        assert manager.get("/admin/verifications/").status_code == 200
        denied = manager.get("/admin/verifications/")
    assert denied.status_code == 302
    assert denied.url == "/admin/login/"
    call.assert_called_once()
