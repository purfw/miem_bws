# Развёртывание ConfHub

Рабочий стенд: `https://91.132.58.253/`. Приложение установлено в
`/srv/confhub`; системный nginx завершает TLS и проксирует запросы в
Compose-шлюз на `127.0.0.1:18080`.

## Состав

Один адрес на 443 обслуживает и приложение, и API; с 80 выполняется редирект:

```
https://91.132.58.253/            → фронтенд (front/, Vite/React)
https://91.132.58.253/api/v1/…    → четыре Go-сервиса
```

PostgreSQL, Redis (кеш и отдельный Redis state), NATS JetStream, MinIO,
Elasticsearch 8 и Mailpit наружу не публикуются и доступны только в сегментах
Docker-сети (см. [NETWORK.md](NETWORK.md)). Консоль администратора Django
на внешнем nginx закрыта: публичный `/admin/` возвращает 404, доступ — через
SSH-туннель к loopback-шлюзу.

Внутренние API сервисов слушают TLS 1.3 на порту 8443, требуют клиентский
сертификат собственного CA и HMAC-подпись запроса. HTTP-порты `/internal/*`
не принимают. Сессии, refresh-токены и nonce хранятся в Redis state с AOF и
`noeviction`, отдельно от вытесняемого кеша каталога.

У сервера нет домена, только IP. Let's Encrypt выдаёт для публичных IP
сертификаты профиля `shortlived` сроком 6 дней. Сертификат выпускает Certbot
в Docker, `confhub-certbot-renew.timer` проверяет продление дважды в сутки.
Самоподписанный сертификат в `back/secrets/certs` остаётся аварийным резервом.

## Способ 1. Вручную на сервере

Образы собираются на самой машине, реестр не нужен.

```bash
git clone <репозиторий> confhub && cd confhub/back
PUBLIC_HOST=91.132.58.253 ./scripts/deploy.sh
```

`deploy.sh` идемпотентен. На первом запуске он создаёт:

* `secrets/jwt_ed25519.{key,pub}` — ключи подписи JWT;
* `secrets/certs/{fullchain,privkey}.pem` — временный самоподписанный TLS с IP в SAN;
* `.env.prod` — случайные пароли PostgreSQL, служебных ролей, MinIO,
  `SERVICE_TOKEN_SECRET` и ключ закрытых ссылок;
* `secrets/admin-auth/credentials.enc` и отдельный `secrets/admin-auth/key` —
  зашифрованное хранилище хешей паролей и TOTP-секретов администраторов.

Затем собирает образы и поднимает стек из `docker-compose.prod.yml` и
`docker-compose.build.yml`, применяет межсетевой экран и ставит
systemd-таймеры. С `CONFHUB_HA=1` подключается отказоустойчивый контур
(`docker-compose.ha.yml`, см. [RELIABILITY.md](RELIABILITY.md)).

Первый запуск требует интерактивного терминала: пароль администратора
вводится скрыто, `otpauth://` URI показывается один раз, после добавления в
приложение-аутентификатор нужно ввести текущий шестизначный код. URI не
сохраняется в файлы; не копируйте его в тикеты и логи и не запускайте setup
из CI с записью терминала.

Если в `.env.prod` остались переменные `ADMIN_*` от старой установки, скрипт
переносит их в зашифрованное хранилище и удаляет из `.env.prod`. Пароль и
TOTP-секрет при этом не меняются — при подозрении на утечку выполните
`admin-auth.sh setup` (ниже).

Ключ и шифротекст монтируются в Django только на чтение. Это защищает от
случайного чтения файла, но не от root-доступа или копии, где есть и ключ, и
шифротекст. Остальные сервисные секреты (PostgreSQL, SMTP и др.) хранятся в
`.env.prod` с правами `0600`; внешнего secret manager нет.

Проверка:

```bash
curl -sS -o /dev/null -w '%{http_code}\n' https://91.132.58.253/healthz/conference
curl -sS https://91.132.58.253/api/v1/conferences | head -c 200
```

В межсетевом экране хоста должны быть открыты 443 и 80 (для редиректа).

### Системный nginx на хосте

