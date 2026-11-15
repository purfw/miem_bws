#!/usr/bin/env bash
# Этап 5. Состояние отказоустойчивого контура: PostgreSQL под Patroni, etcd, NATS.
# Сценарий только читает состояние, его можно запускать и на рабочем стенде:
#   COMPOSE_PROJECT=confhub ./scripts/demo/ha-status.sh
set -euo pipefail
source "$(dirname "$0")/lib.sh"

step "1. PostgreSQL: лидер и синхронная реплика (patronictl list)"
pg_cluster

step "2. Репликация на лидере"
pg_sql -c "SELECT format('  реплика %s: %s, режим %s, отставание %s байт', application_name, state, sync_state,
    coalesce(pg_wal_lsn_diff(sent_lsn, replay_lsn), 0)) FROM pg_stat_replication"

step "3. etcd: хранилище состояния Patroni"
for node in etcd-1 etcd-2 etcd-3; do
    echo "  $(docker exec "$(container "$node")" etcdctl endpoint health 2>&1 | tail -1 | sed "s#127.0.0.1:2379#${node}#")"
done

step "4. NATS JetStream: стрим CONFHUB_EVENTS"
read -r leader replicas messages current < <(nats_stream)
echo "  лидер стрима: ${leader}, реплик: ${replicas}, сообщений: ${messages}"
echo "  реплики, догнавшие лидера: ${current}"
