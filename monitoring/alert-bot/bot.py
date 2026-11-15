import os
import json
import logging
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
    status = alert.get('status', 'unknown')
    labels = alert.get('labels', {})
    annotations = alert.get('annotations', {})
    
    severity = labels.get('severity', 'unknown')
    severity_emoji = {
        'critical': '🔴',
        'warning': '🟡',
        'info': '🔵'
    }.get(severity, '⚪')
    
    message = f"{severity_emoji} *{severity.upper()} ALERT*\n\n"
    message += f"📛 *{labels.get('alertname', 'Unknown')}*\n\n"
    
    if labels:
        message += "🏷️ *Labels:*\n"
        for key, value in labels.items():
            if key not in ['alertname', 'severity']:
                message += f"  • {key}: `{value}`\n"
        message += "\n"
    
    if annotations:
        message += "📝 *Description:*\n"
        message += f"_{annotations.get('description', annotations.get('summary', 'N/A'))}_\n"
    
    if status == 'resolved':
        message += f"\n✅ *Status: RESOLVED*"
    else:
        message += f"\n⚠️ *Status: FIRING*"
    
    return message


def send_telegram_message(text):
    if not TELEGRAM_BOT_TOKEN or not TELEGRAM_CHAT_ID:
        logger.error("Telegram bot token or chat ID not configured")
        return False
    
    url = f"https://api.telegram.org/bot{TELEGRAM_BOT_TOKEN}/sendMessage"
    data = {
        'chat_id': TELEGRAM_CHAT_ID,
        'text': text,
        'parse_mode': 'Markdown'
    }
    
    try:
        response = requests.post(url, json=data, timeout=10)
        if response.status_code == 200:
            logger.info("Alert sent to Telegram successfully")
            return True
        else:
            logger.error(f"Failed to send Telegram message: {response.text}")
            return False
    except Exception as e:
        logger.error(f"Error sending Telegram message: {e}")
        return False


@app.route('/alerts', methods=['POST'])
def receive_alerts():
    try:
        data = request.get_json()
        logger.info(f"Received alert payload: {json.dumps(data)[:500]}")
        
        if not data:
            return jsonify({'error': 'No data provided'}), 400
        
        alerts = data.get('alerts', [])
        
        if not alerts and 'commonAnnotations' in data:
            alerts = [{'labels': data.get('commonLabels', {}), 
                       'annotations': data.get('commonAnnotations', {}),
                       'status': data.get('status', '')}]
        
        for alert in alerts:
            message = format_alert_message(alert)
            send_telegram_message(message)
        
        return jsonify({'status': 'ok', 'alerts_processed': len(alerts)})
    
    except Exception as e:
        logger.error(f"Error processing alert: {e}")
        return jsonify({'error': str(e)}), 500


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
    port = int(os.getenv('PORT', 8080))
    app.run(host='0.0.0.0', port=port)
