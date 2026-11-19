# ConfHub

Платформа для научных конференций: каталог и поиск конференций, регистрация
участников, подача и рецензирование заявок на доклады, уведомления. Проект
курса «Безопасность вычислительных систем».

## Компоненты

| Компонент | Технология | Назначение |
| --- | --- | --- |
| `front` | React, Vite, nginx | Веб-интерфейс |
| `nginx` | nginx | Шлюз: статика и маршрутизация `/api/v1` |
| `identity-svc` | Go | Учётные записи, вход, JWT, роли |
| `conference-svc` | Go | Конференции, расписание, регистрации, поиск |
| `application-svc` | Go | Заявки на доклады, файлы, документы участия |
| `engagement-svc` | Go | Уведомления, почта, журнал действий |
| `django-admin` | Python, Django | Консоль администратора |
| PostgreSQL 16 | Patroni + etcd | База данных, отдельная схема на сервис |
| Redis | два экземпляра | Кеш каталога; сессии и токены |
| NATS JetStream | кластер из 3 узлов | Брокер событий между сервисами |
| MinIO, Elasticsearch | | Файлы заявок; полнотекстовый поиск |
| Prometheus, Grafana, Loki, Jaeger | | Метрики, журналы, трассировка |

Сервисы общаются событиями через NATS (шаблон outbox) и по внутреннему API с
взаимной проверкой сертификатов (mTLS).

## Запуск

Локально, для разработки:

```bash
cd back
make secrets
make up            # http://localhost:8080
```

На сервере:

```bash
cd back
PUBLIC_HOST=<адрес> ./scripts/deploy.sh               # один узел БД и брокера
CONFHUB_HA=1 PUBLIC_HOST=<адрес> ./scripts/deploy.sh  # с отказоустойчивым контуром
```

Стенд для демонстрации отказов, рядом с рабочим и независимо от него:

```bash
cd back
sudo ./scripts/lab.sh up       # http://127.0.0.1:18081
```

## Документация

| Тема | Файл |
| --- | --- |
| Развёртывание | [docs/DEPLOY.md](docs/DEPLOY.md) |
| Сегменты сети и межсетевой экран | [docs/NETWORK.md](docs/NETWORK.md) |
| Отказоустойчивость, копии, MTBF/MTTR/SLO | [docs/RELIABILITY.md](docs/RELIABILITY.md) |
| Схема базы данных | [back/db/schema.sql](back/db/schema.sql) |
| API | Swagger UI на `/docs`; схемы строятся из роутеров сервисов: `/api/v1/openapi/{identity,conference,application,engagement}.json` |
| Мониторинг | [monitoring/README.md](monitoring/README.md) |

## Сценарии для защиты

Каждый сценарий — готовый скрипт в `back/scripts/demo/` (или команда
`back/scripts/firewall.sh`); запускаются из каталога `back/`. Скрипты, которые
только читают состояние, показываются на рабочем стенде. Сценарии отказов
выполняются на стенде `confhub-lab`, чтобы не ронять рабочий.

| Этап | Что показать | Команда |
| --- | --- | --- |
| 2 | Контейнеры, БД, кеш, брокер, ответы сервисов | `COMPOSE_PROJECT=confhub ./scripts/demo/stack-status.sh` |
| 3 | insert, select, update, delete через приложение | `sudo ./scripts/demo/database-crud.sh` |
| 3 | Запись в кеш и чтение из кеша | `COMPOSE_PROJECT=confhub ./scripts/demo/cache.sh` |
| 3 | Публикация и потребление сообщения | `sudo ./scripts/demo/broker.sh` |
| 4 | Сегменты сети и их изоляция | `sudo COMPOSE_PROJECT=confhub ./scripts/demo/segments.sh` |
| 4 | Все правила межсетевого экрана | `sudo ./scripts/firewall.sh rules` (вывод nft: `show`) |
| 4 | Разрешённое проходит, запрещённое нет | `sudo ./scripts/firewall.sh test --summary` (по строке на соединение: `test`) |
| 4 | Ошибки при снятии нужного правила | `sudo ./scripts/demo/firewall-revoke.sh` |
| 5 | Состояние кластеров PostgreSQL, etcd, NATS | `COMPOSE_PROJECT=confhub ./scripts/demo/ha-status.sh` |
| 5 | Отказ лидера PostgreSQL | `sudo ./scripts/demo/postgres-failover.sh` |
| 5 | Отказ узла брокера | `sudo ./scripts/demo/nats-failover.sh` |
| 5 | Резервная копия и восстановление | `sudo ./scripts/demo/backup-restore.sh` |

Сценарии без `COMPOSE_PROJECT=confhub` работают со стендом `confhub-lab`. Сценарии,
которые меняют данные или ломают узлы, на рабочем проекте не запускаются.
