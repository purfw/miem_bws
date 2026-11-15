SECTIONS = (
    ("verifications", "Полномочия организаторов", "read"),
    ("applications", "Заявки на доклады", "read"),
    ("accounts", "Учетные записи", "accounts"),
    ("queues", "Фоновые задачи", "operations"),
    ("audit", "Журнал действий", "audit"),
    ("users", "Пользователи", "accounts"),
    ("team", "Команда админки", "manage_admins"),
)

ROLE_LABELS = {
    "superadmin": "Администратор",
    "moderator": "Модератор",
    "operator": "Оператор",
    "auditor": "Аудитор",
}

ACTION_SECTIONS = {
    "moderate_verification": "verifications",
    "verification_document": "verifications",
    "moderate_application": "applications",
    "set_account_active": "accounts",
    "retry_queue": "queues",
    "change_user": "users",
    "invite_admin": "team",
    "change_admin_access": "team",
}
