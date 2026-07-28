import json
import os
import sqlite3
import socket
import urllib.error
import urllib.request
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

DB_PATH = os.path.join(os.path.dirname(__file__), os.environ.get("DATABASE_PATH", "orders.db"))
CATALOG_API_URL = os.environ.get("CATALOG_API_URL", "http://127.0.0.1:3001")
REDIS_HOST = os.environ.get("REDIS_HOST", "127.0.0.1")
REDIS_PORT = int(os.environ.get("REDIS_PORT", "6379"))


def ensure_db() -> None:
    conn = sqlite3.connect(DB_PATH)
    cur = conn.cursor()
    cur.execute(
        """
        CREATE TABLE IF NOT EXISTS orders (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            product_id INTEGER NOT NULL,
            product_name TEXT NOT NULL,
            product_price REAL NOT NULL,
            quantity INTEGER NOT NULL,
            status TEXT NOT NULL
        )
        """
    )
    conn.commit()
    conn.close()


def list_orders() -> list[dict]:
    conn = sqlite3.connect(DB_PATH)
    cur = conn.cursor()
    cur.execute(
        "SELECT id, product_id, product_name, product_price, quantity, status FROM orders ORDER BY id DESC"
    )
    rows = [
        dict(
            id=row[0],
            product_id=row[1],
            product_name=row[2],
            product_price=row[3],
            quantity=row[4],
            status=row[5],
        )
        for row in cur.fetchall()
    ]
    conn.close()
    return rows


def create_order(product_id: int, quantity: int) -> dict:
    product = fetch_product(product_id)
    if not product:
        raise ValueError("product_not_found")

    conn = sqlite3.connect(DB_PATH)
    cur = conn.cursor()
    cur.execute(
        "INSERT INTO orders (product_id, product_name, product_price, quantity, status) VALUES (?, ?, ?, ?, ?)",
        (product["id"], product["name"], product["price"], quantity, "confirmed"),
    )
    row_id = cur.lastrowid
    conn.commit()
    conn.close()

    return dict(
        id=row_id,
        product_id=product["id"],
        product_name=product["name"],
        product_price=product["price"],
        quantity=quantity,
        status="confirmed",
    )


def fetch_product(product_id: int) -> dict:
    try:
        with urllib.request.urlopen(f"{CATALOG_API_URL}/products/{product_id}", timeout=1) as resp:
            if resp.status != 200:
                return None
            payload = json.load(resp)
    except (urllib.error.URLError, ValueError):
        return None
    return payload


def cache_ready() -> bool:
    try:
        with socket.create_connection((REDIS_HOST, REDIS_PORT), timeout=1):
            return True
    except OSError:
        return False


def catalog_ready() -> bool:
    try:
        with urllib.request.urlopen(f"{CATALOG_API_URL}/health", timeout=1) as response:
            if response.status != 200:
                return False
            payload = json.load(response)
    except (OSError, ValueError, json.JSONDecodeError):
        return False
    return payload.get("service") == "catalog-api" and payload.get("status") == "ok"


def write_json(payload, status=HTTPStatus.OK):
    body = json.dumps(payload, ensure_ascii=False).encode("utf-8")
    return status, [("Content-Type", "application/json; charset=utf-8")], body


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
        self.send_header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
        self.send_header("Access-Control-Allow-Headers", "Content-Type")
        self.end_headers()

    def do_GET(self):
        if self.path == "/health":
            deps = {
                "catalog_api": {
                    "ready": catalog_ready(),
                    "url": CATALOG_API_URL,
                },
                "redis": {
                    "ready": cache_ready(),
                },
            }
            status = "ok" if all((cache_ready(), catalog_ready())) else "degraded"
            code = HTTPStatus.OK if status == "ok" else HTTPStatus.SERVICE_UNAVAILABLE
            payload = {
                "service": "order-api",
                "status": status,
                "dependencies": deps,
            }
            headers, body = write_json(payload, code)
            json_response(self, code, headers, body)
            return

        if self.path == "/orders":
            payload = {"orders": list_orders()}
            headers, body = write_json(payload)
            json_response(self, HTTPStatus.OK, headers, body)
            return

        payload = {"code": "not_found", "message": "route not found"}
        headers, body = write_json(payload, HTTPStatus.NOT_FOUND)
        json_response(self, HTTPStatus.NOT_FOUND, headers, body)

    def do_POST(self):
        if self.path != "/orders":
            payload = {"code": "not_found", "message": "route not found"}
            headers, body = write_json(payload, HTTPStatus.NOT_FOUND)
            json_response(self, HTTPStatus.NOT_FOUND, headers, body)
            return

        size = int(self.headers.get("Content-Length", "0"))
        body_raw = self.rfile.read(size)
        try:
            data = json.loads(body_raw or b"{}")
            product_id = int(data.get("product_id", 0))
            quantity = int(data.get("quantity", 1))
        except (TypeError, ValueError, json.JSONDecodeError):
            payload = {"code": "bad_request", "message": "invalid payload"}
            headers, body = write_json(payload, HTTPStatus.BAD_REQUEST)
            json_response(self, HTTPStatus.BAD_REQUEST, headers, body)
            return

        if product_id <= 0 or quantity <= 0:
            payload = {"code": "bad_request", "message": "product_id and quantity must be positive integers"}
            headers, body = write_json(payload, HTTPStatus.BAD_REQUEST)
            json_response(self, HTTPStatus.BAD_REQUEST, headers, body)
            return

        try:
            order = create_order(product_id, quantity)
        except ValueError:
            payload = {"code": "product_not_found", "message": f"product {product_id} not found"}
            headers, body = write_json(payload, HTTPStatus.NOT_FOUND)
            json_response(self, HTTPStatus.NOT_FOUND, headers, body)
            return
        headers, body = write_json(order, HTTPStatus.CREATED)
        json_response(self, HTTPStatus.CREATED, headers, body)

    def log_message(self, fmt, *args):  # pragma: no cover
        return


def main() -> None:
    ensure_db()
    server = ThreadingHTTPServer(("0.0.0.0", 3002), Handler)
    server.serve_forever()


if __name__ == "__main__":
    main()
