import uuid
from datetime import datetime, timezone
from html.parser import HTMLParser
from types import SimpleNamespace
from unittest.mock import patch
from urllib.parse import parse_qs, urlsplit

import psycopg
import pytest
from django.test import Client

from console import views
from console.client import ServiceError
from .test_authentication import login
from .test_roles import second_account

PAGES = ("verifications", "applications", "accounts", "queues", "audit")
VISIBLE = {
    "superadmin": (*PAGES, "users", "team"),
    "moderator": ("verifications", "applications", "audit"),
    "operator": ("verifications", "applications", "queues"),
    "auditor": ("verifications", "applications", "audit"),
}


class Navigation(HTMLParser):
    def __init__(self, html):
        super().__init__()
        self.inside, self.links = False, []
        self.feed(html)

    def handle_starttag(self, tag, attributes):
        attributes = dict(attributes)
        if tag == "nav" and attributes.get("aria-label") == "Разделы консоли":
            self.inside = True
        if tag == "a" and self.inside:
            self.links.append(attributes)

    def handle_endtag(self, tag):
        if tag == "nav":
            self.inside = False


def signed_in(credentials, role="superadmin"):
    account = (
        credentials
        if role == "superadmin"
        else second_account(credentials, role)
    )
    client = Client()
    assert login(client, account).status_code == 302
    return client


@pytest.mark.parametrize("role", VISIBLE)
def test_dashboard_redirects_without_loading_services(credentials, role):
    client = signed_in(credentials, role)
    with (
        patch("console.views.call") as call,
        patch("console.views._recent_audit") as audit,
    ):
        response = client.get("/admin/?vp=4&ap=9")
    assert (
        response.status_code == 302 and response.url == "/admin/verifications/"
    )
    assert "no-store" in response.headers["Cache-Control"]
    call.assert_not_called()
    audit.assert_not_called()


@pytest.mark.parametrize("role", VISIBLE)
@pytest.mark.parametrize("section", PAGES)
def test_section_permissions_navigation_and_cache_policy(
    credentials, role, section
):
    client = signed_in(credentials, role)
    with (
        patch("console.views.call", return_value={}),
        patch("console.views._recent_audit", return_value=([], False)),
    ):
        response = client.get(f"/admin/{section}/")
    allowed = section in VISIBLE[role]
    assert response.status_code == (200 if allowed else 403)
    html = response.content.decode()
    links = Navigation(html).links
    assert [a["href"] for a in links] == [
        f"/admin/{name}/" for name in VISIBLE[role]
    ]
    assert [a["href"] for a in links if a.get("aria-current") == "page"] == (
        [f"/admin/{section}/"] if allowed else []
    )
    assert "no-store" in response.headers["Cache-Control"]
    if not allowed:
        assert "Недостаточно прав" in html and "ConfHub · консоль" in html


@pytest.mark.parametrize("section", PAGES)
def test_sections_require_login_before_loading_data(credentials, section):
    with (
        patch("console.views.call") as call,
        patch("console.views._recent_audit") as audit,
    ):
        response = Client().get(f"/admin/{section}/")
    assert response.status_code == 302 and response.url == "/admin/login/"
    call.assert_not_called()
    audit.assert_not_called()


@pytest.mark.parametrize("section", PAGES)
def test_sections_only_load_their_own_data(credentials, section):
    client = signed_in(credentials)
    with (
        patch("console.views.call", return_value={}) as call,
        patch(
            "console.views._recent_audit", return_value=([], False)
        ) as audit,
    ):
        response = client.get(f"/admin/{section}/?page=2&vp=8&ap=9")
    assert response.status_code == 200
    if section == "verifications":
        assert call.call_args.args == (
            "APPLICATION_SVC_URL",
            "GET",
            "/internal/verifications?status=pending&page=2&per_page=50",
        )
        assert call.call_count == 1
    elif section == "applications":
        assert call.call_args.args == (
            "APPLICATION_SVC_URL",
            "GET",
            "/internal/applications?status=submitted,under_review,"
            "revision_requested&page=2&per_page=50",
        )
        assert call.call_count == 1
    elif section == "queues":
        assert [c.args for c in call.call_args_list] == [
            (s, "GET", "/internal/operations/queues")
            for s in (
                "IDENTITY_SVC_URL",
                "CONFERENCE_SVC_URL",
                "APPLICATION_SVC_URL",
                "ENGAGEMENT_SVC_URL",
            )
        ]
    else:
        call.assert_not_called()
    if section == "audit":
        audit.assert_called_once_with(2)
    else:
        audit.assert_not_called()
    assert (
        "vp=" not in response.content.decode()
        and "ap=" not in response.content.decode()
    )


