import os
import uuid
from datetime import datetime
from urllib.parse import urlencode

import psycopg
from cryptography.fernet import InvalidToken
from django.core.cache import cache
from django.http import (
    HttpResponse,
    HttpResponseRedirect,
)
from django.shortcuts import redirect, render
from django.urls import reverse
from django.views.decorators.http import (
    require_GET,
    require_http_methods,
    require_POST,
)
from django.views.decorators.cache import never_cache

from .client import ServiceError, call
from .authentication import (
    authenticated,
    reserve_login_attempt,
    verify_credentials,
)
from .credentials import get_credentials, load_accounts
from .auth_audit import record
from .access import admin_required as _admin_required
from .access import permission_required as _permission_required
from .access import admin_context
from .navigation import SECTIONS


@require_GET
def health(_request):
    return HttpResponse(status=204)


@require_http_methods(["GET", "POST"])
@never_cache
def login_view(request):
    if authenticated(request):
        return redirect("dashboard")
    error = ""
    if request.method == "POST":
        username = request.POST.get("username", "")[:80]
        rate_key, allowed = reserve_login_attempt(
            username, request.META.get("REMOTE_ADDR", "")
        )
        if not allowed:
            try:
                record("admin.login_limited", username)
            except ServiceError:
                return HttpResponse("Журнал входов недоступен", status=503)
            error = "Слишком много попыток. Повторите через 5 минут."
        else:
            account = get_credentials(username)
            verified = verify_credentials(
                request.POST.get("username", ""),
                request.POST.get("password", ""),
                request.POST.get("otp", ""),
            )
            current = get_credentials(username)
            if (
                verified
                and account is not None
                and current is not None
                and account.version == current.version
            ):
                try:
                    record("admin.login", username, account.actor_id)
                except ServiceError:
                    return HttpResponse("Журнал входов недоступен", status=503)
                cache.delete(rate_key)
                request.session.flush()
                request.session["admin_2fa"] = True
                request.session["admin_auth_version"] = account.version
                request.session["admin_username"] = username
                request.session.cycle_key()
                return redirect("dashboard")
            try:
                record("admin.login_failed", username)
            except ServiceError:
                return HttpResponse("Журнал входов недоступен", status=503)
            error = "Неверный логин, пароль или одноразовый код."
    return render(
        request,
        "login.html",
        {
            "error": error,
            "activated": request.GET.get("notice") == "activated",
        },
    )


@require_POST
@_admin_required
def logout_view(request):
    account = request.admin_account
    request.session.flush()
    try:
        record("admin.logout", account.username, account.actor_id)
    except ServiceError:
        return HttpResponse(
            "Сессия завершена; журнал временно недоступен", status=503
        )
    return redirect("login")