Если 80/443 уже заняты системным nginx, Compose-шлюз публикуется только на
loopback, а TLS завершает системный nginx:

```bash
PUBLIC_HOST=91.132.58.253 HOST_PROXY_PORT=18080 ./scripts/deploy.sh
cp nginx/confhub.host.conf.example /etc/nginx/conf.d/confhub.conf
nginx -t && systemctl reload nginx
```

Compose в этом режиме не занимает внешние порты и не затрагивает другие
виртуальные хосты. `confhub.host.conf.example` используется до выпуска
сертификата, после — `confhub.host.letsencrypt.conf.example`. Unit-файлы и
таймеры лежат в `back/systemd/`.

### Консоль администратора

С компьютера администратора:

```bash
ssh -L 18081:127.0.0.1:18080 root@91.132.58.253
```

Не закрывая SSH, откройте `http://127.0.0.1:18081/admin/` и войдите своим
логином, паролем и кодом из аутентификатора. После входа `/admin/`
перенаправляет в первый доступный роли раздел.

| URL | Раздел | Просмотр | Действия |
| --- | --- | --- | --- |
| `/admin/verifications/` | Полномочия организаторов | Все четыре роли | `superadmin`, `moderator` |
| `/admin/applications/` | Заявки на доклады | Все четыре роли | `superadmin`, `moderator` |
| `/admin/accounts/` | Учётные записи по UUID | `superadmin` | Блокировка и разблокировка |
| `/admin/queues/` | Фоновые задачи | `superadmin`, `operator` | Повтор неудачного задания |
| `/admin/audit/` | Журнал действий | `superadmin`, `moderator`, `auditor` | Только просмотр |
| `/admin/users/` | Поиск пользователей и роли платформы | `superadmin` | Роли, блокировка и разблокировка |
| `/admin/team/` | Команда консоли | `superadmin` | Приглашения, роли и статус сотрудников |

Списки листаются параметром `?page=`. Заявки фильтруются
`?status=submitted,under_review,revision_requested`; без фильтра показаны все
три состояния. После действия сохраняются страница и фильтр; возврат
выполняется только в заранее заданный раздел, произвольный `next` не
принимается. Недоступные роли разделы скрыты в навигации, прямой запрос к ним
получает 403. Все изменения — POST-запросы с CSRF. Журнал читается по 50
записей через роль `django_reader`; напрямую в БД консоль не пишет.

Роль пользователя на платформе доступа к консоли не даёт. Сотрудники
консоли и их роли (`superadmin`, `moderator`, `operator`, `auditor`) ведутся
отдельно. Последнего активного `superadmin` нельзя отключить или лишить
этой роли. Изменения хранилища подхватываются без перезапуска, сессии
изменённой учётной записи отзываются. Входы и отказы пишутся в аудит без
пароля и кода.

Управление из командной строки на сервере:

```bash
cd /srv/confhub/back
bash scripts/admin-auth.sh add .env.prod --username moderator --roles moderator
bash scripts/admin-auth.sh list .env.prod
bash scripts/admin-auth.sh role .env.prod --username moderator --roles auditor
bash scripts/admin-auth.sh disable .env.prod --username moderator
bash scripts/admin-auth.sh setup .env.prod      # новый пароль и TOTP, отзыв сессий
```

После `setup` пересоздайте контейнер Django:

```bash
HOST_PROXY_PORT=18080 docker compose -p confhub --env-file .env.prod \
  -f docker-compose.prod.yml -f docker-compose.build.yml \
  -f docker-compose.host.yml up -d --build --force-recreate django-admin
```

Перед сменой учётных данных убедитесь, что есть резервный SSH-доступ.

В разделе «Команда консоли» `superadmin` создаёт приглашение. Оно действует
20 минут и используется один раз; почта для него не нужна. Сотрудник
открывает ссылку через свой SSH-туннель, задаёт пароль и подключает TOTP.

Для записи в хранилище контейнер Django получает доступ к
`secrets/admin-auth`: владелец root, группа 0, каталог `2770`, `.lock` —
`0660`, ключ и данные — `0640`. Скрипты настройки поддерживают эти права;
на старой установке их выставляют вручную:

