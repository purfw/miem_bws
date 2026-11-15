import os
from pathlib import Path
from urllib.parse import urlsplit

import django
import pyotp
import pytest
from django.conf import settings
from django.core.cache import cache

from console.credentials import (
    AdminPasswordHasher,
    Credentials,
    get_credentials,
    save_credentials,
)


test_redis = os.getenv("TEST_REDIS_URL", "")
if any(
    os.getenv(name)
    for name in (
        "IDENTITY_SVC_URL",
        "CONFERENCE_SVC_URL",
        "APPLICATION_SVC_URL",
        "ENGAGEMENT_SVC_URL",
        "DATABASE_URL",
        "REDIS_URL",
        "ADMIN_AUTH_FILE",
        "ADMIN_AUTH_KEY_FILE",
    )
):
    raise RuntimeError(
        "Refusing tests with application services or credentials"
    )
if test_redis and not (urlsplit(test_redis).hostname or "").startswith(
    "confhub-test-redis-"
):
    raise RuntimeError("Refusing a Redis instance outside the test namespace")

if settings.configured:
    backend = settings.CACHES["default"]["BACKEND"]
    location = settings.CACHES["default"].get("LOCATION", "")
    if backend != "django.core.cache.backends.locmem.LocMemCache" and (
        not test_redis or location != test_redis
    ):
        raise RuntimeError(
            "Refusing configured application caches. Run tests in a separate "
            "container without production settings and credentials."
        )
else:
    settings.configure(
        SECRET_KEY="test-only-secret-not-for-deployment",
        DEBUG=False,
        ALLOWED_HOSTS=["testserver"],
        ROOT_URLCONF="conf_admin.urls",
        INSTALLED_APPS=[],
        MIDDLEWARE=[
            "django.contrib.sessions.middleware.SessionMiddleware",
            "django.middleware.csrf.CsrfViewMiddleware",
        ],
        SESSION_ENGINE="django.contrib.sessions.backends.cache",
        SESSION_COOKIE_NAME="confhub_admin_session",
        SESSION_COOKIE_AGE=900,
        CACHES={
            "default": {
                "BACKEND": (
                    "django.core.cache.backends.redis.RedisCache"
                    if test_redis
                    else "django.core.cache.backends.locmem.LocMemCache"
                ),
                "LOCATION": test_redis or "confhub-tests",
            }
        },
        PASSWORD_HASHERS=[
            "console.credentials.AdminPasswordHasher",
            "django.contrib.auth.hashers.PBKDF2PasswordHasher",
        ],
        TEMPLATES=[
            {
                "BACKEND": "django.template.backends.django.DjangoTemplates",
                "DIRS": [Path(__file__).resolve().parents[2] / "templates"],
                "APP_DIRS": False,
            }
        ],
    )
    django.setup()


@pytest.fixture
def credentials(tmp_path, monkeypatch):
    hasher = AdminPasswordHasher()
    value = Credentials(
        username="admin",
        password_hash=hasher.encode("Test-password-2026", hasher.salt()),
        totp_secret=pyotp.random_base32(),
        actor_id="5534bcbc-2a36-46ca-8149-e19ce8e7a0d6",
        version="bd1f6d8b-e245-4ca4-9e5d-9e55e7e9d92b",
    )
    directory = tmp_path / "admin-auth"
    save_credentials(directory, value)
    monkeypatch.setenv("ADMIN_AUTH_FILE", str(directory / "credentials.enc"))
    monkeypatch.setenv("ADMIN_AUTH_KEY_FILE", str(directory / "key"))
    get_credentials.cache_clear()
    cache.clear()
    yield value
    get_credentials.cache_clear()
    cache.clear()