PAGE_SIZE = 50
APPLICATION_STATUSES = {
    "submitted": "Подана",
    "under_review": "На проверке",
    "revision_requested": "На доработке",
}
SUPPORT_STATUSES = {
    "new": "Новое",
    "in_progress": "В работе",
    "closed": "Закрыто",
}
SUPPORT_TRANSITIONS = {
    "in_progress": "Взять в работу",
    "closed": "Закрыть",
    "new": "Вернуть в новые",
}
REVIEW_RECOMMENDATIONS = {
    "accept": "Принять",
    "minor_revision": "Небольшая доработка",
    "major_revision": "Существенная доработка",
    "reject": "Отклонить",
}
# Shown to authors for reviews they may read; staff logins stay internal.
REVIEWER_DISPLAY_NAME = "Программный комитет"
REVIEW_TEXT_LIMIT = 5000
REVIEW_NOTICES = {"review-created": "Рецензия сохранена"}
REVIEW_ERRORS = {
    "operation-failed": (
        "Не удалось сохранить рецензию. Повторите действие позже."
    ),
    "already-reviewed": "Вы уже оставили рецензию на эту заявку.",
    "not-found": "Заявка не найдена.",
}
QUEUE_SERVICES = {
    "IDENTITY_SVC_URL": "Учётные записи",
    "CONFERENCE_SVC_URL": "Мероприятия",
    "APPLICATION_SVC_URL": "Заявки и документы",
    "ENGAGEMENT_SVC_URL": "Уведомления",
}
QUEUE_LABELS = {
    "outbox": "Исходящие события",
    "notifications": "Доставка уведомлений",
    "storage_deletions": "Очистка хранилища",
    "events": "Обработка событий",
}
NOTICES = {
    "verifications": {
        "verification-approved": "Заявка на полномочия одобрена",
        "verification-rejected": "Заявка на полномочия отклонена",
    },
    "applications": {
        "application-under-review": "Заявка взята на проверку",
        "application-accepted": "Заявка принята",
        "application-revision-requested": "Заявка отправлена на доработку",
        "application-rejected": "Заявка отклонена",
    },
    "accounts": {
        "account-blocked": "Учетная запись заблокирована",
        "account-unblocked": "Учетная запись разблокирована",
    },
    "support": {
        "support-new": "Обращение возвращено в новые",
        "support-in-progress": "Обращение взято в работу",
        "support-closed": "Обращение закрыто",
    },
    "queues": {"queue-retried": "Задание поставлено на повтор"},
    "audit": {},
}
ACTION_ERRORS = {
    "verifications": (
        "Не удалось изменить заявку на полномочия. Повторите действие позже."
    ),
    "applications": (
        "Не удалось изменить заявку на доклад. Повторите действие позже."
    ),
    "accounts": (
        "Не удалось изменить доступ пользователя. Повторите действие позже."
    ),
    "support": (
        "Не удалось изменить статус обращения. Повторите действие позже."
    ),
    "queues": (
        "Не удалось поставить задание на повтор. Повторите действие позже."
    ),
}


def _page(value):
    try:
        return max(1, min(int(value), 100000))
    except (TypeError, ValueError):
        return 1


def _application_statuses(values):
    selected = {
        part.strip()
        for value in values
        for part in value.split(",")
        if part.strip()
    }
    if not selected or not selected.issubset(APPLICATION_STATUSES):
        return list(APPLICATION_STATUSES)
    return [status for status in APPLICATION_STATUSES if status in selected]


def _support_statuses(values):
    selected = next((value for value in values if value), "")
    return [selected] if selected in SUPPORT_STATUSES else []


def _section_statuses(section, values):
    if section == "applications":
        return _application_statuses(values.getlist("status"))
    if section == "support":
        return _support_statuses(values.getlist("status"))
    return ()


def _section_url(section, page, statuses=(), **extra):
    if section not in NOTICES:
        raise ValueError("Unknown console section")
    parameters = {"page": _page(page)}
    if section == "applications":
        parameters["status"] = ",".join(statuses or APPLICATION_STATUSES)
    elif section == "support" and statuses:
        parameters["status"] = ",".join(statuses)
    parameters.update(extra)
    return reverse(section) + "?" + urlencode(parameters)


def _page_context(
    request, section, page, *, more=False, statuses=(), errors=()
):
    messages = list(errors)
    if (
        request.GET.get("error") == "operation-failed"
        and section in ACTION_ERRORS
    ):
        messages.insert(0, ACTION_ERRORS[section])
    return {
        **admin_context(request, section),
        "section_title": dict((name, label) for name, label, _ in SECTIONS)[
            section
        ],
        "page": page,
        "notice": NOTICES[section].get(request.GET.get("notice"), ""),
        "errors": messages,
        "application_statuses": ",".join(statuses),
        "previous_url": _section_url(section, page - 1, statuses)
        if page > 1
        else "",
        "next_url": _section_url(section, page + 1, statuses) if more else "",
        "retry_url": _section_url(section, page, statuses),
    }


def _return_to_section(request, section, *, notice=None, failed=False):
    statuses = _section_statuses(section, request.POST)
    extra = {"error": "operation-failed"} if failed else {"notice": notice}
    return redirect(
        _section_url(
            section, _page(request.POST.get("page")), statuses, **extra
        )
    )


