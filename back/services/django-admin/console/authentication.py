import hmac
import re
import time

import pyotp
from django.contrib.auth.hashers import check_password
from django.core.cache import cache

from .credentials import get_credentials


def authenticated(request):
    credentials = get_credentials(request.session.get("admin_username"))
    valid = (
        credentials is not None
        and credentials.active
        and request.session.get("admin_2fa") is True
        and request.session.get("admin_auth_version") == credentials.version
    )
    if valid:
        request.admin_account = credentials
    return valid


def reserve_login_attempt(username=None):
    credentials = get_credentials(username) or get_credentials()
    key = f"admin-login:{credentials.version}"
    cache.add(key, 0, timeout=300)
    try:
        attempts = cache.incr(key)
    except ValueError:
        cache.add(key, 1, timeout=300)
        attempts = cache.incr(key)
    return key, attempts <= 5


def verify_credentials(username, password, otp):
    account = get_credentials(username)
    credentials = account or get_credentials()
    username_ok = hmac.compare_digest(
        username.encode(), credentials.username.encode()
    )
    password_ok = check_password(password, credentials.password_hash)
    if (
        account is None
        or not username_ok
        or not password_ok
        or not re.fullmatch(r"[0-9]{6}", otp)
    ):
        return False
    totp = pyotp.TOTP(credentials.totp_secret)
    counter = int(time.time()) // totp.interval
    for step in (counter - 1, counter, counter + 1):
        if hmac.compare_digest(totp.generate_otp(step), otp):
            # SET NX allows only one worker to consume this code.
            return cache.add(
                f"admin-otp:{credentials.version}:{step}", True, timeout=120
            )
    return False
