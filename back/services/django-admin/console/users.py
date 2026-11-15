import uuid
from urllib.parse import urlencode

from django.http import HttpResponse, HttpResponseBadRequest
from django.shortcuts import redirect, render
from django.urls import reverse
from django.views.decorators.http import require_GET, require_POST

from .access import admin_context, permission_required
from .client import ServiceError, call
from .management_forms import USER_ROLES, UserRoleForm


@require_GET
@permission_required("accounts")
def users(request):
    query = request.GET.get("q", "").strip()
    try:
        page = int(request.GET.get("page", "1"))
        if not 1 <= page <= 100000 or len(query) > 255:
            raise ValueError
    except ValueError:
        return HttpResponseBadRequest("Некорректный поиск или номер страницы")
    errors, rows, more = [], [], False
    try:
        result = call(
            "IDENTITY_SVC_URL",
            "GET",
            "/internal/users?" + urlencode({"q": query, "page": page}),
        )
        more = result.get("has_more", False)
        labels = dict(USER_ROLES)
        for user in result.get("data", []):
            rows.append(
                {
                    "user": user,
                    "role_label": labels.get(user["role"], user["role"]),
                    "role_form": UserRoleForm(
                        auto_id=False,
                        initial={"user_id": user["id"], "role": user["role"]},
                    ),
                    "invite_query": urlencode({"username": user["email"]}),
                }
            )
    except ServiceError:
        errors.append("Не удалось загрузить пользователей. Повторите запрос")
    return render(
        request,
        "users.html",
        {
            **admin_context(request, "users"),
            "users": rows,
            "errors": errors,
            "query": query,
            "page": page,
            "notice": request.GET.get("notice") == "updated",
            "previous_query": urlencode({"q": query, "page": page - 1})
            if page > 1
            else "",
            "next_query": urlencode({"q": query, "page": page + 1})
            if more
            else "",
        },
        status=503 if errors else 200,
    )


@require_POST
@permission_required("accounts")
def change_user(request):
    action = request.POST.get("action")
    actor = request.admin_account.actor_id
    if action == "role":
        form = UserRoleForm(request.POST)
        if not form.is_valid():
            return HttpResponseBadRequest(
                "Выберите допустимую роль пользователя"
            )
        user_id = form.cleaned_data["user_id"]
        method, path, payload = (
            "PATCH",
            f"/internal/users/{user_id}/role",
            {"role": form.cleaned_data["role"], "changed_by": actor},
        )
    elif action in ("disable", "enable"):
        try:
            user_id = uuid.UUID(request.POST.get("user_id", ""))
        except ValueError:
            return HttpResponseBadRequest("Некорректный пользователь")
        method, path, payload = (
            "PUT",
            f"/internal/users/{user_id}/active",
            {"active": action == "enable", "changed_by": actor},
        )
    else:
        return HttpResponseBadRequest("Неизвестное действие")
    try:
        call("IDENTITY_SVC_URL", method, path, payload)
    except ServiceError:
        return HttpResponse(
            "Не удалось сохранить изменения. "
            "Вернитесь к пользователям и повторите запрос",
            status=502,
        )
    query = request.POST.get("q", "").strip()[:255]
    try:
        page = max(1, min(int(request.POST.get("page", "1")), 100000))
    except (TypeError, ValueError):
        page = 1
    return redirect(
        reverse("users")
        + "?"
        + urlencode({"q": query, "page": page, "notice": "updated"})
    )