def _action_error(request, section, message, *, status=400):
    values = request.POST if request.method == "POST" else request.GET
    statuses = _section_statuses(section, values)
    return render(
        request,
        "console/action_error.html",
        {
            **admin_context(request, section),
            "message": message,
            "return_url": _section_url(
                section, _page(values.get("page")), statuses
            ),
        },
        status=status,
    )


@require_GET
@_admin_required
def dashboard(request):
    sections = admin_context(request, "")["console_sections"]
    if not sections:
        return render(
            request,
            "console/forbidden.html",
            admin_context(request, ""),
            status=403,
        )
    return redirect(sections[0]["name"])


@require_GET
@_permission_required("read")
def verifications(request):
    page = _page(request.GET.get("page"))
    items, more, errors = [], False, []
    try:
        result = call(
            "APPLICATION_SVC_URL",
            "GET",
            f"/internal/verifications?status=pending&page={page}"
            f"&per_page={PAGE_SIZE}",
        )
        items, more = result.get("data", []), result.get("has_more", False)
    except (ServiceError, KeyError):
        errors.append(
            "Не удалось загрузить заявки на полномочия организаторов."
        )
    return render(
        request,
        "console/verifications.html",
        {
            **_page_context(
                request, "verifications", page, more=more, errors=errors
            ),
            "verifications": items,
        },
    )


@require_GET
@_permission_required("read")
def applications(request):
    page = _page(request.GET.get("page"))
    statuses = _application_statuses(request.GET.getlist("status"))
    items, more, errors = [], False, []
    try:
        result = call(
            "APPLICATION_SVC_URL",
            "GET",
            f"/internal/applications?status={','.join(statuses)}"
            f"&page={page}&per_page={PAGE_SIZE}",
        )
        items = [
            dict(
                item,
                status_label=APPLICATION_STATUSES.get(
                    item.get("status"), item.get("status", "")
                ),
            )
            for item in result.get("items", [])
        ]
        more = result.get("has_more", False)
    except (ServiceError, KeyError):
        errors.append("Не удалось загрузить заявки на доклады.")
    return render(
        request,
        "console/applications.html",
        {
            **_page_context(
                request,
                "applications",
                page,
                more=more,
                statuses=statuses,
                errors=errors,
            ),
            "applications": items,
            "status_options": [
                {"value": value, "label": label, "selected": value in statuses}
                for value, label in APPLICATION_STATUSES.items()
            ],
        },
    )


def _timestamp(value):
    try:
        return datetime.fromisoformat(str(value))
    except ValueError:
        return None


@require_GET
@_permission_required("read")
def support(request):
    page = _page(request.GET.get("page"))
    statuses = _support_statuses(request.GET.getlist("status"))
    items, more, errors = [], False, []
    query = {"page": page, "per_page": PAGE_SIZE}
    if statuses:
        query["status"] = statuses[0]
    try:
        result = call(
            "ENGAGEMENT_SVC_URL",
            "GET",
            "/internal/support/messages?" + urlencode(query),
        )
        items = [
            dict(
                item,
                status_label=SUPPORT_STATUSES.get(
                    item.get("status"), item.get("status", "")
                ),
                created=_timestamp(item.get("created_at", "")),
                transitions=[
                    {"value": value, "label": label}
                    for value, label in SUPPORT_TRANSITIONS.items()
                    if value != item.get("status")
                ],
            )
            for item in result.get("data", [])
        ]
        more = result.get("has_more", False)
    except (ServiceError, KeyError, AttributeError):
        errors.append("Не удалось загрузить обращения в поддержку.")
    return render(
        request,
        "console/support.html",
        {
            **_page_context(
                request,
                "support",
                page,
                more=more,
                statuses=statuses,
                errors=errors,
            ),
            "support_messages": items,
            "selected_status": statuses[0] if statuses else "",
            "status_options": [
                {"value": value, "label": label}
                for value, label in SUPPORT_STATUSES.items()
            ],
        },
    )


