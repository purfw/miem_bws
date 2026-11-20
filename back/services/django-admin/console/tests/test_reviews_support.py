import uuid
from unittest.mock import patch
from urllib.parse import parse_qs, urlsplit

import pytest
from django.test import Client

from console.client import ServiceError
from .test_dashboard import signed_in

APPLICATION = {
    "id": "6f1d1f43-35a9-4c1b-9d0e-2c4c9a3d1b7e",
    "title": "Доклад о графовых базах",
    "conference_title": "ConfHub 2026",
    "user_name": "Автор",
    "user_email": "author@example.test",
    "status": "under_review",
}


def review(reviewer_id, **extra):
    return {
        "id": str(uuid.uuid4()),
        "application_id": APPLICATION["id"],
        "reviewer_id": reviewer_id,
        "reviewer_name": "Программный комитет",
        "score": 8,
        "summary": "Сильная постановка задачи",
        "strengths": "Новизна",
        "recommendation": "minor_revision",
        "is_visible_to_author": False,
        "created_at": "2026-11-20T10:15:00Z",
        **extra,
    }


def support_message(status="new", **extra):
    return {
        "id": str(uuid.uuid4()),
        "name": "Пользователь",
        "email": "user@example.test",
        "topic": "bug",
        "topic_label": "Ошибка на сайте",
        "subject": "Не открывается страница",
        "message": "Страница конференции\nвозвращает ошибку",
        "status": status,
        "created_at": "2026-11-20T10:15:00Z",
        **extra,
    }


# Reviews


def test_reviews_page_lists_all_reviews_and_offers_form(credentials):
    client = signed_in(credentials)
    other = str(uuid.uuid4())
    result = {
        "application": APPLICATION,
        "items": [review(other, is_visible_to_author=True)],
    }
    with patch("console.views.call", return_value=result) as call:
        response = client.get(
            f"/admin/applications/{APPLICATION['id']}/reviews/"
            "?page=2&status=under_review"
        )
    assert response.status_code == 200
    assert call.call_args.args == (
        "APPLICATION_SVC_URL",
        "GET",
        f"/internal/applications/{APPLICATION['id']}/reviews",
    )
    html = response.content.decode()
    assert "Доклад о графовых базах" in html and "8/10" in html
    assert "Небольшая доработка" in html and "Видна" in html
    assert other in html
    assert (
        f'action="/admin/applications/{APPLICATION["id"]}/reviews/new/"'
        in (html)
    )
    assert "/admin/applications/?page=2&amp;status=under_review" in html
    assert 'name="status" value="under_review"' in html


def test_reviews_page_hides_form_after_own_review(credentials):
    client = signed_in(credentials)
    result = {
        "application": APPLICATION,
        "items": [review(credentials.actor_id)],
    }
    with patch("console.views.call", return_value=result):
        html = client.get(
            f"/admin/applications/{APPLICATION['id']}/reviews/"
        ).content.decode()
    assert "Вы уже оставили рецензию" in html
    assert "/reviews/new/" not in html
    assert "Скрыта" in html and "<dd>Вы</dd>" in html


@pytest.mark.parametrize("role", ["auditor", "operator"])
def test_read_only_roles_see_reviews_without_form(credentials, role):
    client = signed_in(credentials, role)
    result = {"application": APPLICATION, "items": [review(str(uuid.uuid4()))]}
    with patch("console.views.call", return_value=result):
        response = client.get(
            f"/admin/applications/{APPLICATION['id']}/reviews/"
        )
    assert response.status_code == 200
    assert "Сильная постановка задачи" in response.content.decode()
    assert "/reviews/new/" not in response.content.decode()
    with patch("console.views.call") as call:
        assert (
            client.post(
                f"/admin/applications/{APPLICATION['id']}/reviews/new/",
                {"score": "5", "recommendation": "accept", "summary": "Ок"},
            ).status_code
            == 403
        )
        call.assert_not_called()


def test_review_fields_are_escaped(credentials):
    client = signed_in(credentials)
    payload = "<script>alert('unsafe')</script>"
    result = {
        "application": dict(APPLICATION, title=payload),
        "items": [review(str(uuid.uuid4()), summary=payload)],
    }
    with patch("console.views.call", return_value=result):
        html = client.get(
            f"/admin/applications/{APPLICATION['id']}/reviews/"
        ).content.decode()
    assert payload not in html and "&lt;script&gt;" in html