```bash
chgrp 0 secrets/admin-auth secrets/admin-auth/.lock
chmod 2770 secrets/admin-auth
chmod 660 secrets/admin-auth/.lock
```

### Наполнение каталога

Публичный каталог пуст, пока не создана конференция. Конференцию создаёт
внутренний API. Он недоступен снаружи, поэтому
запрос выполняется на сервере и подписывается `X-Service-Token`:

```bash
cd /srv/confhub/back
python3 scripts/internal-call.py POST "https://conference-svc:8443/internal/conferences" < conf.json
```

Скрипт берёт HMAC-секрет из `.env.prod` и клиентский сертификат из
`secrets/mtls`; секрет не передаётся в аргументах процесса. Поля `conf.json`
соответствуют `createReq` в
`services/conference-svc/internal/handler/internal.go`; обязательные —
`title`, `slug`, `start_date`, `end_date`, `created_by`. Дата начала должна
быть не раньше 01.01.2027. Конференция создаётся в статусе `draft`;
публикация:

```bash
echo '{"status":"registration_open"}' | \
  python3 scripts/internal-call.py PATCH "https://conference-svc:8443/internal/conferences/<id>"
```

### Почта и ORCID

Без внешнего SMTP письма копятся в очереди и доставляются только в Mailpit —
пользователи не получат ссылки подтверждения email и сброса пароля. Для
реальной доставки в `.env.prod` задаются `SMTP_HOST`, `SMTP_PORT`,
`SMTP_USER`, `SMTP_PASSWORD`, `SMTP_FROM`, `SMTP_TLS_MODE` и пересоздаётся
`engagement-svc` (теми же Compose-файлами и проектом, что при обновлении).
Для порта 587 обычно `starttls`, для 465 — `implicit`; `none` допустим только
для внутреннего SMTP без аутентификации.