@pytest.mark.parametrize(
    "raw,expected",
    [("abc", 1), ("0", 1), ("-3", 1), ("", 1), ("2.5", 1), ("100001", 100000)],
)
@pytest.mark.parametrize("section", ("verifications", "applications", "audit"))
def test_independent_page_numbers_are_normalized(
    credentials, section, raw, expected
):
    client = signed_in(credentials)
    with (
        patch("console.views.call", return_value={}) as call,
        patch(
            "console.views._recent_audit", return_value=([], False)
        ) as audit,
    ):
        response = client.get(f"/admin/{section}/?page={raw}")
    assert (
        response.status_code == 200
        and f"Страница {expected}" in response.content.decode()
    )
    if section == "audit":
        audit.assert_called_once_with(expected)
    else:
        assert f"&page={expected}&" in call.call_args.args[2]


def test_application_filter_and_pagination_preserve_statuses(credentials):
    client = signed_in(credentials)
    with patch(
        "console.views.call", return_value={"items": [], "has_more": True}
    ) as call:
        response = client.get(
            "/admin/applications/?page=3&status=under_review"
            "&status=revision_requested"
        )
    assert (
        call.call_args.args[2]
        == "/internal/applications?status=under_review,revision_requested"
        "&page=3&per_page=50"
    )
    html = response.content.decode()
    assert 'name="status" value="under_review" checked' in html
    assert 'name="status" value="revision_requested" checked' in html
    assert "page=4&amp;status=under_review%2Crevision_requested" in html
    with patch("console.views.call", return_value={}) as call:
        assert (
            client.get("/admin/applications/?status=unknown").status_code
            == 200
        )
    assert (
        "status=submitted,under_review,revision_requested"
        in call.call_args.args[2]
    )


def test_audit_query_is_parameterized_read_only_and_limited(monkeypatch):
    monkeypatch.setenv(
        "DATABASE_URL",
        "postgres://django_reader@database/confhub_test_console",
    )
    with patch("console.views.psycopg.connect") as connect:
        connection = connect.return_value.__enter__.return_value
        cursor = connection.cursor.return_value.__enter__.return_value
        cursor.description = [
            SimpleNamespace(name=n)
            for n in (
                "event_type",
                "actor_id",
                "entity_type",
                "entity_id",
                "occurred_at",
            )
        ]
        cursor.fetchall.return_value = [
            (
                "admin.login",
                None,
                "administrator",
                None,
                datetime(2026, 10, 31, tzinfo=timezone.utc),
            )
        ] * 51
        rows, more = views._recent_audit(3)
    assert len(rows) == 50 and more
    sql, parameters = cursor.execute.call_args.args
    assert sql.lstrip().startswith("SELECT")
    assert "ORDER BY occurred_at DESC, id DESC LIMIT %s OFFSET %s" in sql
    assert parameters == (51, 100)
    assert connect.call_args.args[0].startswith("postgres://django_reader@")


def test_service_error_is_confined_to_its_section(credentials):
    client = signed_in(credentials)
    with patch("console.views.call", side_effect=ServiceError("unavailable")):
        response = client.get("/admin/verifications/")
    assert (
        response.status_code == 200
        and "Не удалось загрузить заявки на полномочия"
        in response.content.decode()
    )
    with patch("console.views.call", return_value={}):
        other = client.get("/admin/applications/")
    assert (
        other.status_code == 200
        and "Не удалось загрузить заявки на полномочия"
        not in other.content.decode()
    )
    with patch(
        "console.views._recent_audit",
        side_effect=psycopg.OperationalError("unavailable"),
    ):
        response = client.get("/admin/audit/")
    assert (
        response.status_code == 200
        and "Журнал действий временно недоступен" in response.content.decode()
    )


