import hashlib
import os
import secrets
import time
import uuid
from pathlib import Path

import pyotp
from django.core.cache import cache
from django.http import HttpResponse, HttpResponseBadRequest
from django.shortcuts import redirect, render
from django.urls import reverse
from django.views.decorators.cache import never_cache
from django.views.decorators.http import (
    require_GET,
    require_POST,
    require_http_methods,
)
from django.views.decorators.debug import sensitive_post_parameters

from .access import admin_context, permission_required
from .auth_audit import record
from .client import ServiceError
from .credentials import (
    AdminPasswordHasher,
    Credentials,
    change_account,
    get_credentials,
    load_accounts,
    save_credentials,
)
from .management_forms import ActivationForm, InvitationForm, TeamAccessForm

INVITATION_TTL = 20 * 60


def _directory():
    return Path(os.environ["ADMIN_AUTH_FILE"]).parent


def _accounts():
    return load_accounts(
        os.environ["ADMIN_AUTH_FILE"], os.environ["ADMIN_AUTH_KEY_FILE"]
    )


def _digest(value):
    return hashlib.sha256(value.encode()).hexdigest()


@require_GET
@permission_required("manage_admins")
def team(request):
    rows = [
        {
            "account": account,
            "form": TeamAccessForm(
                auto_id=False,
                initial={"roles": account.roles, "version": account.version},
            ),
        }
        for account in _accounts()
    ]
    return render(
        request,
        "team.html",
        {
            **admin_context(request, "team"),
            "accounts": rows,
            "invitation_form": InvitationForm(
                initial={"username": request.GET.get("username", "")[:80]}
            ),
            "created_invite": request.session.pop(
                "admin_created_invite", None
            ),
            "notice": request.GET.get("notice") == "updated",
            "error": request.session.pop("admin_team_error", ""),
        },
    )


@require_POST
@permission_required("manage_admins")
def invite(request):
    form = InvitationForm(request.POST)
    if form.is_valid():
        username, role = (
            form.cleaned_data["username"],
            form.cleaned_data["role"],
        )
        if any(a.username == username for a in _accounts()):
            form.add_error("username", "Этот логин уже занят")
    if not form.is_valid():
        request.session["admin_team_error"] = " ".join(
            str(e) for errors in form.errors.values() for e in errors
        )
        return redirect("team")
    creator = request.admin_account
    token = secrets.token_urlsafe(32)
    digest = _digest(token)
    invitation = {
        "username": username,
        "roles": [role],
        "actor_id": str(uuid.uuid4()),
        "created_by": creator.actor_id,
        "creator_username": creator.username,
        "creator_version": creator.version,
        "expires_at": int(time.time()) + INVITATION_TTL,
    }
    try:
        record("admin.invitation_created", username, creator.actor_id)
    except ServiceError:
        return HttpResponse(
            "Журнал недоступен. Приглашение не создано", status=503
        )
    cache.set("admin-invite:" + digest, invitation, INVITATION_TTL)
    cache.set("admin-invite-user:" + _digest(username), digest, INVITATION_TTL)
    request.session["admin_created_invite"] = {
        "url": request.build_absolute_uri(reverse("activate_admin"))
        + "?token="
        + token,
        "username": username,
        "minutes": INVITATION_TTL // 60,
    }
    return redirect("team")


@require_POST
@permission_required("manage_admins")
def change_access(request, actor_id):
    account = next(
        (a for a in _accounts() if a.actor_id == str(actor_id)), None
    )
    if account is None:
        return HttpResponse("Учётная запись не найдена", status=404)
    current = request.admin_account
    if account.actor_id == current.actor_id:
        return HttpResponseBadRequest(
            "Собственные права и статус меняет другой администратор"
        )
    action = request.POST.get("action")
    roles, active = None, None
    if action == "roles":
        form = TeamAccessForm(request.POST)
        if not form.is_valid():
            return HttpResponseBadRequest(
                "Выберите хотя бы одну допустимую роль"
            )
        roles, version = (
            form.cleaned_data["roles"],
            str(form.cleaned_data["version"]),
        )
    elif action in ("disable", "enable"):
        active = action == "enable"
        version = request.POST.get("version", "")
    else:
        return HttpResponseBadRequest("Неизвестное действие")
    try:
        record(
            "admin.access_change_requested", account.username, current.actor_id
        )
        change_account(
            _directory(),
            account.username,
            roles=roles,
            active=active,
            version=version,
        )
    except ServiceError:
        return HttpResponse("Журнал недоступен. Доступ не изменён", status=503)
    except ValueError as error:
        request.session["admin_team_error"] = str(error)
        return redirect("team")
    return redirect(reverse("team") + "?notice=updated")


