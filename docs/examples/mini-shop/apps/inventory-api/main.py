import json
import os
import sqlite3
import socket
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import urlparse


DATA_DIR = Path(os.environ.get("DATA_DIR", ".")).resolve()
DB_PATH = DATA_DIR / os.environ.get("DATABASE_PATH", "inventory.db")
REDIS_HOST = os.environ.get("REDIS_HOST", "127.0.0.1")
REDIS_PORT = int(os.environ.get("REDIS_PORT", "6379"))
INITIAL_STOCK = {
    1: 20,
    2: 15,
    3: 30,
}


def ensure_db() -> None:
    conn = sqlite3.connect(DB_PATH)
    cur = conn.cursor()
    cur.execute(
        """
        CREATE TABLE IF NOT EXISTS inventory (
            product_id INTEGER PRIMARY KEY,
            stock INTEGER NOT NULL
        )
        """
    )
    for product_id, stock in INITIAL_STOCK.items():
        cur.execute(
            "INSERT INTO inventory (product_id, stock) VALUES (?, ?) ON CONFLICT(product_id) DO NOTHING",
            (product_id, stock),
        )
    conn.commit()
    conn.close()


def list_inventory() -> list[dict]:
    conn = sqlite3.connect(DB_PATH)
    cur = conn.cursor()
    cur.execute("SELECT product_id, stock FROM inventory ORDER BY product_id")
    rows = [dict(product_id=row[0], stock=row[1]) for row in cur.fetchall()]
    conn.close()
    return rows


def get_stock(product_id: int) -> int:
    conn = sqlite3.connect(DB_PATH)
    cur = conn.cursor()
    cur.execute("SELECT stock FROM inventory WHERE product_id = ?", (product_id,))
    row = cur.fetchone()
    conn.close()
    if not row:
        return 0
    return int(row[0])


def reserve_stock(product_id: int, quantity: int) -> bool:
    conn = sqlite3.connect(DB_PATH)
    cur = conn.cursor()
    cur.execute("SELECT stock FROM inventory WHERE product_id = ?", (product_id,))
    row = cur.fetchone()
    if not row:
        conn.close()
        return False

    stock = int(row[0])
    if stock < quantity:
        conn.close()
        return False

    cur.execute(
        "UPDATE inventory SET stock = ? WHERE product_id = ?",
        (stock - quantity, product_id),
    )
    conn.commit()
    conn.close()
    return True


def release_stock(product_id: int, quantity: int) -> bool:
    if quantity <= 0:
        return False
    conn = sqlite3.connect(DB_PATH)
    cur = conn.cursor()
    cur.execute("SELECT stock FROM inventory WHERE product_id = ?", (product_id,))
    row = cur.fetchone()
    if not row:
        conn.close()
        return False
    cur.execute(
        "UPDATE inventory SET stock = stock + ? WHERE product_id = ?",
        (quantity, product_id),
    )
    conn.commit()
    changed = conn.total_changes > 0
    conn.close()
    return bool(changed)


def cache_ready() -> bool:
    try:
        with socket.create_connection((REDIS_HOST, REDIS_PORT), timeout=1):
            return True
    except OSError:
        return False


def db_ready() -> bool:
    try:
        DB_PATH.parent.mkdir(parents=True, exist_ok=True)
        with sqlite3.connect(DB_PATH) as conn:
            conn.execute("PRAGMA quick_check")
        return True
    except (OSError, sqlite3.DatabaseError):
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


def reserve_to_response(product_id: int, quantity: int) -> tuple[int, dict]:
    if product_id <= 0 or quantity <= 0:
        return (
            HTTPStatus.BAD_REQUEST,
            {"code": "bad_request", "message": "product_id and quantity must be positive integers"},
        )

    current = get_stock(product_id)
    if current <= 0:
        return (
            HTTPStatus.NOT_FOUND,
            {"code": "not_found", "message": f"product {product_id} not found"},
        )
    if current < quantity:
        return (
            HTTPStatus.CONFLICT,
            {"code": "insufficient_stock", "message": "not enough stock"},
        )

    if not reserve_stock(product_id, quantity):
        return (
            HTTPStatus.CONFLICT,
            {"code": "insufficient_stock", "message": "not enough stock"},
        )
    return (
        HTTPStatus.OK,
        {"product_id": product_id, "reserved": quantity, "remaining": get_stock(product_id)},
    )


