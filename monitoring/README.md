# Мониторинг ConfHub

Отдельный Compose-проект `monitoring`: метрики, журналы, алерты в Telegram и
обёртки для резервного копирования. Запускается после основного проекта
`confhub` и подключается к его сегменту `confhub_app`.

## Компоненты

| Сервис | Порт на хосте | Назначение |
| --- | --- | --- |
| Prometheus | 127.0.0.1:9090 | Сбор метрик и правила алертов (`prometheus/alerts.yml`) |
| Alertmanager | 127.0.0.1:9093 | Группировка алертов, отправка в `alert-bot` |
| alert-bot | 127.0.0.1:8082 | Пересылка алертов Alertmanager в Telegram |
| Grafana | 127.0.0.1:3000 | Дашборды; источники данных и дашборды загружаются из `grafana/provisioning` |
| Loki | 127.0.0.1:3100 | Хранение журналов |
| Promtail | — | Сбор журналов Docker-контейнеров в Loki |
| Node Exporter | 127.0.0.1:9100 | Метрики хоста |
| cAdvisor | 127.0.0.1:8081 | Метрики контейнеров |

Все порты опубликованы только на loopback. С компьютера администратора
интерфейсы открываются через SSH-туннель, например для Grafana:

```bash
ssh -L 3000:127.0.0.1:3000 root@<адрес-сервера>
```

Prometheus опрашивает `/metrics` четырёх API (`identity-svc`,
`conference-svc`, `application-svc`, `engagement-svc`) по сегменту
`confhub_app`. Его адрес в этом сегменте статический, а доступ к портам
метрик разрешён правилами `back/firewall/policy.conf`.

## Настройка и запуск

```bash
cd /srv/confhub/monitoring
./manage_monitoring.sh setup     # создаёт .env из .env.example, ставит logrotate
$EDITOR .env                     # GRAFANA_PASSWORD, TELEGRAM_BOT_TOKEN, TELEGRAM_CHAT_ID
./manage_monitoring.sh start
```

| Переменная | Назначение |
| --- | --- |
| `GRAFANA_USER`, `GRAFANA_PASSWORD` | Учётная запись администратора Grafana; без пароля стек не запустится |
| `TELEGRAM_BOT_TOKEN` | Токен бота от @BotFather |
| `TELEGRAM_CHAT_ID` | Чат, куда бот пишет алерты (бот должен получить в нём `/start`) |
| `APPLICATION_NETWORK` | Сеть основного проекта, по умолчанию `confhub_app` |

## Команды

```bash
./manage_monitoring.sh start | stop | restart | status
./manage_monitoring.sh logs [сервис]
./manage_monitoring.sh logs-follow [сервис]
./manage_monitoring.sh backup
./manage_monitoring.sh list-backups
./manage_monitoring.sh restore --check     /srv/confhub-backups/confhub-<timestamp>.tar.gz.age
./manage_monitoring.sh restore --verify-db /srv/confhub-backups/confhub-<timestamp>.tar.gz.age
```

`backup` и `restore` — обёртки над `back/scripts/backup.sh` и
`back/scripts/restore.sh`. Регулярную копию делает `confhub-backup.timer`
(ежедневно в 03:00 МСК), его устанавливает `back/scripts/deploy.sh`. Формат
архивов, шифрование и срок хранения описаны в
[docs/RELIABILITY.md](../docs/RELIABILITY.md).

## Алерты

| Группа | Алерты |
| --- | --- |
| Контейнеры | `ContainerDown`, `ContainerRestartLoop`, `HighCPUUsage`, `HighMemoryUsage` |
| Хост | `HighSystemCPU`, `HighSystemMemory`, `HighDiskUsage` (свободно < 20 %), `DiskAlmostFull` (< 10 %) |
| Сервисы | `TargetDown`, `HighRequestLatency` (p95 > 5 с) |
| Очереди (outbox, почта, выдача ролей) | `QueueCollectionFailed`, `QueueItemParked`, `QueueDeliveryStalled` |
| Журналы | `LokiNotIngesting`, `LokiDroppedEntries` |

Алерты уровня `critical` отправляются сразу и повторяются раз в час,
`warning` — с задержкой 30 с и повтором раз в 4 часа. При активном
`critical` одноимённый `warning` для того же экземпляра подавляется.

## Дашборды

| Дашборд | Содержание |
| --- | --- |
| Container & System Metrics | Хост и контейнеры: CPU, память, диск, сеть |
| Application Services Dashboard | Состояние, CPU и память четырёх API; их журналы и ошибки из Loki |
| ConfHub HTTP and delivery | Запросы в секунду, p95 длительности, незавершённые элементы очередей и возраст самого старого |

## Журналы

Promtail читает журналы Docker-контейнеров и отправляет их в Loki.
`logs/logrotate.conf` (ставится командой `setup` в `/etc/logrotate.d/confhub`)
ограничивает размер json-журналов Docker на хосте.

## Диагностика

* Состояние целей Prometheus: `curl -s http://127.0.0.1:9090/api/v1/targets`.
  Если API в состоянии `down`, проверьте, что основной проект запущен и
  правила экрана применены (`sudo back/scripts/firewall.sh test --summary`).
* Алерты не приходят в Telegram: `curl -s http://127.0.0.1:8082/health`
  показывает, заданы ли токен и чат; журнал — `docker logs alert-bot`.