Пример для Яндекс Почты: `smtp.yandex.ru:465`, `SMTP_TLS_MODE=implicit`,
`SMTP_USER` и `SMTP_FROM` — адрес ящика, `SMTP_PASSWORD` — пароль приложения
типа «Почта», а не пароль Яндекс ID
([инструкция](https://yandex.ru/support/yandex-360/customers/mail/ru/mail-clients/others)).

ORCID включается, когда заданы `ORCID_CLIENT_ID` и `ORCID_CLIENT_SECRET`.
В кабинете ORCID Public API Client регистрируется redirect URI
`https://91.132.58.253/api/v1/auth/orcid/callback`. Без этих переменных
кнопка ORCID скрыта, вход по паролю работает.

### Регламентные задания

`deploy.sh` устанавливает systemd-таймеры:

| Таймер | Что делает |
| --- | --- |
| `confhub-backup.timer` | Ежедневно в 03:00 МСК — полная зашифрованная копия стенда (`scripts/backup.sh`) |
| `confhub-certbot-renew.timer` | Дважды в сутки — проверка и продление IP-сертификата |
| `confhub-audit-maintenance.timer` | Подготовка месячных партиций аудита; записи старше 12 месяцев переносятся в архив MinIO без payload и идентификаторов |
| `confhub-weekly-review.timer` | Еженедельный отчёт: аномальная активность, недоставленные сообщения, outbox, диск, долгие SQL-запросы |
| `confhub-key-rotation.timer` | Ежеквартально — замена ключей JWT и закрытых ссылок с сохранением keyring и автоматическим откатом |
| `confhub-service-keys.timer` | Ежемесячно — замена межсервисного HMAC, заблаговременное обновление mTLS-сертификатов |
| `confhub-firewall.service` | Восстановление правил межсетевого экрана после перезагрузки |

### Резервное копирование

`scripts/backup.sh` кратко останавливает пишущие сервисы и сохраняет дамп
PostgreSQL, данные MinIO, NATS и Redis state, используемые Docker-образы,
конфигурацию compose и nginx (включая системный), Let's Encrypt и секреты.
Сервисы запускаются сразу после снимка, до упаковки и шифрования. Архив
шифруется age и кладётся в `/srv/confhub-backups` (каталог `0700`, архивы
`0600`), срок хранения — 14 дней. Elasticsearch в копию не входит: индекс
перестраивается из PostgreSQL.

Закрытый ключ age лежит в `/srv/confhub-backup-keys/identity.key` и в архив не
входит. Его нужно хранить и вне сервера: без ключа архивы не расшифровать.
Копии хранятся на том же сервере и от потери сервера не защищают.

Проверка архива:

```bash
bash scripts/restore.sh --check        /srv/confhub-backups/confhub-<timestamp>.tar.gz.age
bash scripts/restore.sh --verify-db    /srv/confhub-backups/confhub-<timestamp>.tar.gz.age
bash scripts/restore.sh --verify-stack /srv/confhub-backups/confhub-<timestamp>.tar.gz.age
```

`--check` проверяет структуру архива. `--verify-db` восстанавливает PostgreSQL
во временную базу и удаляет только её. `--verify-stack` поднимает отдельный
Compose-проект с собственными томами, без внешних портов и выхода в интернет,
проверяет готовность сервисов и публичные и внутренние API, затем удаляет
этот проект. Рабочие БД и тома не затрагиваются: это проверка
восстановимости, а не аварийное переключение.

### API-документация и трассировка

Схемы OpenAPI строятся из работающих роутеров:
`/api/v1/openapi/identity.json`, `conference.json`, `application.json`,
`engagement.json`. Swagger UI — страница `/docs`. Маршрут без описания
отклоняется проверкой при старте сервиса и в тестах. Внутренние методы
описаны, но наружу не публикуются.

Jaeger доступен только на loopback-порту 16686:

```bash
ssh -L 16686:127.0.0.1:16686 root@91.132.58.253
```

Откройте `http://127.0.0.1:16686/`. HTTP-запросы и основные обработчики NATS
передают W3C trace context. В атрибутах используются шаблоны маршрутов, без
тел запросов, query string и токенов. Трассы хранятся 48 часов, выборка
корневых запросов — 10 %.

### Тесты Django

Не запускайте pytest через `docker exec` в рабочем контейнере Django: тесты
очищают кеш, а в рабочем образе нет тестовых зависимостей. Используйте
отдельный контейнер с `pip install -r requirements-dev.txt`, без `.env.prod` и адресов
рабочих сервисов, с собственным Redis и
`TEST_REDIS_URL=redis://confhub-test-redis-<уникальное-имя>:6379/0`.
Тестовый bootstrap отвергает рабочие настройки и адреса вне тестового
пространства имён.

### Обновление

```bash
git pull && PUBLIC_HOST=91.132.58.253 HOST_PROXY_PORT=18080 ./scripts/deploy.sh
```

Миграции применяются сервисами при старте. Повторный `deploy.sh` надёжнее
ручного `docker compose up`: все команды должны использовать проект
`confhub`, `.env.prod` и одинаковый набор Compose-файлов. Шлюз разрешает
имена через Docker DNS, поэтому пересоздание контейнера фронтенда или API
не требует перезапуска nginx. Если файл конфигурации nginx заменён через
rsync, шлюз нужно пересоздать: bind mount остаётся на прежнем inode, и
reload его не видит.

## Способ 2. Через CI

`.sourcecraft/ci.yaml` выполняет сборку, линтеры, тесты и сканы; публикации
образов и деплоя в нём нет. Для автоматического деплоя нужны реестр образов,
SSH-ключ и адрес сервера в секретах CI, а шаг деплоя должен вызывать
`scripts/deploy.sh` с тем же набором Compose-файлов, что и при ручном
развёртывании. До этого деплой выполняется способом 1.

## Ограничения

* Внешнего secret manager и резервных кодов для 2FA администраторов нет.
* Резервные копии хранятся на том же сервере; внешней копии архивов и
  ключа нет.
* Каналы до PostgreSQL, Redis, NATS, MinIO и Elasticsearch не шифруются,
  доступ к ним ограничен сегментами сети и межсетевым экраном.
* Все узлы, включая кластеры PostgreSQL и NATS, работают на одном сервере.
