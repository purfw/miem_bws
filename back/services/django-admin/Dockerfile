FROM python:3.12-alpine AS runtime

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PIP_NO_CACHE_DIR=1

WORKDIR /app

RUN apk add --no-cache libpq wget

COPY requirements.txt /tmp/requirements.txt
RUN pip install --upgrade pip && pip install -r /tmp/requirements.txt

RUN addgroup -S app && adduser -S app -G app

COPY . /app
USER app:app

EXPOSE 8000
CMD ["gunicorn", "conf_admin.wsgi:application", "--bind", "0.0.0.0:8000", "--workers", "2", "--timeout", "60"]
