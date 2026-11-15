#!/usr/bin/env bash
# Общие функции демонстрационных сценариев. По умолчанию сценарии работают со
# стендом confhub-lab (scripts/lab.sh), а не с рабочим проектом confhub.

# Ширина колонок с кириллицей считается в символах только в UTF-8.
export LC_ALL=C.UTF-8

PROJECT="${COMPOSE_PROJECT:-confhub-lab}"
# Шлюз слушает loopback: 18080 у рабочего проекта, 18081 у стенда confhub-lab.
if [[ "$PROJECT" == confhub ]]; then default_port=18080; else default_port=18081; fi
GATEWAY="${GATEWAY_URL:-http://127.0.0.1:${HOST_PROXY_PORT:-$default_port}}"
PG_NODES=(postgres postgres-2)
NATS_NODES=(nats nats-2 nats-3)
APP_SERVICES=(identity-svc conference-svc application-svc engagement-svc)

[[ "$PROJECT" =~ ^[a-zA-Z0-9_-]+$ ]] || { echo "Unsafe compose project" >&2; exit 2; }

container() { echo "${PROJECT}-$1-1"; }
now() { date +%s.%N; }
elapsed() { awk -v a="$1" -v b="$(now)" 'BEGIN { printf "%.1f", b - a }'; }
step() { printf '\n\033[1m== %s\033[0m\n' "$*"; }
stamp() { date +%H:%M:%S; }

# Сценарии с отказами и потерей данных нельзя случайно запустить на рабочем стенде.
require_lab() {
    if [[ "$PROJECT" == confhub && "${CONFIRM_PRODUCTION:-}" != yes ]]; then
        echo "Refusing to run a failure scenario against the production project." >&2
        echo "Use the lab stand (scripts/lab.sh up) or set CONFIRM_PRODUCTION=yes." >&2
        exit 2
    fi
}

running() { [[ "$(docker inspect -f '{{.State.Running}}' "$(container "$1")" 2>/dev/null)" == true ]]; }

# --- PostgreSQL / Patroni -----------------------------------------------------
pg_cluster() {
    local node
    for node in "${PG_NODES[@]}"; do
        if running "$node" && docker exec "$(container "$node")" patronictl list "$@" 2>/dev/null; then return 0; fi
    done
    return 1
}

# Имя узла с указанной ролью (Leader, Sync Standby, Replica) или пустая строка.
pg_member() {
    pg_cluster -f json | python3 -c '
import json, sys
role = sys.argv[1].lower()
for member in json.load(sys.stdin):
    if member.get("Role", "").lower() == role and member.get("State") in ("running", "streaming"):
        print(member["Member"])
        break
' "$1" 2>/dev/null || true
}

pg_sql() {
    local leader
    leader="$(pg_member leader)"
    [[ -n "$leader" ]] || { echo "No PostgreSQL leader" >&2; return 1; }
    docker exec -i "$(container "$leader")" psql -X -At -v ON_ERROR_STOP=1 -d confhub "$@"
}

# --- NATS JetStream -----------------------------------------------------------
# Печатает: <лидер стрима> <число реплик> <сообщений> <реплики, догнавшие лидера>
# Точное состояние реплик знает только лидер стрима, поэтому опрашиваются все
# узлы и берётся ответ того, кто назвал лидером себя.
nats_stream() {
    local node answer fallback=""
    for node in "${NATS_NODES[@]}"; do
        running "$node" || continue
        answer="$(docker exec "$(container "$node")" wget -qO- -T 3 'http://127.0.0.1:8222/jsz?streams=true&config=true' 2>/dev/null | python3 -c '
import json, sys
data = json.load(sys.stdin)
for account in data.get("account_details", []):
    for stream in account.get("stream_detail", []):
        cluster = stream.get("cluster", {})
        current = [r["name"] for r in cluster.get("replicas", []) if r.get("current")]
        own = "own" if cluster.get("leader") == data.get("server_name", sys.argv[1]) else "other"
        print(own, cluster.get("leader") or "-", stream["config"]["num_replicas"], stream["state"]["messages"], ",".join(sorted(current)) or "-")
        sys.exit(0)
sys.exit(1)
' "$(nats_server "$node")" 2>/dev/null)" || continue
        if [[ "$answer" == own\ * ]]; then echo "${answer#own }"; return 0; fi
        fallback="${answer#other }"
    done
    [[ -n "$fallback" ]] && echo "$fallback"
}

# Имя сервера NATS по имени сервиса compose (nats -> nats-1) и обратно.
nats_server() { if [[ "$1" == nats ]]; then echo nats-1; else echo "$1"; fi; }
nats_service() { if [[ "$1" == nats-1 ]]; then echo nats; else echo "$1"; fi; }

# --- Прикладной запрос на запись ----------------------------------------------
# Регистрация пользователя: INSERT в PostgreSQL и событие user.registered в NATS.
# Печатает код ответа HTTP.
register_user() {
    curl -s -o /dev/null -m "${2:-4}" -w '%{http_code}' -X POST "${GATEWAY}/api/v1/auth/register" \
        -H 'Content-Type: application/json' \
        -d "{\"email\":\"$1\",\"password\":\"Demo-$(openssl rand -hex 12)-Aa1!\",\"first_name\":\"Demo\",\"last_name\":\"User\"}" || true
}

api_code() { curl -s -o /dev/null -m "${2:-4}" -w '%{http_code}' "${GATEWAY}$1" || true; }

# Код ответа и время запроса: "200 0.012".
api_timed() { curl -s -o /dev/null -m "${2:-10}" -w '%{http_code} %{time_total}\n' "${GATEWAY}$1" || echo '000 0'; }

# Сетевое пространство контейнера сервиса: netns <сервис> <команда...>
netns() {
    local pid
    pid="$(docker inspect -f '{{.State.Pid}}' "$(container "$1")")"
    shift
    nsenter -t "$pid" -n "$@"
}
