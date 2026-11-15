#!/usr/bin/env python3
import datetime as dt
import hashlib
import hmac
import html
import http.cookiejar
import json
import pathlib
import re
import secrets
import subprocess
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid
from ops_transport import internal_opener

BASE = "https://91.132.58.253"
ROOT = pathlib.Path(__file__).resolve().parent.parent
ENV = dict(
    line.split("=", 1)
    for line in (ROOT / ".env.prod").read_text().splitlines()
    if "=" in line and not line.startswith("#")
)
SECRET = ENV["SERVICE_TOKEN_SECRET"]
USERS = []
CONFERENCE = None
FILES = []
SUPPORT_MESSAGES = []
LAST_PUBLIC_CALL = 0.0


def container_ip(service):
    return subprocess.check_output(
        [
            "docker",
            "inspect",
            "-f",
            "{{range.NetworkSettings.Networks}}{{.IPAddress}}{{end}}",
            f"confhub-{service}-1",
        ],
        text=True,
    ).strip()


INTERNAL = {
    name: f"https://{name}:8443"
    for name in ("identity-svc", "conference-svc", "application-svc", "engagement-svc")
}
INTERNAL_OPENER = internal_opener(ROOT)
MAILPIT = f"http://{container_ip('mailpit')}:8025"


def call(
    method, path, data=None, user=None, expected=(200, 201, 202, 204), service=None
):
    global LAST_PUBLIC_CALL
    if not service:
        interval = 2.1 if path.startswith(("/auth/", "/support")) else 0.7
        time.sleep(max(0, interval - (time.monotonic() - LAST_PUBLIC_CALL)))
        LAST_PUBLIC_CALL = time.monotonic()
    body = b"" if data is None else json.dumps(data, separators=(",", ":")).encode()
    headers = {"Content-Type": "application/json", "Accept": "application/json"}
    if user and user.get("token"):
        headers["Authorization"] = "Bearer " + user["token"]
    if service:
        timestamp, nonce = str(int(time.time())), str(uuid.uuid4())
        digest = hashlib.sha256(body).hexdigest()
        signature = hmac.new(
            SECRET.encode(),
            f"v2\n{method}\n{path}\n{timestamp}\n{nonce}\n{digest}".encode(),
            hashlib.sha256,
        ).hexdigest()
        headers["X-Service-Token"] = f"v2.{timestamp}.{nonce}.{signature}"
    url = INTERNAL[service] + path if service else BASE + "/api/v1" + path
    request = urllib.request.Request(
        url, data=body if data is not None else None, method=method, headers=headers
    )
    opener = (
        INTERNAL_OPENER
        if service
        else (user["opener"] if user else urllib.request.build_opener())
    )
    try:
        response = opener.open(request, timeout=20)
    except urllib.error.HTTPError as error:
        response = error
    with response:
        status, raw = response.status, response.read()
    if status not in expected:
        raise RuntimeError(f"{method} {path.split('?')[0]}: HTTP {status}")
    return json.loads(raw) if raw and raw.startswith((b"{", b"[")) else None


def poll(predicate, description, seconds=25):
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        result = predicate()
        if result:
            return result
        time.sleep(0.5)
    raise RuntimeError("Timed out: " + description)


def message(user, contains):
    with urllib.request.urlopen(
        MAILPIT + "/api/v1/messages?limit=100", timeout=10
    ) as response:
        items = json.load(response)["messages"]
    for item in items:
        if not any(
            recipient["Address"].lower() == user["email"] for recipient in item["To"]
        ):
            continue
        with urllib.request.urlopen(
            MAILPIT + "/api/v1/message/" + item["ID"], timeout=10
        ) as response:
            full = json.load(response)
        if contains in full.get("HTML", ""):
            return full
    return None


def login(user):
    result = call(
        "POST",
        "/auth/login",
        {"email": user["email"], "password": user["password"]},
        user=user,
    )
    user["token"] = result["access_token"]
    return result["user"]