def test_missing_application_and_outage_are_reported(credentials):
    client = signed_in(credentials)
    path = f"/admin/applications/{uuid.uuid4()}/reviews/"
    with patch(
        "console.views.call", side_effect=ServiceError("404: not found")
    ):
        assert "Заявка не найдена" in client.get(path).content.decode()
    with patch("console.views.call", side_effect=ServiceError("unavailable")):
        response = client.get(path)
    assert response.status_code == 200
    assert "Не удалось загрузить рецензии" in response.content.decode()


def test_create_review_sends_actor_and_returns_to_review_page(credentials):
    client = signed_in(credentials)
    item = uuid.uuid4()
    with patch("console.views.call", return_value={}) as call:
        response = client.post(
            f"/admin/applications/{item}/reviews/new/",
            {
                "page": "3",
                "status": "under_review",
                "score": "7",
                "recommendation": "major_revision",
                "summary": "  Нужны данные экспериментов  ",
                "strengths": "",
                "weaknesses": "Мало источников",
                "visible_to_author": "true",
                "reviewer_id": str(uuid.uuid4()),
                "reviewer_name": "spoofed",
            },
        )
    assert call.call_args.args == (
        "APPLICATION_SVC_URL",
        "POST",
        f"/internal/applications/{item}/reviews",
        {
            "score": 7,
            "summary": "Нужны данные экспериментов",
            "strengths": None,
            "weaknesses": "Мало источников",
            "recommendation": "major_revision",
            "is_visible_to_author": True,
            "reviewer_id": credentials.actor_id,
            "reviewer_name": "Программный комитет",
        },
    )
    assert response.status_code == 302
    location = urlsplit(response.url)
    assert not location.netloc
    assert location.path == f"/admin/applications/{item}/reviews/"
    assert parse_qs(location.query) == {
        "page": ["3"],
        "status": ["under_review"],
        "notice": ["review-created"],
    }
    with patch(
        "console.views.call",
        return_value={"application": APPLICATION, "items": []},
    ):
        assert (
            "Рецензия сохранена" in client.get(response.url).content.decode()
        )


@pytest.mark.parametrize(
    "data",
    [
        {"score": "11", "recommendation": "accept", "summary": "Хорошо"},
        {"score": "abc", "recommendation": "accept", "summary": "Хорошо"},
        {"score": "5", "recommendation": "maybe", "summary": "Хорошо"},
        {"score": "5", "recommendation": "accept", "summary": "   "},
        {
            "score": "5",
            "recommendation": "accept",
            "summary": "Хорошо",
            "weaknesses": "x" * 5001,
        },
    ],
)
def test_invalid_review_is_rejected_before_service_call(credentials, data):
    client = signed_in(credentials)
    with patch("console.views.call") as call:
        response = client.post(
            f"/admin/applications/{uuid.uuid4()}/reviews/new/", data
        )
    assert response.status_code == 400
    assert "Действие не выполнено" in response.content.decode()
    call.assert_not_called()


@pytest.mark.parametrize(
    "error,code,message",
    [
        (
            "409: already reviewed",
            "already-reviewed",
            "Вы уже оставили рецензию",
        ),
        ("404: not found", "not-found", "Заявка не найдена"),
        ("unavailable", "operation-failed", "Не удалось сохранить рецензию"),
    ],
)
def test_review_service_errors_are_explained(
    credentials, error, code, message
):
    client = signed_in(credentials)
    item = uuid.uuid4()
    with patch("console.views.call", side_effect=ServiceError(error)):
        response = client.post(
            f"/admin/applications/{item}/reviews/new/",
            {"score": "5", "recommendation": "accept", "summary": "Хорошо"},
        )
    assert response.status_code == 302
    assert parse_qs(urlsplit(response.url).query)["error"] == [code]
    with patch(
        "console.views.call",
        return_value={"application": APPLICATION, "items": []},
    ):
        assert message in client.get(response.url).content.decode()


def test_reviews_route_does_not_reach_moderation(credentials):
    client = signed_in(credentials)
    with patch("console.views.call") as call:
        response = client.post(
            f"/admin/applications/{uuid.uuid4()}/reviews/", {"page": "1"}
        )
    assert response.status_code == 405
    call.assert_not_called()


def test_applications_link_to_reviews(credentials):
    client = signed_in(credentials)
    with patch("console.views.call", return_value={"items": [APPLICATION]}):
        html = client.get(
            "/admin/applications/?status=under_review"
        ).content.decode()
    assert (
        f'href="/admin/applications/{APPLICATION["id"]}/reviews/'
        '?page=1&amp;status=under_review"' in html
    )


# Support inbox


