from functools import wraps

from django.shortcuts import redirect, render
from django.urls import reverse
from django.views.decorators.cache import never_cache

from .authentication import authenticated
from .navigation import ACTION_SECTIONS, ROLE_LABELS, SECTIONS


def admin_required(view):
    @never_cache
    @wraps(view)
    def wrapped(request, *args, **kwargs):
        if not authenticated(request):
            return redirect("login")
        return view(request, *args, **kwargs)

    return wrapped


def permission_required(permission):
    def decorate(view):
        @admin_required
        @wraps(view)
        def wrapped(request, *args, **kwargs):
            account = request.admin_account
            if not account.permits(permission):
                route = (
                    request.resolver_match.url_name
                    if request.resolver_match
                    else ""
                )
                return render(
                    request,
                    "console/forbidden.html",
                    admin_context(request, ACTION_SECTIONS.get(route, route)),
                    status=403,
                )
            return view(request, *args, **kwargs)

        return wrapped

    return decorate


def admin_context(request, section):
    account = request.admin_account
    return {
        "admin_username": account.username,
        "admin_roles": ", ".join(
            ROLE_LABELS.get(role, role) for role in account.roles
        ),
        "can_accounts": account.permits("accounts"),
        "can_manage_admins": account.permits("manage_admins"),
        "active_section": section,
        "can_moderate": account.permits("moderate"),
        "console_sections": [
            {
                "name": name,
                "label": label,
                "url": reverse(name),
                "active": name == section,
                "management_start": name == "users",
            }
            for name, label, permission in SECTIONS
            if account.permits(permission)
        ],
    }