def register(label):
    user = {
        "email": f"smoke-{label}-{uuid.uuid4().hex}@example.test",
        "password": secrets.token_urlsafe(24),
        "opener": urllib.request.build_opener(
            urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar())
        ),
    }
    result = call(
        "POST",
        "/auth/register",
        {
            "email": user["email"],
            "password": user["password"],
            "first_name": "Smoke",
            "last_name": "Test",
            "locale": "en",
        },
        user=user,
    )
    user["id"] = result["user_id"]
    USERS.append(user)
    mail = poll(lambda: message(user, "/verify?token="), "verification email")
    token = re.search(r"/verify\?token=([a-f0-9-]+)", html.unescape(mail["HTML"]))[1]
    call("GET", "/auth/verify?token=" + token, expected=(204,))
    call("GET", "/auth/verify?token=" + token, expected=(404,))
    assert login(user)["email_verified"]
    return user


def main():
    global CONFERENCE
    owner, attendee = register("owner"), register("attendee")
    print(
        "PASS registration, localized confirmation emails, single-use verification",
        flush=True,
    )
    verification = call(
        "POST",
        "/verifications/",
        {"organization": "Smoke Test Organization"},
        user=owner,
    )
    call(
        "PATCH",
        "/internal/verifications/" + verification["id"],
        {"status": "approved", "changed_by": owner["id"]},
        service="application-svc",
    )
    poll(
        lambda: call("GET", "/internal/users/" + owner["id"], service="identity-svc")[
            "role"
        ]
        == "verified_organizer",
        "organizer role delivery",
    )
    call(
        "PATCH",
        "/internal/verifications/" + verification["id"],
        {"status": "rejected", "reason": "duplicate test", "changed_by": owner["id"]},
        expected=(409,),
        service="application-svc",
    )
    login(owner)
    print(
        "PASS verification decision, durable role delivery, duplicate decision rejected",
        flush=True,
    )
    start = max(dt.date.today() + dt.timedelta(days=2), dt.date(2027, 1, 1))
    deadline = dt.datetime.now(dt.timezone.utc) + dt.timedelta(hours=6)
    conference_title = "Smoke test " + uuid.uuid4().hex[:8]
    conference = call(
        "POST",
        "/conferences/",
        {
            "title": conference_title,
            "slug": "smoke-" + uuid.uuid4().hex,
            "description": "Temporary deployment verification event",
            "format": "online",
            "start_date": start.isoformat(),
            "end_date": (start + dt.timedelta(days=1)).isoformat(),
            "registration_deadline": deadline.isoformat(),
            "max_participants": 1,
        },
        user=owner,
    )
    CONFERENCE = conference["id"]
    criteria = {
        "criteria": {
            "formats": ["online"],
            "from": start.isoformat(),
            "to": start.isoformat(),
        },
        "notify_on": ["conference_published"],
        "channels": ["email"],
    }
    call(
        "POST",
        "/subscriptions/criteria",
        {**criteria, "criteria": {}},
        user=attendee,
        expected=(422,),
    )
    filtered = call("POST", "/subscriptions/criteria", criteria, user=attendee)["data"]
    assert filtered["conference_id"] is None
    assert (
        call("POST", "/subscriptions/criteria", criteria, user=attendee)["data"]["id"]
        == filtered["id"]
    )
    call("DELETE", "/subscriptions/" + filtered["id"], user=owner, expected=(404,))
    call(
        "PATCH",
        "/conferences/" + CONFERENCE,
        {"status": "registration_open"},
        user=owner,
    )
    poll(
        lambda: message(attendee, conference_title),
        "matching criteria publication notification",
    )
    call("DELETE", "/subscriptions/" + filtered["id"], user=attendee)
    print(
        "PASS criteria subscriptions, canonical deduplication, ownership and publication delivery",
        flush=True,
    )
    call("PUT", "/conferences/" + CONFERENCE + "/favorite", user=attendee)
    assert any(
        item["id"] == CONFERENCE
        for item in call("GET", "/favorites", user=attendee)["data"]
    )
    call("DELETE", "/conferences/" + CONFERENCE + "/favorite", user=attendee)
    subscription = call(
        "POST",
        "/subscriptions/",
        {
            "conference_id": CONFERENCE,
            "notify_on": [
                "deadline_approaching",
                "session_updated",
                "speaker_added",
                "conference_cancelled",
            ],
            "channels": ["email"],
        },
        user=attendee,
    )["data"]
    slot = {
        "title": "Deployment test talk",
        "slot_date": start.isoformat(),
        "start_time": "10:00",
        "end_time": "11:00",
        "speaker_name": "Smoke Speaker",
        "session_type": "talk",
    }
    session = call("POST", "/conferences/" + CONFERENCE + "/sessions", slot, user=owner)
    poll(lambda: message(attendee, "programme has changed"), "programme notification")
    poll(lambda: message(attendee, "new speaker"), "speaker notification")
    print(
        "PASS create conference without topics, favorites, programme and speaker subscriptions",
        flush=True,
    )
    confirmed = call(
        "POST",
        "/conferences/" + CONFERENCE + "/register",
        {"role_at_conf": "attendee"},
        user=owner,
    )["data"]
    waitlisted = call(
        "POST",
        "/conferences/" + CONFERENCE + "/register",
        {"role_at_conf": "attendee"},
        user=attendee,
    )["data"]
    assert confirmed["status"] == "confirmed" and waitlisted["status"] == "waitlisted"
    call(
        "PUT",
        "/sessions/" + session["id"] + "/link",
        {"join_url": "https://example.test/meeting"},
        user=owner,
    )
    call("GET", "/sessions/" + session["id"] + "/join", user=attendee, expected=(403,))
    call("DELETE", "/registrations/" + confirmed["id"], user=owner)
    poll(
        lambda: any(
            item["id"] == waitlisted["id"] and item["status"] == "confirmed"
            for item in call("GET", "/my/registrations?per_page=100", user=attendee)[
                "data"
            ]
        ),
        "waitlist promotion",
    )
    assert (
        call("GET", "/sessions/" + session["id"] + "/join", user=attendee)["url"]
        == "https://example.test/meeting"
    )
    print(
        "PASS capacity, waitlist promotion, private join-link permissions", flush=True
    )
    call(
        "POST",
        "/conferences/" + CONFERENCE + "/applications",
        {
            "form": "speaker",
            "title": "Invalid section test",
            "section_id": str(uuid.uuid4()),
        },
        user=attendee,
        expected=(422,),
    )
    application = call(
        "POST",
        "/conferences/" + CONFERENCE + "/applications",
        {"form": "speaker", "title": "Deployment test presentation"},
        user=attendee,
    )
    app_id = application["id"]
    call(
        "PATCH",
        "/applications/" + app_id,
        {"section_id": "invalid"},
        user=attendee,
        expected=(422,),
    )
    call(
        "PATCH",
        "/applications/" + app_id,
        {"section_id": str(uuid.uuid4())},
        user=attendee,
        expected=(422,),
    )
    call("GET", "/applications/" + app_id, user=owner, expected=(403,))
    boundary = uuid.uuid4().hex
    pdf = b"%PDF-1.4\n1 0 obj\n<< /Type /Catalog >>\nendobj\n%%EOF\n"
    body = (
        (
            f'--{boundary}\r\nContent-Disposition: form-data; name="file"; filename="smoke.pdf"\r\nContent-Type: application/pdf\r\n\r\n'
        ).encode()
        + pdf
        + f"\r\n--{boundary}--\r\n".encode()
    )
    request = urllib.request.Request(
        BASE + "/api/v1/applications/" + app_id + "/files",
        data=body,
        headers={
            "Authorization": "Bearer " + attendee["token"],
            "Content-Type": "multipart/form-data; boundary=" + boundary,
        },
    )
    with attendee["opener"].open(request, timeout=20) as response:
        uploaded = json.load(response)
    FILES.append((attendee, uploaded["id"]))
    url = call("GET", "/files/" + uploaded["id"] + "?redirect=false", user=attendee)[
        "url"
    ]
    with urllib.request.urlopen(url, timeout=20) as response:
        assert response.read() == pdf
    call(
        "GET",
        "/files/" + uploaded["id"] + "?redirect=false",
        user=owner,
        expected=(403,),
    )
    call("DELETE", "/files/" + uploaded["id"], user=attendee)
    FILES.clear()
    moderation_path = "/internal/applications/" + app_id + "/moderate"
    for status in ["under_review", "revision_requested"]:
        call(
            "PATCH",
            moderation_path,
            {
                "status": status,
                "comment": "Please revise the abstract",
                "changed_by": owner["id"],
            },
            service="application-svc",
        )
    assert (
        call("GET", "/applications/" + app_id, user=attendee)["moderator_comment"]
        == "Please revise the abstract"
    )
    call(
        "PATCH",
        "/applications/" + app_id,
        {
            "title": "Revised test presentation",
            "abstract": "Updated abstract for deployment verification",
        },
        user=attendee,
    )
    assert (
        call("POST", "/applications/" + app_id + "/resubmit", user=attendee)["status"]
        == "submitted"
    )
    for status in ["under_review", "accepted"]:
        call(
            "PATCH",
            moderation_path,
            {"status": status, "changed_by": owner["id"]},
            service="application-svc",
        )
    call(
        "PATCH",
        "/applications/" + app_id,
        {"title": "Must be rejected"},
        user=attendee,
        expected=(409,),
    )
    print(
        "PASS applications, private file upload/download/delete, revision and resubmission, moderation",
        flush=True,
    )
    support = call(
        "POST",
        "/support/messages",
        {
            "name": "Smoke Test",
            "email": attendee["email"],
            "topic": "bug",
            "message": "Temporary deployment support verification",
        },
    )
    SUPPORT_MESSAGES.append((support["id"], attendee["email"]))
    assert support["status"] == "accepted"
    print("PASS support form accepted and persisted", flush=True)
    call("DELETE", "/subscriptions/" + subscription["id"], user=attendee)
    assert not any(
        item["id"] == subscription["id"]
        for item in call("GET", "/subscriptions/", user=attendee)["data"]
    )
    old_access = attendee["token"]
    call("POST", "/auth/forgot-password", {"email": attendee["email"]}, user=attendee)
    reset = poll(lambda: message(attendee, "/reset-password?token="), "reset email")
    token = re.search(
        r"/reset-password\?token=([a-f0-9-]+)", html.unescape(reset["HTML"])
    )[1]
    attendee["password"] = secrets.token_urlsafe(24)
    call(
        "POST",
        "/auth/reset-password",
        {"token": token, "new_password": attendee["password"]},
        user=attendee,
    )
    attendee["token"] = old_access
    call("GET", "/auth/me", user=attendee, expected=(401,))
    call("GET", "/favorites", user=attendee, expected=(401,))
    call("POST", "/auth/refresh", user=attendee, expected=(401,))
    login(attendee)
    print(
        "PASS unsubscribe, password reset, old access/refresh tokens rejected across services",
        flush=True,
    )


