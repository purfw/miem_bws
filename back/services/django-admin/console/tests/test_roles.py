import uuid
from dataclasses import replace
from unittest.mock import patch

import pyotp
import pytest
from django.test import Client

from console.credentials import get_credentials, save_credentials
from .test_authentication import login


def second_account(credentials, role="moderator"):
    account = replace(
        credentials,
        username="second-admin",
        actor_id=str(uuid.uuid4()),
        version=str(uuid.uuid4()),
        totp_secret=pyotp.random_base32(),
        roles=(role,),
    )
    from pathlib import Path
    import os

    save_credentials(Path(os.environ["ADMIN_AUTH_FILE"]).parent, account)
    return account


def test_accounts_have_separate_totp_and_lockout(credentials):
    moderator = second_account(credentials)
    for _ in range(5):
        login(Client(), credentials, password="wrong")
    assert login(Client(), moderator).status_code == 302
    assert get_credentials(moderator.username) == moderator
    assert get_credentials() == credentials


@pytest.mark.parametrize("role", ["auditor", "operator"])
def test_read_only_roles_cannot_moderate(credentials, role):
    account = second_account(credentials, role)
    client = Client()
    assert login(client, account).status_code == 302
    with patch("console.views.call") as call:
        assert (
            client.post(
                f"/admin/applications/{uuid.uuid4()}/accepted/"
            ).status_code
            == 403
        )
        assert (
            client.post(
                "/admin/accounts/active/",
                {"user_id": str(uuid.uuid4()), "active": "false"},
            ).status_code
            == 403
        )
        call.assert_not_called()


def test_moderator_cannot_retry_jobs(credentials):
    account = second_account(credentials)
    client = Client()
    assert login(client, account).status_code == 302
    with patch("console.views.call") as call:
        assert (
            client.post(
                f"/admin/queues/{uuid.uuid4()}/retry/",
                {"service": "APPLICATION_SVC_URL", "queue": "outbox"},
            ).status_code
            == 403
        )
        call.assert_not_called()


def test_live_account_disabling_revokes_only_its_sessions(
    credentials, monkeypatch
):
    account = second_account(credentials)
    client = Client()
    other = Client()
    assert login(client, account).status_code == 302
    assert login(other, credentials).status_code == 302
    from pathlib import Path
    import os

    directory = Path(os.environ["ADMIN_AUTH_FILE"]).parent
    save_credentials(
        directory, replace(account, active=False, version=str(uuid.uuid4()))
    )
    revoked = client.get("/admin/")
    assert revoked.status_code == 302
    assert revoked.url == "/admin/login/"
    with (
        patch("console.views.call", return_value={}),
        patch("console.views._recent_audit", return_value=[]),
    ):
        assert other.get("/admin/", follow=True).status_code == 200


def test_last_superadmin_cannot_be_disabled(credentials):
    from pathlib import Path
    import os

    with pytest.raises(ValueError, match="last active superadmin"):
        save_credentials(
            Path(os.environ["ADMIN_AUTH_FILE"]).parent,
            replace(credentials, active=False),
        )
    assert get_credentials() == credentials


def test_authentication_audit_outage_fails_closed(credentials):
    from console.client import ServiceError

    client = Client()
    with patch(
        "console.views.record", side_effect=ServiceError("unavailable")
    ):
        assert login(client, credentials).status_code == 503
    assert not client.session.get("admin_2fa")


def test_moderation_uses_the_current_accounts_actor(credentials):
    account = second_account(credentials)
    client = Client()
    assert login(client, account).status_code == 302
    with patch("console.views.call") as call:
        assert (
            client.post(
                f"/admin/applications/{uuid.uuid4()}/accepted/"
            ).status_code
            == 302
        )
    assert call.call_args.args[3]["changed_by"] == account.actor_id


def test_operator_can_retry_quarantined_events(credentials):
    account = second_account(credentials, role="operator")
    client = Client()
    assert login(client, account).status_code == 302
    item = uuid.uuid4()
    with patch("console.views.call") as call:
        assert (
            client.post(
                f"/admin/queues/{item}/retry/",
                {"service": "ENGAGEMENT_SVC_URL", "queue": "events"},
            ).status_code
            == 302
        )
    assert call.call_args.args[2] == (
        f"/internal/operations/queues/events/{item}/retry"
    )
    assert call.call_args.args[3]["changed_by"] == account.actor_id
