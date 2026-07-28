import json
import os
import sqlite3
import urllib.error
import urllib.request
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import urlparse

DATA_DIR = Path(os.environ.get("DATA_DIR", ".")).resolve()
DB_PATH = DATA_DIR / os.environ.get("DATABASE_PATH", "cart.db")
CATALOG_API_URL = os.environ.get("CATALOG_API_URL", "http://127.0.0.1:3001")


def ensure_db() -> None:
    DB_PATH.parent.mkdir(parents=True, exist_ok=True)
    conn = sqlite3.connect(DB_PATH)
    cur = conn.cursor()
    cur.execute(
        """
        CREATE TABLE IF NOT EXISTS carts (
            customer_id INTEGER PRIMARY KEY,
            updated_at TEXT NOT NULL
        )
        """
    )
    cur.execute(
        """
        CREATE TABLE IF NOT EXISTS cart_items (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            customer_id INTEGER NOT NULL,
            product_id INTEGER NOT NULL,
            quantity INTEGER NOT NULL
        )
        """
    )
    conn.commit()
    conn.close()


def db_ready() -> bool:
    try:
        with sqlite3.connect(DB_PATH) as conn:
            conn.execute("SELECT name FROM carts LIMIT 1")
            conn.execute("SELECT name FROM cart_items LIMIT 1")
        return True
    except sqlite3.DatabaseError:
        return False


def ensure_cart(customer_id: int) -> None:
    conn = sqlite3.connect(DB_PATH)
    cur = conn.cursor()
    cur.execute(
        "INSERT OR IGNORE INTO carts (customer_id, updated_at) VALUES (?, datetime('now'))",
        (customer_id,),
    )
    conn.commit()
    conn.close()


def fetch_product(product_id: int) -> tuple[dict | None, str | None]:
    try:
        with urllib.request.urlopen(f"{CATALOG_API_URL}/products/{product_id}", timeout=1) as resp:
            if resp.status != 200:
                if resp.status == 404:
                    return None, "product_not_found"
                return None, "catalog_unreachable"
            payload = json.load(resp)
    except urllib.error.URLError:
        return None, "catalog_unreachable"
    except (ValueError, json.JSONDecodeError):
        return None, "catalog_unreachable"
    return payload, None


def add_item(customer_id: int, product_id: int, quantity: int) -> tuple[dict, str | None]:
    if quantity <= 0:
        return {}, "invalid_quantity"

    product, error = fetch_product(product_id)
    if error:
        if error == "product_not_found":
            return {}, "product_not_found"
        return {}, "catalog_unreachable"

    conn = sqlite3.connect(DB_PATH)
    cur = conn.cursor()
    ensure_cart(customer_id)
    cur.execute(
        "SELECT id, quantity FROM cart_items WHERE customer_id = ? AND product_id = ?",
        (customer_id, product_id),
    )
    row = cur.fetchone()
    if row is None:
        cur.execute(
            "INSERT INTO cart_items (customer_id, product_id, quantity) VALUES (?, ?, ?)",
            (customer_id, product_id, quantity),
        )
    else:
        cur.execute(
            "UPDATE cart_items SET quantity = quantity + ? WHERE customer_id = ? AND product_id = ?",
            (quantity, customer_id, product_id),
        )
    cur.execute(
        "UPDATE carts SET updated_at = datetime('now') WHERE customer_id = ?",
        (customer_id,),
    )
    conn.commit()
    conn.close()

    cart = get_cart(customer_id)
    if not cart:
        return {}, "internal_error"
    return cart, None


def get_cart(customer_id: int) -> dict:
    conn = sqlite3.connect(DB_PATH)
    cur = conn.cursor()
    cur.execute(
        """
        SELECT ci.id, ci.product_id, ci.quantity
        FROM cart_items ci
        WHERE ci.customer_id = ?
        ORDER BY ci.id
        """,
        (customer_id,),
    )
    rows = cur.fetchall()
    conn.close()

    items = []
    subtotal = 0.0
    for item_id, product_id, quantity in rows:
        product, error = fetch_product(product_id)
        if error:
            product = {
                "id": product_id,
                "name": f"product {product_id}",
                "price": 0.0,
            }
        line_total = float(product["price"]) * int(quantity)
        subtotal += line_total
        items.append(
            {
                "id": item_id,
                "product_id": product_id,
                "product_name": product["name"],
                "unit_price": product["price"],
                "quantity": quantity,
                "total": line_total,
            }
        )

    return {
        "customer_id": customer_id,
        "items": items,
        "total": subtotal,
        "item_count": sum(item["quantity"] for item in items),
    }