def test_queue_partial_failure_keeps_other_services_visible(credentials):
    client = signed_in(credentials)

    def result(service, _method, _path):
        if service == "IDENTITY_SVC_URL":
            raise ServiceError("unavailable")
        return {
            "data": [
                {
                    "id": str(uuid.uuid4()),
                    "queue": "events",
                    "status": "dead",
                    "attempts": 3,
                    "created_at": "2026-10-31T10:00:00Z",
                }
            ]
        }

    with patch("console.views.call", side_effect=result):
        response = client.get("/admin/queues/")
    assert response.status_code == 200
    assert (
        "Не удалось загрузить фоновые задачи: Учётные записи"
        in response.content.decode()
    )
    assert response.content.decode().count('name="service"') == 3


ACTIONS = [
    (
        "verifications",
        "approved",
        {},
        "verification-approved",
        "Заявка на полномочия одобрена",
    ),
    (
        "verifications",
        "rejected",
        {"reason": "Документы не подтверждают полномочия"},
        "verification-rejected",
        "Заявка на полномочия отклонена",
    ),
    (
        "applications",
        "under_review",
        {},
        "application-under-review",
        "Заявка взята на проверку",
    ),
    ("applications", "accepted", {}, "application-accepted", "Заявка принята"),
    (
        "applications",
        "revision_requested",
        {"comment": "Уточните метод исследования"},
        "application-revision-requested",
        "Заявка отправлена на доработку",
    ),
    (
        "applications",
        "rejected",
        {},
        "application-rejected",
        "Заявка отклонена",
    ),
    (
        "accounts",
        "false",
        {},
        "account-blocked",
        "Учетная запись заблокирована",
    ),
    (
        "accounts",
        "true",
        {},
        "account-unblocked",
        "Учетная запись разблокирована",
    ),
    ("queues", "retry", {}, "queue-retried", "Задание поставлено на повтор"),
]


@pytest.mark.parametrize("section,action,extra,notice,message", ACTIONS)
def test_actions_preserve_section_page_filter_and_notice(
    credentials, section, action, extra, notice, message
):
    client = signed_in(credentials)
    item_id = uuid.uuid4()
    data = {
        "page": "3",
        "next": "https://evil.example/",
        "return_to": "audit",
        "section": "audit",
        **extra,
    }
    if section == "accounts":
        path = "/admin/accounts/active/"
        data.update(user_id=str(item_id), active=action)
    elif section == "queues":
        path = f"/admin/queues/{item_id}/retry/"
        data.update(service="APPLICATION_SVC_URL", queue="outbox")
    else:
        path = f"/admin/{section}/{item_id}/{action}/"
    if section == "applications":
        data["status"] = "under_review"
    with patch("console.views.call") as call:
        response = client.post(path, data)
    assert response.status_code == 302
    location = urlsplit(response.url)
    assert not location.netloc and location.path == f"/admin/{section}/"
    query = parse_qs(location.query)
    assert query["page"] == ["3"] and query["notice"] == [notice]
    if section == "applications":
        assert query["status"] == ["under_review"]
    assert call.call_args.args[3]["changed_by"] == credentials.actor_id
    with (
        patch("console.views.call", return_value={}),
        patch("console.views._recent_audit", return_value=([], False)),
    ):
        rendered = client.get(response.url)
    assert message in rendered.content.decode()


def test_unknown_notice_is_not_reflected(credentials):
    client = signed_in(credentials)
    with patch("console.views.call", return_value={}):
        response = client.get("/admin/verifications/?notice=unknown-code")
        wrong = client.get("/admin/verifications/?notice=application-accepted")
    assert "unknown-code" not in response.content.decode()
    assert "Заявка принята" not in wrong.content.decode()


def test_required_reason_and_comment_are_checked_before_service_call(
    credentials,
):
    client = signed_in(credentials)
    with patch("console.views.call") as call:
        for path in (
            f"/admin/verifications/{uuid.uuid4()}/rejected/",
            f"/admin/applications/{uuid.uuid4()}/revision_requested/",
        ):
            response = client.post(path, {"page": "2"})
            assert (
                response.status_code == 400
                and "Действие не выполнено" in response.content.decode()
            )
        call.assert_not_called()