def test_support_inbox_renders_messages_and_filter(credentials):
    client = signed_in(credentials)
    item = support_message("in_progress", user_id=str(uuid.uuid4()))
    with patch(
        "console.views.call", return_value={"data": [item], "has_more": True}
    ) as call:
        response = client.get("/admin/support/?status=in_progress&page=2")
    assert call.call_args.args == (
        "ENGAGEMENT_SVC_URL",
        "GET",
        "/internal/support/messages?page=2&per_page=50&status=in_progress",
    )
    html = response.content.decode()
    assert "Не открывается страница" in html and "Ошибка на сайте" in html
    assert "В работе" in html and "20.11.2026" in html
    assert '<time datetime="2026-11-20T10:15:00Z">' in html
    assert 'value="in_progress" checked' in html
    assert "page=3&amp;status=in_progress" in html
    assert f"/admin/support/{item['id']}/closed/" in html
    assert f"/admin/support/{item['id']}/new/" in html
    assert f"/admin/support/{item['id']}/in_progress/" not in html


def test_support_inbox_ignores_unknown_status_filter(credentials):
    client = signed_in(credentials)
    with patch("console.views.call", return_value={"data": []}) as call:
        response = client.get("/admin/support/?status=resolved")
    assert response.status_code == 200
    assert call.call_args.args[2] == (
        "/internal/support/messages?page=1&per_page=50"
    )
    assert "Обращений нет" in response.content.decode()


def test_support_inbox_escapes_and_reports_outage(credentials):
    client = signed_in(credentials)
    payload = "<script>alert('unsafe')</script>"
    with patch(
        "console.views.call",
        return_value={"data": [support_message(message=payload)]},
    ):
        html = client.get("/admin/support/").content.decode()
    assert payload not in html and "&lt;script&gt;" in html
    with patch("console.views.call", side_effect=ServiceError("unavailable")):
        response = client.get("/admin/support/")
    assert "Не удалось загрузить обращения" in response.content.decode()


@pytest.mark.parametrize(
    "status,notice,message",
    [
        ("in_progress", "support-in-progress", "Обращение взято в работу"),
        ("closed", "support-closed", "Обращение закрыто"),
        ("new", "support-new", "Обращение возвращено в новые"),
    ],
)
def test_support_status_change_preserves_page_and_filter(
    credentials, status, notice, message
):
    client = signed_in(credentials)
    item = uuid.uuid4()
    with patch("console.views.call", return_value={}) as call:
        response = client.post(
            f"/admin/support/{item}/{status}/",
            {"page": "2", "status": "new", "next": "https://evil.example/"},
        )
    assert call.call_args.args == (
        "ENGAGEMENT_SVC_URL",
        "PATCH",
        f"/internal/support/messages/{item}",
        {"status": status, "changed_by": credentials.actor_id},
    )
    location = urlsplit(response.url)
    assert response.status_code == 302 and not location.netloc
    assert location.path == "/admin/support/"
    assert parse_qs(location.query) == {
        "page": ["2"],
        "status": ["new"],
        "notice": [notice],
    }
    with patch("console.views.call", return_value={"data": []}):
        assert message in client.get(response.url).content.decode()


def test_support_status_failures(credentials):
    client = signed_in(credentials)
    with patch("console.views.call") as call:
        response = client.post(
            f"/admin/support/{uuid.uuid4()}/resolved/", {"page": "1"}
        )
        assert response.status_code == 400
        call.assert_not_called()
    with patch("console.views.call", side_effect=ServiceError("unavailable")):
        response = client.post(
            f"/admin/support/{uuid.uuid4()}/closed/", {"page": "1"}
        )
    assert parse_qs(urlsplit(response.url).query)["error"] == [
        "operation-failed"
    ]
    with patch("console.views.call", return_value={"data": []}):
        assert (
            "Не удалось изменить статус обращения"
            in client.get(response.url).content.decode()
        )


@pytest.mark.parametrize("role", ["auditor", "operator"])
def test_read_only_roles_cannot_change_support_status(credentials, role):
    client = signed_in(credentials, role)
    with patch(
        "console.views.call", return_value={"data": [support_message()]}
    ):
        html = client.get("/admin/support/").content.decode()
    assert "Взять в работу" not in html
    with patch("console.views.call") as call:
        assert (
            client.post(f"/admin/support/{uuid.uuid4()}/closed/").status_code
            == 403
        )
        call.assert_not_called()


@pytest.mark.parametrize(
    "path",
    [
        f"/admin/support/{uuid.uuid4()}/closed/",
        f"/admin/applications/{uuid.uuid4()}/reviews/new/",
    ],
)
def test_new_actions_require_post_and_csrf(credentials, path):
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
    with patch("console.views.call") as call:
        assert client.get(path).status_code == 405
        assert client.post(path, {}).status_code == 403
        call.assert_not_called()
