import json
import os
import sqlite3
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import urlparse


DATA_DIR = Path(os.environ.get("DATA_DIR", ".")).resolve()
DB_PATH = DATA_DIR / os.environ.get("DATABASE_PATH", "customers.db")

CUSTOMERS = [
    ("Amy Lin", "amy.lin@example.com"),
    ("David Chen", "david.chen@example.com"),
    ("Sara Wu", "sara.wu@example.com"),
]


def ensure_db() -> None:
    DB_PATH.parent.mkdir(parents=True, exist_ok=True)
    conn = sqlite3.connect(DB_PATH)
    cur = conn.cursor()
    cur.execute(
        """
        CREATE TABLE IF NOT EXISTS customers (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            name TEXT NOT NULL,
            email TEXT NOT NULL
        )
        """
    )
    cur.execute("SELECT COUNT(*) FROM customers")
    if cur.fetchone()[0] == 0:
        cur.executemany("INSERT INTO customers (name, email) VALUES (?, ?)", CUSTOMERS)
        conn.commit()
    conn.close()


def list_customers() -> list[dict]:
    conn = sqlite3.connect(DB_PATH)
    cur = conn.cursor()
    cur.execute("SELECT id, name, email FROM customers ORDER BY id")
    rows = [dict(id=row[0], name=row[1], email=row[2]) for row in cur.fetchall()]
    conn.close()
    return rows


def get_customer(customer_id: int) -> dict | None:
    conn = sqlite3.connect(DB_PATH)
    cur = conn.cursor()
    cur.execute("SELECT id, name, email FROM customers WHERE id = ?", (customer_id,))
    row = cur.fetchone()
    conn.close()
    if not row:
        return None
    return dict(id=row[0], name=row[1], email=row[2])


def db_ready() -> bool:
    try:
        with sqlite3.connect(DB_PATH) as conn:
            conn.execute("PRAGMA quick_check")
        return True
    except sqlite3.DatabaseError:
        return False


def write_json(payload, status=HTTPStatus.OK):
    body = json.dumps(payload, ensure_ascii=False).encode("utf-8")
    return [("Content-Type", "application/json; charset=utf-8")], body


def json_response(handler, status, headers, body):
    handler.send_response(status)
    for key, value in headers:
        handler.send_header(key, value)
    handler.send_header("Content-Length", str(len(body)))
    handler.send_header("Access-Control-Allow-Origin", "*")
    handler.end_headers()
    handler.wfile.write(body)


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def do_OPTIONS(self):
        self.send_response(HTTPStatus.NO_CONTENT)
        self.send_header("Access-Control-Allow-Origin", "*")
        self.send_header("Access-Control-Allow-Methods", "GET, OPTIONS")
        self.send_header("Access-Control-Allow-Headers", "Content-Type")
        self.end_headers()

    def do_GET(self):
        parsed = urlparse(self.path)
        if parsed.path == "/health":
            ready = db_ready()
            payload = {
                "service": "customer-api",
                "status": "ok" if ready else "degraded",
                "dependencies": {
                    "sqlite": {"ready": ready, "path": str(DB_PATH)},
                },
            }
            code = HTTPStatus.OK if payload["status"] == "ok" else HTTPStatus.SERVICE_UNAVAILABLE
            headers, body = write_json(payload)
            json_response(self, code, headers, body)
            return

        if parsed.path == "/customers":
            payload = {"customers": list_customers()}
            headers, body = write_json(payload)
            json_response(self, HTTPStatus.OK, headers, body)
            return

        if parsed.path.startswith("/customers/"):
            try:
                customer_id = int(parsed.path.rsplit("/", 1)[-1])
            except ValueError:
                payload = {"code": "bad_request", "message": "customer_id must be a number"}
                headers, body = write_json(payload, HTTPStatus.BAD_REQUEST)
                json_response(self, HTTPStatus.BAD_REQUEST, headers, body)
                return

            customer = get_customer(customer_id)
            if not customer:
                payload = {"code": "not_found", "message": f"customer {customer_id} not found"}
                headers, body = write_json(payload, HTTPStatus.NOT_FOUND)
                json_response(self, HTTPStatus.NOT_FOUND, headers, body)
                return

            headers, body = write_json(customer)
            json_response(self, HTTPStatus.OK, headers, body)
            return

        payload = {"code": "not_found", "message": "route not found"}
        headers, body = write_json(payload, HTTPStatus.NOT_FOUND)
        json_response(self, HTTPStatus.NOT_FOUND, headers, body)

    def log_message(self, fmt, *args):  # pragma: no cover
        return


def main() -> None:
    ensure_db()
    server = ThreadingHTTPServer(("0.0.0.0", 3004), Handler)
    server.serve_forever()


if __name__ == "__main__":
    main()
