import html
import logging
import os

import requests
from flask import Flask, request, jsonify

app = Flask(__name__)

logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(name)s - %(levelname)s - %(message)s'
)
logger = logging.getLogger(__name__)

TELEGRAM_BOT_TOKEN = os.getenv('TELEGRAM_BOT_TOKEN', '')
TELEGRAM_CHAT_ID = os.getenv('TELEGRAM_CHAT_ID', '')


def format_alert_message(alert):
    """Builds a Telegram HTML message; every interpolated value is escaped."""
    status = alert.get('status', 'unknown')
    labels = alert.get('labels') or {}
    annotations = alert.get('annotations') or {}

    def esc(value):
        return html.escape(str(value), quote=False)

    severity = str(labels.get('severity', 'unknown'))
    severity_emoji = {
        'critical': '🔴',
        'warning': '🟡',
        'info': '🔵'
    }.get(severity, '⚪')

    message = f"{severity_emoji} <b>{esc(severity.upper())} ALERT</b>\n\n"
    message += f"📛 <b>{esc(labels.get('alertname', 'Unknown'))}</b>\n\n"

    if labels:
        message += "🏷️ <b>Labels:</b>\n"
        for key, value in labels.items():
            if key not in ['alertname', 'severity']:
                message += f"  • {esc(key)}: <code>{esc(value)}</code>\n"
        message += "\n"

    if annotations:
        message += "📝 <b>Description:</b>\n"
        description = annotations.get('description', annotations.get('summary', 'N/A'))
        message += f"<i>{esc(description)}</i>\n"

    if status == 'resolved':
        message += "\n✅ <b>Status: RESOLVED</b>"
    else:
        message += "\n⚠️ <b>Status: FIRING</b>"

    return message


def send_telegram_message(text):
    if not TELEGRAM_BOT_TOKEN or not TELEGRAM_CHAT_ID:
        logger.error("Telegram bot token or chat ID not configured")
        return False

    url = f"https://api.telegram.org/bot{TELEGRAM_BOT_TOKEN}/sendMessage"
    data = {
        'chat_id': TELEGRAM_CHAT_ID,
        'text': text,
        'parse_mode': 'HTML'
    }

    try:
        response = requests.post(url, json=data, timeout=10)
        if response.status_code == 200:
            logger.info("Alert sent to Telegram successfully")
            return True
        else:
            logger.error("Failed to send Telegram message: HTTP %s %s",
                         response.status_code, response.text[:500])
            return False
    except requests.RequestException as e:
        # The exception text contains the request URL with the bot token.
        logger.error("Error sending Telegram message: %s", type(e).__name__)
        return False


@app.route('/alerts', methods=['POST'])
def receive_alerts():
    try:
        data = request.get_json()
        if not data:
            return jsonify({'error': 'No data provided'}), 400

        alerts = data.get('alerts', [])
        logger.info("Received %d alert(s), status %s", len(alerts), data.get('status', ''))

        if not alerts and 'commonAnnotations' in data:
            alerts = [{'labels': data.get('commonLabels', {}),
                       'annotations': data.get('commonAnnotations', {}),
                       'status': data.get('status', '')}]

        for alert in alerts:
            message = format_alert_message(alert)
            send_telegram_message(message)

        return jsonify({'status': 'ok', 'alerts_processed': len(alerts)})

    except Exception:
        logger.exception("Error processing alert")
        return jsonify({'error': 'internal error'}), 500


@app.route('/health', methods=['GET'])
def health_check():
    return jsonify({
        'status': 'healthy',
        'telegram_configured': bool(TELEGRAM_BOT_TOKEN and TELEGRAM_CHAT_ID)
    })


@app.route('/', methods=['GET'])
def root():
    return jsonify({
        'service': 'Telegram Alert Bot',
        'version': '1.0.0',
        'endpoints': ['/alerts', '/health']
    })


if __name__ == '__main__':
    # Local debugging only; the container runs gunicorn.
    port = int(os.getenv('PORT', 8080))
    app.run(host='0.0.0.0', port=port)
