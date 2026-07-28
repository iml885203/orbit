import json
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import sqlite3
from pathlib import Path
import os
from urllib.parse import urlparse

DATA_DIR = Path(os.environ.get("DATA_DIR", ".")).resolve()
DB_PATH = DATA_DIR / os.environ.get("DATABASE_PATH", "catalog.db")
seed_products = [
    ("Espresso Beans", 12.0),
    ("Oolong Tea", 18.5),
    ("Chocolate Muffin", 9.5),
]


def ensure_db() -> None:
    conn = sqlite3.connect(DB_PATH)
    cur = conn.cursor()
    cur.execute(
        """
        CREATE TABLE IF NOT EXISTS products (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            name TEXT NOT NULL,
            price REAL NOT NULL
        )
        """
    )
    cur.execute("SELECT COUNT(*) FROM products")
    (count,) = cur.fetchone()
    if count == 0:
        cur.executemany("INSERT INTO products (name, price) VALUES (?, ?)", seed_products)
    conn.commit()
    conn.close()


def list_products() -> list[dict]:
    conn = sqlite3.connect(DB_PATH)
    cur = conn.cursor()
    cur.execute("SELECT id, name, price FROM products ORDER BY id")
    rows = [dict(id=row[0], name=row[1], price=row[2]) for row in cur.fetchall()]
    conn.close()
    return rows


def get_product(product_id: int) -> dict | None:
    conn = sqlite3.connect(DB_PATH)
    cur = conn.cursor()
    cur.execute("SELECT id, name, price FROM products WHERE id = ?", (product_id,))
    row = cur.fetchone()
    conn.close()
    if not row:
        return None
    return dict(id=row[0], name=row[1], price=row[2])


def db_ready() -> bool:
    try:
        DB_PATH.parent.mkdir(parents=True, exist_ok=True)
        with sqlite3.connect(DB_PATH) as conn:
            conn.execute("PRAGMA quick_check")
        return True
    except OSError:
        return False
    except sqlite3.DatabaseError:
        return False


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
        parsed = urlparse(self.path)
        ensure_db()

        if parsed.path == "/health":
            ready = db_ready()
            payload = {
                "service": "catalog-api",
                "status": "ok" if ready else "degraded",
                "dependencies": {
                    "sqlite": {"ready": ready, "path": str(DB_PATH)},
                },
            }
            status = HTTPStatus.OK if payload["status"] == "ok" else HTTPStatus.SERVICE_UNAVAILABLE
            headers, body = write_json(payload, status=status)
            json_response(self, status, headers, body)
            return

        if parsed.path == "/products":
            payload = {
                "products": list_products(),
            }
            headers, body = write_json(payload)
            json_response(self, HTTPStatus.OK, headers, body)
            return

        if parsed.path.startswith("/products/"):
            try:
                product_id = int(parsed.path.split("/")[-1])
            except ValueError:
                product_id = None
            if not product_id:
                payload = {
                    "code": "bad_request",
                    "message": "product_id must be a number",
                }
                headers, body = write_json(payload, HTTPStatus.BAD_REQUEST)
                json_response(self, HTTPStatus.BAD_REQUEST, headers, body)
                return

            product = get_product(product_id)
            if not product:
                payload = {
                    "code": "not_found",
                    "message": f"product {product_id} not found",
                }
                headers, body = write_json(payload, HTTPStatus.NOT_FOUND)
                json_response(self, HTTPStatus.NOT_FOUND, headers, body)
                return

            headers, body = write_json(product)
            json_response(self, HTTPStatus.OK, headers, body)
            return

        payload = {"code": "not_found", "message": "route not found"}
        headers, body = write_json(payload, HTTPStatus.NOT_FOUND)
        json_response(self, HTTPStatus.NOT_FOUND, headers, body)

    def do_POST(self):
        payload = {"code": "method_not_allowed", "message": "POST not allowed on this route"}
        headers, body = write_json(payload, HTTPStatus.METHOD_NOT_ALLOWED)
        json_response(self, HTTPStatus.METHOD_NOT_ALLOWED, headers, body)

    def log_message(self, fmt, *args):  # pragma: no cover
        return


def main() -> None:
    ensure_db()
    server = ThreadingHTTPServer(("0.0.0.0", 3001), Handler)
    server.serve_forever()


if __name__ == "__main__":
    main()