@require_POST
@_permission_required("moderate")
def set_support_status(request, item_id: uuid.UUID, status: str):
    if status not in SUPPORT_STATUSES:
        return _action_error(
            request, "support", "Недопустимый статус обращения."
        )
    try:
        call(
            "ENGAGEMENT_SVC_URL",
            "PATCH",
            f"/internal/support/messages/{item_id}",
            {"status": status, "changed_by": _actor(request)},
        )
        return _return_to_section(
            request,
            "support",
            notice="support-" + status.replace("_", "-"),
        )
    except ServiceError:
        return _return_to_section(request, "support", failed=True)


def _applications_return_url(values):
    return _section_url(
        "applications",
        _page(values.get("page")),
        _application_statuses(values.getlist("status")),
    )


def _reviews_url(item_id, values, **extra):
    parameters = {
        "page": _page(values.get("page")),
        "status": ",".join(_application_statuses(values.getlist("status"))),
        **extra,
    }
    return (
        reverse("application_reviews", args=[item_id])
        + "?"
        + urlencode(parameters)
    )


def _reviewer_names():
    try:
        return {
            account.actor_id: account.username
            for account in load_accounts(
                os.environ["ADMIN_AUTH_FILE"],
                os.environ["ADMIN_AUTH_KEY_FILE"],
            )
        }
    except (OSError, KeyError, ValueError, InvalidToken):
        return {}


@require_GET
@_permission_required("read")
def application_reviews(request, item_id: uuid.UUID):
    application, reviews, errors = None, [], []
    try:
        result = call(
            "APPLICATION_SVC_URL",
            "GET",
            f"/internal/applications/{item_id}/reviews",
        )
        application = result["application"]
        names = _reviewer_names()
        actor = _actor(request)
        reviews = [
            dict(
                review,
                recommendation_label=REVIEW_RECOMMENDATIONS.get(
                    review.get("recommendation"),
                    review.get("recommendation", ""),
                ),
                reviewer_label=names.get(
                    review.get("reviewer_id"), review.get("reviewer_id", "")
                ),
                own=review.get("reviewer_id") == actor,
                created=_timestamp(review.get("created_at", "")),
            )
            for review in result.get("items", [])
        ]
    except ServiceError as exc:
        errors.append(
            REVIEW_ERRORS["not-found"]
            if str(exc).startswith("404:")
            else "Не удалось загрузить рецензии на заявку."
        )
    except (KeyError, TypeError, AttributeError):
        errors.append("Не удалось загрузить рецензии на заявку.")
    error = REVIEW_ERRORS.get(request.GET.get("error"))
    if error:
        errors.insert(0, error)
    statuses = _application_statuses(request.GET.getlist("status"))
    return render(
        request,
        "console/application_reviews.html",
        {
            **admin_context(request, "applications"),
            "section_title": "Рецензии на заявку",
            "page": _page(request.GET.get("page")),
            "application_statuses": ",".join(statuses),
            "notice": REVIEW_NOTICES.get(request.GET.get("notice"), ""),
            "errors": errors,
            "retry_url": _reviews_url(item_id, request.GET),
            "return_url": _applications_return_url(request.GET),
            "application": application,
            "application_status_label": APPLICATION_STATUSES.get(
                (application or {}).get("status"),
                (application or {}).get("status", ""),
            ),
            "reviews": reviews,
            "already_reviewed": any(review["own"] for review in reviews),
            "item_id": item_id,
            "score_options": range(10, 0, -1),
            "recommendation_options": REVIEW_RECOMMENDATIONS.items(),
            "text_limit": REVIEW_TEXT_LIMIT,
        },
    )


