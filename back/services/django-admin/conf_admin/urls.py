from django.urls import path

from console import views, team, users

urlpatterns = [
    path("healthz", views.health, name="health"),
    path("admin/login/", views.login_view, name="login"),
    path("admin/logout/", views.logout_view, name="logout"),
    path("admin/", views.dashboard, name="dashboard"),
    path("admin/verifications/", views.verifications, name="verifications"),
    path("admin/applications/", views.applications, name="applications"),
    path("admin/accounts/", views.accounts, name="accounts"),
    path("admin/queues/", views.queues, name="queues"),
    path("admin/audit/", views.audit, name="audit"),
    path("admin/users/", users.users, name="users"),
    path("admin/users/access/", users.change_user, name="change_user"),
    path("admin/team/", team.team, name="team"),
    path("admin/team/invite/", team.invite, name="invite_admin"),
    path(
        "admin/team/<uuid:actor_id>/access/",
        team.change_access,
        name="change_admin_access",
    ),
    path("admin/activate/", team.activate, name="activate_admin"),
    path(
        "admin/queues/<uuid:item_id>/retry/",
        views.retry_queue,
        name="retry_queue",
    ),
    path(
        "admin/verifications/<uuid:item_id>/<str:status>/",
        views.moderate_verification,
        name="moderate_verification",
    ),
    path(
        "admin/verification-documents/<uuid:item_id>/",
        views.verification_document,
        name="verification_document",
    ),
    path(
        "admin/applications/<uuid:item_id>/<str:status>/",
        views.moderate_application,
        name="moderate_application",
    ),
    path(
        "admin/accounts/active/",
        views.set_account_active,
        name="set_account_active",
    ),
]
