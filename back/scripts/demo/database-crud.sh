#!/usr/bin/env bash
# Этап 3. Операции с БД через приложение: insert, select, update, delete.
#
# Запросы идут в API через шлюз, после каждого результат проверяется прямым
# SQL-запросом к PostgreSQL. В конце показаны счетчики операций PostgreSQL
# по двум таблицам и файл схемы db/schema.sql.
set -euo pipefail
source "$(dirname "$0")/lib.sh"
require_lab
cd "$(dirname "$0")/../.."   # каталог back

api="${GATEWAY}/api/v1"
json='Content-Type: application/json'
email="crud-$(date +%s)@demo.example.com"
password="Demo-$(openssl rand -hex 12)-Aa1!"
slug=demo-security-2027

counters() {
    sleep 11   # соединения сервисов передают статистику PostgreSQL с задержкой до 10 с
    pg_sql -c "SELECT format('  %-26s insert %-4s update %-4s delete %-4s select %s', schemaname||'.'||relname,
        n_tup_ins, n_tup_upd, n_tup_del, seq_scan+idx_scan)
        FROM pg_stat_user_tables WHERE relname IN ('users','favorites') ORDER BY 1"
}
call() { curl -s -m 10 -o /dev/null -w '%{http_code}' "$@"; }

step "1. Подготовка: опубликованная конференция и счетчики до сценария"
pg_sql -q -c "INSERT INTO conference_svc.conferences (id, title, slug, start_date, end_date, created_by, status, publication_allowed)
    SELECT gen_random_uuid(), 'Демо-конференция по безопасности ВС', '${slug}', '2027-03-10', '2027-03-12',
           '00000000-0000-4000-8000-000000000240', 'registration_open', true
    WHERE NOT EXISTS (SELECT 1 FROM conference_svc.conferences WHERE slug='${slug}')"
echo "  конференция ${slug}: $(pg_sql -c "SELECT status FROM conference_svc.conferences WHERE slug='${slug}'")"
counters

step "2. INSERT: регистрация пользователя (POST /auth/register)"
code="$(call -X POST "$api/auth/register" -H "$json" \
    -d "{\"email\":\"$email\",\"password\":\"$password\",\"first_name\":\"Иван\",\"last_name\":\"Иванов\"}")"
echo "  HTTP ${code}"
echo "  в базе: $(pg_sql -c "SELECT email || ', ' || first_name || ' ' || last_name FROM identity_svc.users WHERE email='$email'")"

token="$(curl -s -m 10 -X POST "$api/auth/login" -H "$json" -d "{\"email\":\"$email\",\"password\":\"$password\"}" \
    | python3 -c 'import json, sys; print(json.load(sys.stdin)["access_token"])')"
auth="Authorization: Bearer $token"

step "3. UPDATE: изменение профиля (PATCH /auth/me)"
code="$(call -X PATCH "$api/auth/me" -H "$auth" -H "$json" -d '{"first_name":"Сергей","last_name":"Иванов"}')"
echo "  HTTP ${code}"
echo "  в базе: $(pg_sql -c "SELECT email || ', ' || first_name || ' ' || last_name FROM identity_svc.users WHERE email='$email'")"

step "4. INSERT и SELECT: конференция в избранном (PUT и GET)"
echo "  PUT /conferences/${slug}/favorite: HTTP $(call -X PUT "$api/conferences/${slug}/favorite" -H "$auth")"
echo "  GET /favorites: $(curl -s -m 10 "$api/favorites" -H "$auth" \
    | python3 -c 'import json, sys; d = json.load(sys.stdin); print("всего", d["pagination"]["total"], [c["slug"] for c in d["data"]])')"
favorites="SELECT count(*) FROM conference_svc.favorites f JOIN identity_svc.users u ON u.id = f.user_id WHERE u.email='$email'"
echo "  в базе: строк в conference_svc.favorites у пользователя: $(pg_sql -c "$favorites")"

step "5. DELETE: удаление из избранного (DELETE)"
echo "  DELETE /conferences/${slug}/favorite: HTTP $(call -X DELETE "$api/conferences/${slug}/favorite" -H "$auth")"
echo "  в базе: строк в conference_svc.favorites у пользователя: $(pg_sql -c "$favorites")"

step "6. Счетчики операций PostgreSQL после сценария"
counters

step "7. SQL-файл схемы"
echo "  back/db/schema.sql: схем $(grep -c '^CREATE SCHEMA' db/schema.sql), таблиц $(grep -c '^CREATE TABLE' db/schema.sql), индексов $(grep -c '^CREATE .*INDEX' db/schema.sql)"