def _review_payload(values):
    try:
        score = int(values.get("score", ""))
    except ValueError:
        score = 0
    recommendation = values.get("recommendation", "")
    summary = values.get("summary", "").strip()
    texts = {
        name: values.get(name, "").strip()
        for name in ("strengths", "weaknesses")
    }
    if not 1 <= score <= 10:
        return None, "Выберите оценку от 1 до 10."
    if recommendation not in REVIEW_RECOMMENDATIONS:
        return None, "Выберите рекомендацию по заявке."
    if not summary:
        return None, "Напишите общий вывод рецензии."
    if any(
        len(text) > REVIEW_TEXT_LIMIT for text in (summary, *texts.values())
    ):
        return None, (
            f"Каждое поле рецензии — не более {REVIEW_TEXT_LIMIT} символов."
        )
    return {
        "score": score,
        "summary": summary,
        "strengths": texts["strengths"] or None,
        "weaknesses": texts["weaknesses"] or None,
        "recommendation": recommendation,
        "is_visible_to_author": values.get("visible_to_author") == "true",
    }, ""


@require_POST
@_permission_required("moderate")
def create_review(request, item_id: uuid.UUID):
    payload, message = _review_payload(request.POST)
    if payload is None:
        return render(
            request,
            "console/action_error.html",
            {
                **admin_context(request, "applications"),
                "message": message,
                "return_url": _reviews_url(item_id, request.POST),
            },
            status=400,
        )
    try:
        call(
            "APPLICATION_SVC_URL",
            "POST",
            f"/internal/applications/{item_id}/reviews",
            {
                **payload,
                "reviewer_id": _actor(request),
                "reviewer_name": REVIEWER_DISPLAY_NAME,
            },
        )
    except ServiceError as exc:
        code = str(exc).split(":", 1)[0]
        error = {"409": "already-reviewed", "404": "not-found"}.get(
            code, "operation-failed"
        )
        return redirect(_reviews_url(item_id, request.POST, error=error))
    return redirect(
        _reviews_url(item_id, request.POST, notice="review-created")
    )


@require_GET
@_permission_required("accounts")
def accounts(request):
    return render(
        request,
        "console/accounts.html",
        _page_context(request, "accounts", _page(request.GET.get("page"))),
    )


@require_GET
@_permission_required("operations")
def queues(request):
    page = _page(request.GET.get("page"))
    items, errors = [], []
    for service, label in QUEUE_SERVICES.items():
        try:
            result = call(service, "GET", "/internal/operations/queues")
            items.extend(
                dict(
                    item,
                    service=service,
                    service_label=label,
                    queue_label=QUEUE_LABELS.get(
                        item.get("queue"), item.get("queue", "")
                    ),
                )
                for item in result.get("data", [])
            )
        except (ServiceError, KeyError):
            errors.append(f"Не удалось загрузить фоновые задачи: {label}.")
    items.sort(
        key=lambda item: (
            str(item.get("created_at", "")),
            str(item.get("id", "")),
        ),
        reverse=True,
    )
    offset = (page - 1) * PAGE_SIZE
    end = offset + PAGE_SIZE
    return render(
        request,
        "console/queues.html",
        {
            **_page_context(
                request,
                "queues",
                page,
                more=len(items) > offset + PAGE_SIZE,
                errors=errors,
            ),
            "queue_items": items[offset:end],
        },
    )


def _recent_audit(page=1):
    with psycopg.connect(
        os.environ["DATABASE_URL"], connect_timeout=5
    ) as connection:
        with connection.cursor() as cursor:
            cursor.execute(
                """
                SELECT event_type, actor_id, entity_type,
                       entity_id, occurred_at
                FROM engagement_svc.audit_log
                ORDER BY occurred_at DESC, id DESC LIMIT %s OFFSET %s
            """,
                (PAGE_SIZE + 1, (page - 1) * PAGE_SIZE),
            )
            names = [item.name for item in cursor.description]
            rows = [dict(zip(names, row)) for row in cursor.fetchall()]
    return rows[:PAGE_SIZE], len(rows) > PAGE_SIZE


@require_GET
@_permission_required("audit")
def audit(request):
    page = _page(request.GET.get("page"))
    items, more, errors = [], False, []
    try:
        items, more = _recent_audit(page)
    except (psycopg.Error, KeyError):
        errors.append("Журнал действий временно недоступен.")
    return render(
        request,
        "console/audit.html",
        {
            **_page_context(request, "audit", page, more=more, errors=errors),
            "audit_items": items,
        },
    )