def cleanup():
    for message_id, email in SUPPORT_MESSAGES:
        assert re.fullmatch(r"smoke-[a-z-]+-[a-f0-9]{32}@example\.test", email)
        query = (
            "DELETE FROM engagement_svc.support_messages WHERE id='"
            + str(uuid.UUID(message_id))
            + "' AND email='"
            + email
            + "'"
        )
        subprocess.run(
            [
                "docker",
                "exec",
                "confhub-postgres-1",
                "psql",
                "-X",
                "-U",
                "postgres",
                "-d",
                "confhub",
                "-v",
                "ON_ERROR_STOP=1",
                "-c",
                query,
            ],
            check=True,
            stdout=subprocess.DEVNULL,
        )
    for user, file_id in FILES:
        call("DELETE", "/files/" + file_id, user=user)
    if CONFERENCE:
        status = call(
            "GET",
            "/internal/conferences/" + CONFERENCE + "/status",
            service="conference-svc",
        )["status"]
        if status != "completed":
            call(
                "DELETE",
                "/internal/conferences/" + CONFERENCE,
                service="conference-svc",
            )
        subprocess.run(
            [
                "docker",
                "exec",
                "confhub-postgres-1",
                "psql",
                "-X",
                "-U",
                "postgres",
                "-d",
                "confhub",
                "-v",
                "ON_ERROR_STOP=1",
                "-c",
                "UPDATE conference_svc.conferences SET deleted_at=now() WHERE id='"
                + str(uuid.UUID(CONFERENCE))
                + "'",
            ],
            check=True,
            stdout=subprocess.DEVNULL,
        )
    for user in USERS:
        try:
            call(
                "DELETE", "/auth/me", {"current_password": user["password"]}, user=user
            )
        except Exception:
            login(user)
            call(
                "DELETE", "/auth/me", {"current_password": user["password"]}, user=user
            )
    print(
        "Temporary conference hidden and test accounts deleted through the application",
        flush=True,
    )


if __name__ == "__main__":
    try:
        main()
    finally:
        cleanup()
