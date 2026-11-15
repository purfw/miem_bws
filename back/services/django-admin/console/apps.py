from django.apps import AppConfig


class ConsoleConfig(AppConfig):
    default_auto_field = "django.db.models.BigAutoField"
    name = "console"

    def ready(self):
        from .credentials import get_credentials

        get_credentials()
