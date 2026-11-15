import hashlib
import logging
import os
import uuid

from .client import call


def record(event_type, username, actor_id=None):
    payload = {
        "event_id": str(uuid.uuid4()),
        "event_type": event_type,
        "actor_id": actor_id,
        "username_hash": hashlib.sha256(username.encode()).hexdigest(),
    }
    if os.getenv("ENGAGEMENT_SVC_URL"):
        call(
            "ENGAGEMENT_SVC_URL", "POST", "/internal/admin/auth-audit", payload
        )
    else:
        logging.getLogger(__name__).info(
            "Administrator authentication", extra=payload
        )
