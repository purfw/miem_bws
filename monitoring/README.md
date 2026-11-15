# Monitoring & Infrastructure Stack

Комплексный набор инструментов для мониторинга, логирования, алертинга и резервного копирования production-приложения.

## Компоненты

### 📊 Мониторинг
- **Prometheus** (порт 9090) - сбор и хранение метрик
- **Node Exporter** (порт 9100) - системные метрики (CPU, RAM, Disk)
- **cAdvisor** (порт 8081) - метрики контейнеров Docker
- **Grafana** (порт 3000) - дашборды и визуализация

### 📝 Логирование
- **Loki** (порт 3100) - агрегатор логов
- **Promtail** - сборщик логов из Docker контейнеров

### 🔔 Алертинг
- **Alertmanager** (порт 9093) - маршрутизация алертов
- **Telegram Bot** - уведомления в Telegram

### 💾 Резервное копирование
- Скрипты для backup PostgreSQL, volumes, конфигов
- Ротация бэкапов
- Архивы шифруются (age) и хранятся локально 14 дней; внешняя выгрузка не настроена

## Быстрый старт

### 1. Первичная настройка

```bash
cd /srv/confhub/monitoring

# Создать .env из примера и заполнить
cp .env.example .env
nano .env
```

Настройте следующие переменные:
- `TELEGRAM_BOT_TOKEN` - токен бота от @BotFather
- `TELEGRAM_CHAT_ID` - ваш chat ID
- `GRAFANA_PASSWORD` - пароль для Grafana

### 2. Запуск мониторинга

```bash
# Запуск всей инфраструктуры мониторинга
./manage_monitoring.sh setup
./manage_monitoring.sh start
```

### 3. Доступ к сервисам

| Сервис | URL | Логин |
|--------|-----|-------|
| Prometheus | http://localhost:9090 | - |
| Grafana | http://localhost:3000 | `GRAFANA_USER` и `GRAFANA_PASSWORD` из `.env` |
| Loki | http://localhost:3100 | - |
| Alertmanager | http://localhost:9093 | - |
| Node Exporter | http://localhost:9100 | - |
| cAdvisor | http://localhost:8081 | - |

## Управление

### Мониторинг

```bash
# Старт/Стоп/Рестарт
./manage_monitoring.sh start
./manage_monitoring.sh stop
./manage_monitoring.sh restart

# Логи
./manage_monitoring.sh logs prometheus
./manage_monitoring.sh logs-follow grafana
```

### Резервное копирование

```bash
# Ручной запуск
./manage_monitoring.sh backup

# Просмотр бэкапов
./manage_monitoring.sh list-backups

# Проверка структуры архива
./manage_monitoring.sh restore --check /srv/confhub-backups/confhub-<timestamp>.tar.gz

# Пробное восстановление PostgreSQL в отдельную временную БД
./manage_monitoring.sh restore --verify-db /srv/confhub-backups/confhub-<timestamp>.tar.gz
```

## Алерты

### Настроенные алерты

1. **ContainerDown** - контейнер упал
2. **HighCPUUsage** - высокая нагрузка CPU (>80%)
3. **HighMemoryUsage** - высокая нагрузка памяти (>80%)
4. **HighDiskUsage** - диск заполняется (<20%)
5. **DiskAlmostFull** - диск почти полный (<10%)
6. **TargetDown** - сервис недоступен
7. **ContainerRestartLoop** - контейнер перезапускается

### Настройка Telegram

1. Создайте бота через @BotFather
2. Получите токен бота
3. Напишите боту /start
4. Получите chat ID через @userinfobot
5. Добавьте значения в `.env` файл

## Дашборды

### Доступные дашборды

1. **Container & System Metrics** - общие метрики системы и контейнеров
2. **Application Services** - метрики конкретных сервисов (identity-svc, conference-svc и т.д.)

Дашборды автоматически загружаются в Grafana при первом запуске.

## Структура файлов

```
monitoring/
├── docker-compose.monitoring.yml  # Docker Compose для мониторинга
├── prometheus/
│   ├── prometheus.yml             # Конфигурация Prometheus
│   └── alerts.yml                 # Правила алертинга
├── grafana/
│   ├── provisioning/              # Автозагрузка datasources/dashboards
│   └── dashboards/               # JSON дашборды
├── alertmanager/
│   └── alertmanager.yml          # Конфигурация Alertmanager
├── loki/
│   └── config.yml                 # Конфигурация Loki
├── promtail/
│   └── config.yml                 # Конфигурация Promtail
├── alert-bot/
│   ├── bot.py                     # Telegram бот
│   └── requirements.txt
├── backup/
│   ├── backup.sh                  # Скрипт бэкапа
│   └── restore.sh                 # Скрипт восстановления
├── logs/
│   ├── logrotate.conf             # Конфигурация ротации логов
│   └── docker-log-rotate.json    # Конфигурация Docker логов
├── manage_monitoring.sh           # Главный скрипт управления
├── .env.example                  # Пример .env файла
└── README.md                      # Этот файл
```

## Расписание

Резервное копирование запускает `confhub-backup.timer` (ежедневно, ставится
`back/scripts/deploy.sh`). Отдельная запись в crontab не нужна.

## Интеграция с основным приложением

Основное приложение должно быть запущено первым в Compose-проекте `confhub`.
Prometheus подключается к его сегменту `confhub_app` (доступ задаёт `back/firewall/policy.conf`) и опрашивает внутренние
`/metrics` четырёх API. Labels контейнеров сами по себе не настраивают scraping.

```bash
cd /srv/confhub/monitoring
docker compose -p monitoring -f docker-compose.monitoring.yml up -d
```

## Troubleshooting

### Prometheus не собирает метрики

```bash
# Проверьте targets
curl http://localhost:9090/api/v1/targets

# Проверьте логи
./manage_monitoring.sh logs prometheus
```

### Telegram бот не работает

```bash
# Проверьте логи
docker logs alert-bot
```

### Нет данных в Grafana

1. Проверьте datasource (Configuration -> Data Sources)
2. Проверьте правильность URL в provisioning
3. Перезапустите Grafana: `docker restart grafana`

## Безопасность

- Все сервисы слушают только на localhost (127.0.0.1)
- Доступ к мониторингу только через reverse proxy
- Доступ Prometheus к сервисам ограничен правилами `back/firewall/policy.conf`
- Используйте strong пароли в .env файле
