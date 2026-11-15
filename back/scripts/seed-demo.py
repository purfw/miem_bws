#!/usr/bin/env python3
"""Idempotently seed the public catalog through Conference Svc internal API."""

import hashlib
import hmac
import json
import os
import sys
import time
import urllib.error
import urllib.request
import uuid

INTERNAL_BASE = os.environ.get("CONFERENCE_INTERNAL_URL", "http://127.0.0.1:8082/internal")
PUBLIC_BASE = os.environ.get("PUBLIC_API_URL", "http://127.0.0.1:18080/api/v1")
SECRET = os.environ.get("SERVICE_TOKEN_SECRET", "")
CREATED_BY = "00000000-0000-4000-8000-000000000240"

CONFERENCES = [
    {
        "title": "VI Всероссийская конференция по когнитивной нейронауке",
        "slug": "cognitive-neuroscience-2026",
        "description": "Эмпирические исследования мозга и поведения, нейровизуализация и вычислительные модели.",
        "format": "offline", "start_date": "2026-09-12", "end_date": "2026-09-14",
        "registration_deadline": "2026-09-05T20:59:00Z", "max_participants": 300,
        "location": "Научный кампус", "city": "Санкт-Петербург", "topics": ["Нейронауки", "Когнитивные науки"],
        "featured": True,
    },
    {
        "title": "Машинное обучение и обработка естественного языка",
        "slug": "ml-nlp-2027",
        "description": "Прикладные методы NLP, мультимодальные модели и retrieval-augmented generation.",
        "format": "hybrid", "start_date": "2027-01-20", "end_date": "2027-01-22",
        "registration_deadline": "2026-12-20T20:59:00Z", "max_participants": 500,
        "location": "МИЭМ", "city": "Москва", "topics": ["Машинное обучение", "Лингвистика"],
    },
    {
        "title": "Биоинформатика: вычислительные подходы к геномике",
        "slug": "bioinformatics-genomics-2027",
        "description": "Анализ геномных данных, машинное обучение для биологии и открытые наборы данных.",
        "format": "offline", "start_date": "2027-03-05", "end_date": "2027-03-07",
        "registration_deadline": "2026-12-01T20:59:00Z", "max_participants": 250,
        "location": "Академгородок", "city": "Новосибирск", "topics": ["Биоинформатика", "Геномика"],
        "status": "registration_closed",
    },
    {
        "title": "Цифровая археология и культурное наследие",
        "slug": "digital-archaeology-2027",
        "description": "Методы 3D-реконструкции, ГИС в археологии и цифровые архивы артефактов.",
        "format": "offline", "start_date": "2027-04-18", "end_date": "2027-04-19",
        "registration_deadline": "2027-03-20T20:59:00Z", "max_participants": 180,
        "location": "Казанский федеральный университет", "city": "Казань", "topics": ["Археология", "Цифровое наследие"],
    },
    {
        "title": "Социолингвистика и языковая политика регионов",
        "slug": "sociolinguistics-2027",
        "description": "Языковые контакты, многоязычие и корпусные исследования живой речи.",
        "format": "hybrid", "start_date": "2027-05-03", "end_date": "2027-05-05",
        "registration_deadline": "2027-04-01T20:59:00Z", "max_participants": 220,
        "location": "Уральский федеральный университет", "city": "Екатеринбург", "topics": ["Лингвистика", "Социология"],
    },
    {
        "title": "Физика конденсированных сред: новые материалы",
        "slug": "condensed-matter-2027",
        "description": "Сверхпроводники, наноматериалы и квантовые эффекты в твёрдых телах.",
        "format": "online", "start_date": "2027-06-16", "end_date": "2027-06-18",
        "registration_deadline": "2027-01-15T20:59:00Z", "max_participants": 600,
        "city": "Онлайн", "topics": ["Физика", "Новые материалы"],
        "status": "registration_closed",
    },
]

SECTIONS = [
    {"title": "Пленарные доклады", "description": "Ключевые выступления конференции", "sort_order": 1},
    {"title": "Молодые исследователи", "description": "Секция студенческих и аспирантских работ", "sort_order": 2},
]
ORGANIZERS = [
    {"name": "Организационный комитет ConferenceHub", "affiliation": "ConferenceHub", "role": "chair", "kind": "organizer", "job_title": "Председатель", "sort_order": 1},
    {"name": "Программный комитет ConferenceHub", "affiliation": "Научное сообщество", "role": "member", "kind": "committee", "job_title": "Координатор программы", "sort_order": 2},
]


def request(method, url, payload=None, signed=False):
    body = b"" if payload is None else json.dumps(payload, ensure_ascii=False).encode()
    headers = {"Accept": "application/json"}
    if payload is not None:
        headers["Content-Type"] = "application/json"
    if signed:
        timestamp = str(int(time.time()))
        nonce = str(uuid.uuid4())
        digest = hmac.new(SECRET.encode(), body + timestamp.encode() + nonce.encode(), hashlib.sha256).hexdigest()
        headers["X-Service-Token"] = f"{timestamp}.{nonce}.{digest}"
    req = urllib.request.Request(url, data=body if payload is not None else None, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=15) as response:
            raw = response.read()
            return response.status, json.loads(raw) if raw else None
    except urllib.error.HTTPError as error:
        raw = error.read().decode(errors="replace")
        return error.code, raw


def main():
    if not SECRET:
        sys.exit("SERVICE_TOKEN_SECRET is required")
    export_status, export_result = request("GET", f"{INTERNAL_BASE}/conferences/export?limit=500", signed=True)
    if export_status != 200 or not isinstance(export_result, dict):
        sys.exit(f"catalog export failed ({export_status}): {export_result}")
    known = {item["slug"]: item for item in export_result.get("items", [])}
    created = 0
    skipped = 0
    for source in CONFERENCES:
        status, _ = request("GET", f"{PUBLIC_BASE}/conferences/{source['slug']}")
        if status == 200:
            print(f"skip {source['slug']}: already exists")
            skipped += 1
            continue
        existing = known.get(source["slug"])
        if existing:
            conference_id = existing["id"]
            current_status = existing["status"]
        else:
            payload = {key: value for key, value in source.items() if key not in {"status", "featured"}}
            payload.update({"created_by": CREATED_BY, "sections": SECTIONS, "organizers": ORGANIZERS, "partners": []})
            status, result = request("POST", f"{INTERNAL_BASE}/conferences", payload, signed=True)
            if status != 201 or not isinstance(result, dict):
                sys.exit(f"create {source['slug']} failed ({status}): {result}")
            conference_id = result["id"]
            current_status = "draft"
        target_status = source.get("status", "registration_open")
        transitions = []
        if current_status == "draft":
            transitions.append("registration_open")
        if target_status == "registration_closed" and current_status != "registration_closed":
            transitions.append("registration_closed")
        if not transitions:
            transitions.append(target_status)
        for index, next_status in enumerate(transitions):
            patch = {"status": next_status}
            if index == 0:
                patch["is_featured"] = bool(source.get("featured", False))
            update_status, update_result = request("PATCH", f"{INTERNAL_BASE}/conferences/{conference_id}", patch, signed=True)
            if update_status != 200:
                sys.exit(f"publish {source['slug']} failed ({update_status}): {update_result}")
        print(f"created {source['slug']}")
        created += 1
    print(f"done: created={created}, skipped={skipped}")


if __name__ == "__main__":
    main()