def clear_cart(customer_id: int) -> None:
    conn = sqlite3.connect(DB_PATH)
    cur = conn.cursor()
    cur.execute("DELETE FROM cart_items WHERE customer_id = ?", (customer_id,))
    cur.execute("DELETE FROM carts WHERE customer_id = ?", (customer_id,))
    conn.commit()
    conn.close()


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
        self.send_header("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
        self.send_header("Access-Control-Allow-Headers", "Content-Type")
        self.end_headers()

    def do_GET(self):
        parsed = urlparse(self.path)
        if parsed.path.startswith("/carts/"):
            parts = parsed.path.split("/")
            if len(parts) != 3:
                payload = {"code": "bad_request", "message": "invalid cart route"}
                headers, body = write_json(payload, HTTPStatus.BAD_REQUEST)
                json_response(self, HTTPStatus.BAD_REQUEST, headers, body)
                return

            try:
                customer_id = int(parts[2])
            except ValueError:
                payload = {"code": "bad_request", "message": "customer_id must be a number"}
                headers, body = write_json(payload, HTTPStatus.BAD_REQUEST)
                json_response(self, HTTPStatus.BAD_REQUEST, headers, body)
                return

            payload = get_cart(customer_id)
            headers, body = write_json(payload)
            json_response(self, HTTPStatus.OK, headers, body)
            return

        if parsed.path == "/health":
            ready = db_ready()
            payload = {
                "service": "cart-api",
                "status": "ok" if ready else "degraded",
                "dependencies": {
                    "sqlite": {"ready": ready, "path": str(DB_PATH)},
                    "catalog_api": {"ready": catalog_ready()},
                },
            }
            code = HTTPStatus.OK if payload["status"] == "ok" else HTTPStatus.SERVICE_UNAVAILABLE
            headers, body = write_json(payload)
            json_response(self, code, headers, body)
            return

        payload = {"code": "not_found", "message": "route not found"}
        headers, body = write_json(payload, HTTPStatus.NOT_FOUND)
        json_response(self, HTTPStatus.NOT_FOUND, headers, body)

    def do_POST(self):
        parsed = urlparse(self.path)
        if parsed.path == "/carts":
            size = int(self.headers.get("Content-Length", "0"))
            raw = self.rfile.read(size)
            try:
                data = json.loads(raw or b"{}")
                customer_id = int(data.get("customer_id", 0))
            except (TypeError, ValueError, json.JSONDecodeError):
                payload = {"code": "bad_request", "message": "invalid payload"}
                headers, body = write_json(payload, HTTPStatus.BAD_REQUEST)
                json_response(self, HTTPStatus.BAD_REQUEST, headers, body)
                return

            if customer_id <= 0:
                payload = {"code": "bad_request", "message": "customer_id must be positive"}
                headers, body = write_json(payload, HTTPStatus.BAD_REQUEST)
                json_response(self, HTTPStatus.BAD_REQUEST, headers, body)
                return

            ensure_cart(customer_id)
            payload = get_cart(customer_id)
            headers, body = write_json(payload)
            json_response(self, HTTPStatus.CREATED, headers, body)
            return

        if parsed.path.startswith("/carts/") and parsed.path.endswith("/items"):
            parts = parsed.path.split("/")
            if len(parts) != 4:
                payload = {"code": "bad_request", "message": "invalid cart route"}
                headers, body = write_json(payload, HTTPStatus.BAD_REQUEST)
                json_response(self, HTTPStatus.BAD_REQUEST, headers, body)
                return

            try:
                customer_id = int(parts[2])
            except ValueError:
                payload = {"code": "bad_request", "message": "customer_id must be a number"}
                headers, body = write_json(payload, HTTPStatus.BAD_REQUEST)
                json_response(self, HTTPStatus.BAD_REQUEST, headers, body)
                return

            size = int(self.headers.get("Content-Length", "0"))
            raw = self.rfile.read(size)
            try:
                data = json.loads(raw or b"{}")
                product_id = int(data.get("product_id", 0))
                quantity = int(data.get("quantity", 1))
            except (TypeError, ValueError, json.JSONDecodeError):
                payload = {"code": "bad_request", "message": "invalid payload"}
                headers, body = write_json(payload, HTTPStatus.BAD_REQUEST)
                json_response(self, HTTPStatus.BAD_REQUEST, headers, body)
                return

            cart, error = add_item(customer_id, product_id, quantity)
            if error == "product_not_found":
                payload = {"code": "product_not_found", "message": f"product {product_id} not found"}
                headers, body = write_json(payload, HTTPStatus.NOT_FOUND)
                json_response(self, HTTPStatus.NOT_FOUND, headers, body)
                return
            if error == "catalog_unreachable":
                payload = {"code": "catalog_unreachable", "message": "catalog API not ready"}
                headers, body = write_json(payload, HTTPStatus.SERVICE_UNAVAILABLE)
                json_response(self, HTTPStatus.SERVICE_UNAVAILABLE, headers, body)
                return
            if error in ("invalid_quantity", "internal_error"):
                code = HTTPStatus.BAD_REQUEST if error == "invalid_quantity" else HTTPStatus.INTERNAL_SERVER_ERROR
                payload = {"code": error, "message": "invalid quantity" if error == "invalid_quantity" else "internal error"}
                headers, body = write_json(payload, code)
                json_response(self, code, headers, body)
                return

            headers, body = write_json(cart, HTTPStatus.OK)
            json_response(self, HTTPStatus.OK, headers, body)
            return

        if parsed.path.endswith("/clear"):
            parts = parsed.path.split("/")
            if len(parts) != 4:
                payload = {"code": "bad_request", "message": "invalid cart route"}
                headers, body = write_json(payload, HTTPStatus.BAD_REQUEST)
                json_response(self, HTTPStatus.BAD_REQUEST, headers, body)
                return
            try:
                customer_id = int(parts[2])
            except ValueError:
                payload = {"code": "bad_request", "message": "customer_id must be a number"}
                headers, body = write_json(payload, HTTPStatus.BAD_REQUEST)
                json_response(self, HTTPStatus.BAD_REQUEST, headers, body)
                return
            clear_cart(customer_id)
            headers, body = write_json({"status": "ok", "message": "cart cleared"})
            json_response(self, HTTPStatus.OK, headers, body)
            return

        payload = {"code": "not_found", "message": "route not found"}
        headers, body = write_json(payload, HTTPStatus.NOT_FOUND)
        json_response(self, HTTPStatus.NOT_FOUND, headers, body)

    def do_DELETE(self):
        if self.path == "/carts":
            size = int(self.headers.get("Content-Length", "0"))
            raw = self.rfile.read(size)
            try:
                data = json.loads(raw or b"{}")
                customer_id = int(data.get("customer_id", 0))
            except (TypeError, ValueError, json.JSONDecodeError):
                payload = {"code": "bad_request", "message": "invalid payload"}
                headers, body = write_json(payload, HTTPStatus.BAD_REQUEST)
                json_response(self, HTTPStatus.BAD_REQUEST, headers, body)
                return

            clear_cart(customer_id)
            headers, body = write_json({"status": "ok"})
            json_response(self, HTTPStatus.OK, headers, body)
            return

        payload = {"code": "not_found", "message": "route not found"}
        headers, body = write_json(payload, HTTPStatus.NOT_FOUND)
        json_response(self, HTTPStatus.NOT_FOUND, headers, body)

    def log_message(self, fmt, *args):  # pragma: no cover
        return


def catalog_ready() -> bool:
    try:
        with urllib.request.urlopen(f"{CATALOG_API_URL}/health", timeout=1) as response:
            if response.status != 200:
                return False
            payload = json.load(response)
    except (OSError, ValueError, json.JSONDecodeError):
        return False
    return payload.get("service") == "catalog-api" and payload.get("status") == "ok"


def main() -> None:
    ensure_db()
    server = ThreadingHTTPServer(("0.0.0.0", 3005), Handler)
    server.serve_forever()


if __name__ == "__main__":
    main()
