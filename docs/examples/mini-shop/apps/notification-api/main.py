import json
import os
import sqlite3
import time
import urllib.error
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

DATABASE_PATH = os.environ.get("DATABASE_PATH", "mini_shop_notifications.db")


def write_json(payload, status=HTTPStatus.OK):
    body = json.dumps(payload, ensure_ascii=False).encode("utf-8")
    return [("Content-Type", "application/json; charset=utf-8"), ("Cache-Control", "no-store")], body


def json_response(handler, status, headers, body):
    handler.send_response(status)
    for key, value in headers:
        handler.send_header(key, value)
    handler.send_header("Content-Length", str(len(body)))
    handler.send_header("Access-Control-Allow-Origin", "*")
    handler.end_headers()
    handler.wfile.write(body)


def init_db() -> None:
    conn = sqlite3.connect(DATABASE_PATH)
    conn.execute(
        """
        CREATE TABLE IF NOT EXISTS notifications (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            customer_id INTEGER NOT NULL,
            order_id INTEGER NOT NULL,
            title TEXT NOT NULL,
            message TEXT NOT NULL,
            created_at INTEGER NOT NULL
        )
        """
    )
    conn.commit()
    conn.close()


def write_notification(customer_id: int, order_id: int, title: str, message: str) -> None:
    conn = sqlite3.connect(DATABASE_PATH)
    now = int(time.time())
    conn.execute(
        "INSERT INTO notifications (customer_id, order_id, title, message, created_at) VALUES (?, ?, ?, ?, ?)",
        (customer_id, order_id, title, message, now),
    )
    conn.commit()
    conn.close()


def load_notifications(limit: int = 20):
    conn = sqlite3.connect(DATABASE_PATH)
    cursor = conn.execute(
        "SELECT id, customer_id, order_id, title, message, created_at FROM notifications ORDER BY id DESC LIMIT ?",
        (limit,),
    )
    rows = cursor.fetchall()
    conn.close()
    return [
        {
            "id": row[0],
            "customer_id": row[1],
            "order_id": row[2],
            "title": row[3],
            "message": row[4],
            "created_at": row[5],
        }
        for row in rows
    ]


def read_body(handler) -> tuple[dict | None, str | None]:
    size = int(handler.headers.get("Content-Length", "0"))
    raw = handler.rfile.read(size)
    if not raw:
        return None, "invalid_payload"

    try:
        payload = json.loads(raw)
        return payload, None
    except (TypeError, json.JSONDecodeError, ValueError):
        return None, "invalid_payload"


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def do_OPTIONS(self):
        self.send_response(HTTPStatus.NO_CONTENT)
        self.send_header("Access-Control-Allow-Origin", "*")
        self.send_header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
        self.send_header("Access-Control-Allow-Headers", "Content-Type")
        self.end_headers()

    def do_GET(self):
        if self.path == "/health":
            payload = {"service": "notification-api", "status": "ok"}
            headers, body = write_json(payload)
            json_response(self, HTTPStatus.OK, headers, body)
            return

        if self.path == "/notifications":
            payload = {"notifications": load_notifications()}
            headers, body = write_json(payload)
            json_response(self, HTTPStatus.OK, headers, body)
            return

        payload = {"code": "not_found", "message": "route not found"}
        headers, body = write_json(payload, HTTPStatus.NOT_FOUND)
        json_response(self, HTTPStatus.NOT_FOUND, headers, body)

    def do_POST(self):
        if self.path != "/notifications":
            payload = {"code": "not_found", "message": "route not found"}
            headers, body = write_json(payload, HTTPStatus.NOT_FOUND)
            json_response(self, HTTPStatus.NOT_FOUND, headers, body)
            return

        payload, err = read_body(self)
        if err:
            headers, body = write_json({"code": err, "message": "invalid payload"}, HTTPStatus.BAD_REQUEST)
            json_response(self, HTTPStatus.BAD_REQUEST, headers, body)
            return

        try:
            customer_id = int(payload["customer_id"])
            order_id = int(payload["order_id"])
            title = str(payload.get("title", "event"))
            message = str(payload.get("message", ""))
        except (KeyError, TypeError, ValueError):
            headers, body = write_json({"code": "invalid_payload", "message": "invalid payload"}, HTTPStatus.BAD_REQUEST)
            json_response(self, HTTPStatus.BAD_REQUEST, headers, body)
            return

        write_notification(customer_id=customer_id, order_id=order_id, title=title, message=message)
        result = {"code": "notification_recorded", "status": "ok"}
        headers, body = write_json(result, HTTPStatus.CREATED)
        json_response(self, HTTPStatus.CREATED, headers, body)

    def log_message(self, fmt, *args):
        return


def main() -> None:
    init_db()
    server = ThreadingHTTPServer(("0.0.0.0", 3009), Handler)
    server.serve_forever()


if __name__ == "__main__":
    main()