def release_to_response(product_id: int, quantity: int) -> tuple[int, dict]:
    if product_id <= 0 or quantity <= 0:
        return (
            HTTPStatus.BAD_REQUEST,
            {"code": "bad_request", "message": "product_id and quantity must be positive integers"},
        )

    if not release_stock(product_id, quantity):
        return (
            HTTPStatus.NOT_FOUND,
            {"code": "not_found", "message": f"product {product_id} not found"},
        )

    return (
        HTTPStatus.OK,
        {"product_id": product_id, "released": quantity, "remaining": get_stock(product_id)},
    )


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def do_OPTIONS(self):
        self.send_response(HTTPStatus.NO_CONTENT)
        self.send_header("Access-Control-Allow-Origin", "*")
        self.send_header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
        self.send_header("Access-Control-Allow-Headers", "Content-Type")
        self.end_headers()

    def do_GET(self):
        parsed = urlparse(self.path)

        if parsed.path == "/health":
            deps = {
                "sqlite": {"ready": db_ready(), "path": str(DB_PATH)},
                "redis": {"ready": cache_ready()},
            }
            all_ready = all(item["ready"] for item in deps.values())
            payload = {
                "service": "inventory-api",
                "status": "ok" if all_ready else "degraded",
                "dependencies": deps,
            }
            status = HTTPStatus.OK if payload["status"] == "ok" else HTTPStatus.SERVICE_UNAVAILABLE
            headers, body = write_json(payload, status)
            json_response(self, status, headers, body)
            return

        if parsed.path == "/stock":
            headers, body = write_json({"items": list_inventory()})
            json_response(self, HTTPStatus.OK, headers, body)
            return

        payload = {"code": "not_found", "message": "route not found"}
        headers, body = write_json(payload, HTTPStatus.NOT_FOUND)
        json_response(self, HTTPStatus.NOT_FOUND, headers, body)

    def do_POST(self):
        if self.path == "/reserve":
            size = int(self.headers.get("Content-Length", "0"))
            raw = self.rfile.read(size)
            try:
                payload = json.loads(raw or b"{}")
                product_id = int(payload.get("product_id", 0))
                quantity = int(payload.get("quantity", 1))
            except (TypeError, ValueError, json.JSONDecodeError):
                headers, body = write_json(
                    {"code": "bad_request", "message": "invalid payload"},
                    HTTPStatus.BAD_REQUEST,
                )
                json_response(self, HTTPStatus.BAD_REQUEST, headers, body)
                return

            status, response = reserve_to_response(product_id, quantity)
            headers, body = write_json(response, status)
            json_response(self, status, headers, body)
            return

        if self.path == "/release":
            size = int(self.headers.get("Content-Length", "0"))
            raw = self.rfile.read(size)
            try:
                payload = json.loads(raw or b"{}")
                product_id = int(payload.get("product_id", 0))
                quantity = int(payload.get("quantity", 1))
            except (TypeError, ValueError, json.JSONDecodeError):
                headers, body = write_json(
                    {"code": "bad_request", "message": "invalid payload"},
                    HTTPStatus.BAD_REQUEST,
                )
                json_response(self, HTTPStatus.BAD_REQUEST, headers, body)
                return

            status, response = release_to_response(product_id, quantity)
            headers, body = write_json(response, status)
            json_response(self, status, headers, body)
            return

        payload = {"code": "not_found", "message": "route not found"}
        headers, body = write_json(payload, HTTPStatus.NOT_FOUND)
        json_response(self, HTTPStatus.NOT_FOUND, headers, body)

    def log_message(self, fmt, *args):  # pragma: no cover
        return


def main() -> None:
    ensure_db()
    server = ThreadingHTTPServer(("0.0.0.0", 3003), Handler)
    server.serve_forever()


if __name__ == "__main__":
    main()