def _actor(request):
    return request.admin_account.actor_id


@require_POST
@_permission_required("moderate")
def moderate_verification(request, item_id: uuid.UUID, status: str):
    if status not in ("approved", "rejected"):
        return _action_error(
            request, "verifications", "Недопустимое решение по заявке."
        )
    reason = request.POST.get("reason", "").strip()
    if status == "rejected" and not reason:
        return _action_error(
            request, "verifications", "Укажите причину отказа в полномочиях."
        )
    try:
        call(
            "APPLICATION_SVC_URL",
            "PATCH",
            f"/internal/verifications/{item_id}",
            {
                "status": status,
                "reason": reason or None,
                "changed_by": _actor(request),
            },
        )
        return _return_to_section(
            request, "verifications", notice=f"verification-{status}"
        )
    except ServiceError:
        return _return_to_section(request, "verifications", failed=True)


@require_GET
@_permission_required("moderate")
def verification_document(request, item_id: uuid.UUID):
    try:
        result = call(
            "APPLICATION_SVC_URL",
            "GET",
            f"/internal/verification-documents/{item_id}",
        )
        return HttpResponseRedirect(result["url"])
    except ServiceError:
        return _action_error(
            request,
            "verifications",
            "Не удалось открыть документ. Повторите запрос позже.",
            status=502,
        )


@require_POST
@_permission_required("moderate")
def moderate_application(request, item_id: uuid.UUID, status: str):
    if status not in (
        "under_review",
        "revision_requested",
        "accepted",
        "rejected",
    ):
        return _action_error(
            request, "applications", "Недопустимое решение по заявке."
        )
    comment = request.POST.get("comment", "").strip()
    if status == "revision_requested" and not comment:
        return _action_error(
            request, "applications", "Укажите, что автору нужно исправить."
        )
    try:
        call(
            "APPLICATION_SVC_URL",
            "PATCH",
            f"/internal/applications/{item_id}/moderate",
            {
                "status": status,
                "comment": comment or None,
                "changed_by": _actor(request),
            },
        )
        return _return_to_section(
            request,
            "applications",
            notice="application-" + status.replace("_", "-"),
        )
    except ServiceError:
        return _return_to_section(request, "applications", failed=True)


@require_POST
@_permission_required("accounts")
def set_account_active(request):
    try:
        user_id = uuid.UUID(request.POST.get("user_id", ""))
    except ValueError:
        return _action_error(
            request, "accounts", "Введите корректный UUID пользователя."
        )
    if request.POST.get("active") not in ("true", "false"):
        return _action_error(
            request,
            "accounts",
            "Выберите блокировку или разблокировку учётной записи.",
        )
    active = request.POST.get("active") == "true"
    try:
        call(
            "IDENTITY_SVC_URL",
            "PUT",
            f"/internal/users/{user_id}/active",
            {
                "active": active,
                "changed_by": _actor(request),
            },
        )
        return _return_to_section(
            request,
            "accounts",
            notice="account-unblocked" if active else "account-blocked",
        )
    except ServiceError:
        return _return_to_section(request, "accounts", failed=True)


@require_POST
@_permission_required("operations")
def retry_queue(request, item_id: uuid.UUID):
    service = request.POST.get("service", "")
    queue = request.POST.get("queue", "")
    if service not in (
        "IDENTITY_SVC_URL",
        "CONFERENCE_SVC_URL",
        "APPLICATION_SVC_URL",
        "ENGAGEMENT_SVC_URL",
    ) or queue not in (
        "outbox",
        "notifications",
        "storage_deletions",
        "events",
    ):
        return _action_error(
            request, "queues", "Недопустимый сервис или очередь."
        )
    try:
        call(
            service,
            "POST",
            f"/internal/operations/queues/{queue}/{item_id}/retry",
            {"changed_by": _actor(request)},
        )
        return _return_to_section(request, "queues", notice="queue-retried")
    except ServiceError:
        return _return_to_section(request, "queues", failed=True)
