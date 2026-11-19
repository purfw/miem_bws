#!/bin/bash
# Usage: ./manage_monitoring.sh <command>

set -euo pipefail

MONITORING_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENV_FILE="${MONITORING_DIR}/.env"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log_info() { echo -e "${GREEN}[INFO]${NC} $*"; }
log_warn() { echo -e "${YELLOW}[WARN]${NC} $*"; }
log_error() { echo -e "${RED}[ERROR]${NC} $*"; }

if [[ ! -f "$ENV_FILE" && "${1:-help}" != setup && "${1:-help}" != help ]]; then
    log_warn "monitoring/.env not found; run ./manage_monitoring.sh setup first"
fi

cd "$MONITORING_DIR"

case "${1:-help}" in
    start)
        log_info "Starting monitoring stack..."
        docker compose -f docker-compose.monitoring.yml up -d
        log_info "Monitoring stack started!"
        echo ""
        echo "Services:"
        echo "  - Prometheus:    http://localhost:9090"
        echo "  - Grafana:       http://localhost:3000 (credentials from monitoring/.env)"
        echo "  - Loki:          http://localhost:3100"
        echo "  - Alertmanager:  http://localhost:9093"
        echo "  - Node Exporter: http://localhost:9100"
        echo "  - cAdvisor:      http://localhost:8081"
        ;;

    stop)
        log_info "Stopping monitoring stack..."
        docker compose -f docker-compose.monitoring.yml down
        log_info "Monitoring stack stopped"
        ;;

    restart)
        log_info "Restarting monitoring stack..."
        docker compose -f docker-compose.monitoring.yml restart
        log_info "Monitoring stack restarted"
        ;;

    status)
        docker compose -f docker-compose.monitoring.yml ps
        ;;

    logs)
        docker compose -f docker-compose.monitoring.yml logs "${2:-}"
        ;;

    logs-follow)
        docker compose -f docker-compose.monitoring.yml logs -f "${2:-}"
        ;;

    backup)
        log_info "Running backup..."
        bash "${MONITORING_DIR}/backup/backup.sh"
        ;;

    restore)
        if [[ -z "${3:-}" ]]; then
            log_error "Usage: $0 restore --check|--verify-db /path/confhub-<timestamp>.tar.gz.age"
            exit 1
        fi
        log_info "Checking backup: $3"
        bash "${MONITORING_DIR}/backup/restore.sh" "$2" "$3"
        ;;

    list-backups)
        ls -la "${BACKUP_ROOT:-/srv/confhub-backups}"/confhub-*.tar.gz* 2>/dev/null || log_warn "No backups found"
        ;;

    setup)
        log_info "Setting up monitoring infrastructure..."

        mkdir -p "${BACKUP_ROOT:-/srv/confhub-backups}"

        if [[ -f "${MONITORING_DIR}/logs/logrotate.conf" ]]; then
            sudo cp "${MONITORING_DIR}/logs/logrotate.conf" /etc/logrotate.d/confhub || log_warn "Could not install logrotate config"
        fi

        if [[ ! -f "$ENV_FILE" ]]; then
            cp "${MONITORING_DIR}/.env.example" "$ENV_FILE"
            log_info "Created .env file, please configure it"
        fi

        log_info "Setup complete!"
        ;;

    help|*)
        echo "Monitoring Management Script"
        echo ""
        echo "Usage: $0 <command>"
        echo ""
        echo "Commands:"
        echo "  start              Start monitoring stack"
        echo "  stop               Stop monitoring stack"
        echo "  restart            Restart monitoring stack"
        echo "  status             Show status of monitoring services"
        echo "  logs [service]     Show logs (optional: service name)"
        echo "  logs-follow [svc]  Follow logs (optional: service name)"
        echo "  backup             Run backup"
        echo "  restore --check|--verify-db <path>  Validate an archive or restore PostgreSQL to a temporary database"
        echo "  list-backups       List available backups"
        echo "  setup              Initial setup (run once)"
        echo "  help               Show this help"
        ;;
esac