def test_failed_action_returns_to_original_section(credentials):
    client = signed_in(credentials)
    with patch("console.views.call", side_effect=ServiceError("unavailable")):
        response = client.post(
            f"/admin/applications/{uuid.uuid4()}/accepted/",
            {"page": "2", "status": "under_review"},
        )
    assert (
        response.status_code == 302
        and urlsplit(response.url).path == "/admin/applications/"
    )
    assert parse_qs(urlsplit(response.url).query) == {
        "page": ["2"],
        "status": ["under_review"],
        "error": ["operation-failed"],
    }


def test_queue_pagination_is_local_to_queue_section(credentials):
    client = signed_in(credentials)
    items = [
        {
            "id": str(uuid.uuid4()),
            "queue": "outbox",
            "status": "failed",
            "attempts": 2,
            "created_at": f"2026-10-31T10:{i:02d}:00Z",
        }
        for i in range(51)
    ]

    def result(service, _method, _path):
        return {"data": items if service == "IDENTITY_SVC_URL" else []}

    with patch("console.views.call", side_effect=result):
        first = client.get("/admin/queues/?page=1")
        second = client.get("/admin/queues/?page=2")
    assert first.content.decode().count('name="service"') == 50
    assert second.content.decode().count('name="service"') == 1
    assert (
        "?page=2" in first.content.decode()
        and 'value="2"' in second.content.decode()
    )


@pytest.mark.parametrize(
    "section", ("verifications", "applications", "accounts", "queues")
)
def test_actions_require_post_and_csrf(credentials, section):
    client = Client(enforce_csrf_checks=True)
    session = client.session
    session.update(
        {
            "admin_username": credentials.username,
            "admin_2fa": True,
            "admin_auth_version": credentials.version,
        }
    )
    session.save()
    item = uuid.uuid4()
    paths = {
        "verifications": f"/admin/verifications/{item}/approved/",
        "applications": f"/admin/applications/{item}/accepted/",
        "accounts": "/admin/accounts/active/",
        "queues": f"/admin/queues/{item}/retry/",
    }
    with patch("console.views.call") as call:
        assert client.get(paths[section]).status_code == 405
        assert client.post(paths[section], {}).status_code == 403
        call.assert_not_called()


def test_valid_csrf_action_still_works(credentials):
    client = Client(enforce_csrf_checks=True)
    session = client.session
    session.update(
        {
            "admin_username": credentials.username,
            "admin_2fa": True,
            "admin_auth_version": credentials.version,
        }
    )
    session.save()
    with patch("console.views.call", return_value={}) as call:
        assert client.get("/admin/verifications/").status_code == 200
        csrf = client.cookies["csrftoken"].value
        response = client.post(
            f"/admin/verifications/{uuid.uuid4()}/approved/",
            {"page": "4"},
            HTTP_X_CSRFTOKEN=csrf,
            HTTP_ORIGIN="http://testserver",
        )
    assert response.status_code == 302
    assert parse_qs(urlsplit(response.url).query) == {
        "page": ["4"],
        "notice": ["verification-approved"],
    }
    assert call.call_args.args[1] == "PATCH"


@pytest.mark.parametrize("section", ("verifications", "applications"))
def test_service_fields_are_escaped(credentials, section):
    client = signed_in(credentials)
    payload = "<script>alert('unsafe')</script>"
    item = {
        "id": str(uuid.uuid4()),
        "user_id": str(uuid.uuid4()),
        "organization": payload,
        "title": payload,
        "user_name": "Автор",
        "user_email": "author@example.test",
        "status": "submitted",
        "documents": [],
    }
    key = "data" if section == "verifications" else "items"
    with patch("console.views.call", return_value={key: [item]}):
        response = client.get(f"/admin/{section}/")
    assert response.status_code == 200
    assert payload not in response.content.decode()
    assert "&lt;script&gt;" in response.content.decode()


def test_invalid_account_state_cannot_silently_block_user(credentials):
    client = signed_in(credentials)
    with patch("console.views.call") as call:
        response = client.post(
            "/admin/accounts/active/",
            {"user_id": str(uuid.uuid4()), "active": "unknown"},
        )
    assert response.status_code == 400
    call.assert_not_called()
