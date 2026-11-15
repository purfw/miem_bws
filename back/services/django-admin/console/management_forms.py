from django import forms


ADMIN_ROLES = (
    ("moderator", "Модератор — проверка заявок и организаторов"),
    ("operator", "Оператор — просмотр и повтор фоновых задач"),
    ("auditor", "Аудитор — просмотр и журнал действий"),
    ("superadmin", "Администратор — все права и управление командой"),
)

USER_ROLES = (
    ("user", "Участник"),
    ("verified_organizer", "Проверенный организатор"),
    ("organizer_member", "Участник команды организатора"),
    ("admin", "Администратор платформы"),
)


class InvitationForm(forms.Form):
    username = forms.RegexField(
        r"^[A-Za-z0-9_.@-]{1,80}$",
        label="Логин в админке",
        max_length=80,
        help_text=(
            "Можно использовать email пользователя. До 80 символов: "
            "латиница, цифры, точка, @, дефис и подчёркивание."
        ),
    )
    role = forms.ChoiceField(
        label="Права доступа", choices=ADMIN_ROLES, initial="moderator"
    )


class TeamAccessForm(forms.Form):
    roles = forms.MultipleChoiceField(
        label="Права доступа",
        choices=ADMIN_ROLES,
        widget=forms.CheckboxSelectMultiple,
    )
    version = forms.UUIDField(widget=forms.HiddenInput)


class ActivationForm(forms.Form):
    password = forms.CharField(
        label="Новый пароль",
        min_length=12,
        max_length=256,
        widget=forms.PasswordInput(attrs={"autocomplete": "new-password"}),
    )
    password_confirm = forms.CharField(
        label="Повторите пароль",
        max_length=256,
        widget=forms.PasswordInput(attrs={"autocomplete": "new-password"}),
    )
    otp = forms.RegexField(
        r"^[0-9]{6}$",
        label="Код из приложения-аутентификатора",
        widget=forms.TextInput(
            attrs={
                "inputmode": "numeric",
                "autocomplete": "one-time-code",
                "maxlength": 6,
            }
        ),
    )

    def clean(self):
        data = super().clean()
        if data.get("password") and data.get("password") != data.get(
            "password_confirm"
        ):
            self.add_error("password_confirm", "Пароли не совпадают")
        return data


class UserRoleForm(forms.Form):
    user_id = forms.UUIDField(widget=forms.HiddenInput)
    role = forms.ChoiceField(label="Роль на платформе", choices=USER_ROLES)