def _invitation(token):
    if not 32 <= len(token) <= 128:
        return None
    digest = _digest(token)
    invitation = cache.get("admin-invite:" + digest)
    if (
        not invitation
        or invitation["expires_at"] <= time.time()
        or cache.get("admin-invite-user:" + _digest(invitation["username"]))
        != digest
    ):
        return None
    creator = get_credentials(invitation["creator_username"])
    if (
        not creator
        or creator.version != invitation["creator_version"]
        or not creator.permits("manage_admins")
    ):
        return None
    return invitation


@require_http_methods(["GET", "POST"])
@never_cache
@sensitive_post_parameters("password", "password_confirm", "otp")
def activate(request):
    token = request.GET.get("token", "")
    invitation = _invitation(token)
    if invitation is None:
        response = render(
            request, "activate_admin.html", {"expired": True}, status=410
        )
        response["Referrer-Policy"] = "same-origin"
        return response
    digest = _digest(token)
    setup_key = "admin_setup_" + digest
    seed = request.session.get(setup_key)
    if not seed:
        seed = pyotp.random_base32()
        request.session[setup_key] = seed
    form = ActivationForm(request.POST if request.method == "POST" else None)
    if request.method == "POST":
        attempts_key = "admin-invite-attempts:" + digest
        if cache.add(attempts_key, 1, 300):
            attempts = 1
        else:
            attempts = cache.incr(attempts_key)
        if attempts > 5:
            return HttpResponse(
                "Слишком много попыток. Повторите через 5 минут",
                status=429,
                headers={"Retry-After": "300", "Cache-Control": "no-store"},
            )
        if form.is_valid():
            if not pyotp.TOTP(seed).verify(
                form.cleaned_data["otp"], valid_window=1
            ):
                form.add_error(
                    "otp", "Неверный код. Проверьте время и ключ в приложении"
                )
            else:
                claim_key = "admin-invite-claim:" + digest
                if not cache.add(claim_key, True, 60):
                    return HttpResponse(
                        "Активация уже выполняется", status=409
                    )
                try:
                    if _invitation(token) is None:
                        return HttpResponse(
                            "Приглашение уже использовано или отозвано",
                            status=410,
                        )
                    record(
                        "admin.activation_requested",
                        invitation["username"],
                        invitation["created_by"],
                    )
                    hasher = AdminPasswordHasher()
                    account = Credentials(
                        invitation["username"],
                        hasher.encode(
                            form.cleaned_data["password"], hasher.salt()
                        ),
                        seed,
                        invitation["actor_id"],
                        str(uuid.uuid4()),
                        tuple(invitation["roles"]),
                    )
                    save_credentials(_directory(), account, create_only=True)
                    cache.delete("admin-invite:" + digest)
                    cache.delete(
                        "admin-invite-user:" + _digest(account.username)
                    )
                    request.session.pop(setup_key, None)
                    return redirect(reverse("login") + "?notice=activated")
                except ServiceError:
                    form.add_error(
                        None, "Журнал недоступен. Учётная запись не создана"
                    )
                except ValueError as error:
                    form.add_error(None, str(error))
                finally:
                    cache.delete(claim_key)
    response = render(
        request,
        "activate_admin.html",
        {
            "form": form,
            "invitation": invitation,
            "setup_secret": seed,
            "otp_uri": pyotp.TOTP(seed).provisioning_uri(
                invitation["username"], issuer_name="ConfHub"
            ),
        },
    )
    response["Referrer-Policy"] = "same-origin"
    return response
