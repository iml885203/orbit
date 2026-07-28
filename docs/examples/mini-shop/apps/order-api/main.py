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
INVENTORY_API_URL = os.environ.get("INVENTORY_API_URL", "http://127.0.0.1:3003")
CUSTOMER_API_URL = os.environ.get("CUSTOMER_API_URL", "http://127.0.0.1:3004")
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
            customer_id INTEGER NOT NULL,
            customer_name TEXT NOT NULL,
            customer_email TEXT NOT NULL,
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
        """
        SELECT id, product_id, product_name, product_price,
               customer_id, customer_name, customer_email, quantity, status
        FROM orders
        ORDER BY id DESC
        """
    )
    rows = [
        dict(
            id=row[0],
            product_id=row[1],
            product_name=row[2],
            product_price=row[3],
            customer_id=row[4],
            customer_name=row[5],
            customer_email=row[6],
            quantity=row[7],
            status=row[8],
        )
        for row in cur.fetchall()
    ]
    conn.close()
    return rows


def _create_order(product: dict, customer: dict, quantity: int) -> dict:
    conn = sqlite3.connect(DB_PATH)
    cur = conn.cursor()
    cur.execute(
        """
        INSERT INTO orders (
            product_id, product_name, product_price,
            customer_id, customer_name, customer_email,
            quantity, status
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
        """,
        (
            product["id"],
            product["name"],
            product["price"],
            customer["id"],
            customer["name"],
            customer["email"],
            quantity,
            "confirmed",
        ),
    )
    row_id = cur.lastrowid
    conn.commit()
    conn.close()

    return dict(
        id=row_id,
        product_id=product["id"],
        product_name=product["name"],
        product_price=product["price"],
        customer_id=customer["id"],
        customer_name=customer["name"],
        customer_email=customer["email"],
        quantity=quantity,
        status="confirmed",
    )


def create_order(product_id: int, quantity: int) -> dict:
    product, _ = fetch_product(product_id)
    if not product:
        raise ValueError("product_not_found")
    customer, _ = fetch_customer(1)
    if not customer:
        raise ValueError("customer_not_found")
    return _create_order(product, customer, quantity)


def create_order_with_customer(product: dict, customer: dict, quantity: int) -> dict:
    return _create_order(product, customer, quantity)


def reserve_inventory(product_id: int, quantity: int) -> None:
    payload = json.dumps({"product_id": product_id, "quantity": quantity}).encode("utf-8")
    req = urllib.request.Request(
        f"{INVENTORY_API_URL}/reserve",
        data=payload,
        method="POST",
        headers={"Content-Type": "application/json"},
    )
    with urllib.request.urlopen(req, timeout=1) as response:
        if response.status != 200:
            raise ValueError("inventory_unavailable")
        _ = json.load(response)


def release_inventory(product_id: int, quantity: int) -> bool:
    payload = json.dumps({"product_id": product_id, "quantity": quantity}).encode("utf-8")
    req = urllib.request.Request(
        f"{INVENTORY_API_URL}/release",
        data=payload,
        method="POST",
        headers={"Content-Type": "application/json"},
    )
    try:
        with urllib.request.urlopen(req, timeout=1) as response:
            if response.status != 200:
                return False
            _ = json.load(response)
            return True
    except urllib.error.URLError:
        return False


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


def fetch_customer(customer_id: int) -> tuple[dict | None, str | None]:
    try:
        with urllib.request.urlopen(f"{CUSTOMER_API_URL}/customers/{customer_id}", timeout=1) as resp:
            if resp.status != 200:
                if resp.status == 404:
                    return None, "customer_not_found"
                return None, "customer_unreachable"
            payload = json.load(resp)
    except urllib.error.URLError:
        return None, "customer_unreachable"
    except (ValueError, json.JSONDecodeError):
        return None, "customer_unreachable"
    return payload, None


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


def inventory_ready() -> bool:
    try:
        with urllib.request.urlopen(f"{INVENTORY_API_URL}/health", timeout=1) as response:
            if response.status != 200:
                return False
            payload = json.load(response)
    except (OSError, ValueError, json.JSONDecodeError):
        return False
    return payload.get("service") == "inventory-api" and payload.get("status") == "ok"


def customer_ready() -> bool:
    try:
        with urllib.request.urlopen(f"{CUSTOMER_API_URL}/health", timeout=1) as response:
            if response.status != 200:
                return False
            payload = json.load(response)
    except (OSError, ValueError, json.JSONDecodeError):
        return False
    return payload.get("service") == "customer-api" and payload.get("status") == "ok"


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
                "inventory_api": {
                    "ready": inventory_ready(),
                    "url": INVENTORY_API_URL,
                },
                "customer_api": {
                    "ready": customer_ready(),
                    "url": CUSTOMER_API_URL,
                },
                "redis": {
                    "ready": cache_ready(),
                },
            }
            status = (
                "ok"
                if all((cache_ready(), catalog_ready(), inventory_ready(), customer_ready()))
                else "degraded"
            )
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
            customer_id = int(data.get("customer_id", 1))
        except (TypeError, ValueError, json.JSONDecodeError):
            payload = {"code": "bad_request", "message": "invalid payload"}
            headers, body = write_json(payload, HTTPStatus.BAD_REQUEST)
            json_response(self, HTTPStatus.BAD_REQUEST, headers, body)
            return

        if product_id <= 0 or quantity <= 0:
            payload = {
                "code": "bad_request",
                "message": "product_id and quantity must be positive integers",
            }
            headers, body = write_json(payload, HTTPStatus.BAD_REQUEST)
            json_response(self, HTTPStatus.BAD_REQUEST, headers, body)
            return
        if customer_id <= 0:
            payload = {
                "code": "bad_request",
                "message": "customer_id must be a positive integer",
            }
            headers, body = write_json(payload, HTTPStatus.BAD_REQUEST)
            json_response(self, HTTPStatus.BAD_REQUEST, headers, body)
            return

        product, product_error = fetch_product(product_id)
        if product_error == "product_not_found":
            payload = {"code": "product_not_found", "message": f"product {product_id} not found"}
            headers, body = write_json(payload, HTTPStatus.NOT_FOUND)
            json_response(self, HTTPStatus.NOT_FOUND, headers, body)
            return
        if product_error:
            payload = {"code": "catalog_unreachable", "message": "catalog API not reachable"}
            headers, body = write_json(payload, HTTPStatus.SERVICE_UNAVAILABLE)
            json_response(self, HTTPStatus.SERVICE_UNAVAILABLE, headers, body)
            return

        customer, customer_error = fetch_customer(customer_id)
        if customer_error == "customer_not_found":
            payload = {"code": "customer_not_found", "message": f"customer {customer_id} not found"}
            headers, body = write_json(payload, HTTPStatus.NOT_FOUND)
            json_response(self, HTTPStatus.NOT_FOUND, headers, body)
            return
        if customer_error:
            payload = {"code": "customer_unreachable", "message": "customer API not reachable"}
            headers, body = write_json(payload, HTTPStatus.SERVICE_UNAVAILABLE)
            json_response(self, HTTPStatus.SERVICE_UNAVAILABLE, headers, body)
            return

        try:
            reserve_inventory(product_id, quantity)
            try:
                order = create_order_with_customer(product=product, customer=customer, quantity=quantity)
            except sqlite3.Error:
                if not release_inventory(product_id, quantity):
                    payload = {
                        "code": "order_failed_release_failed",
                        "message": (
                            "order persistence failed and inventory rollback failed. "
                            "please retry after a short wait"
                        ),
                    }
                    headers, body = write_json(payload, HTTPStatus.SERVICE_UNAVAILABLE)
                    json_response(self, HTTPStatus.SERVICE_UNAVAILABLE, headers, body)
                    return
                payload = {"code": "order_failed", "message": "order persistence failed, stock was rolled back"}
                headers, body = write_json(payload, HTTPStatus.SERVICE_UNAVAILABLE)
                json_response(self, HTTPStatus.SERVICE_UNAVAILABLE, headers, body)
                return
            except ValueError:
                _ = release_inventory(product_id, quantity)
                payload = {"code": "product_not_found", "message": f"product {product_id} not found"}
                headers, body = write_json(payload, HTTPStatus.NOT_FOUND)
                json_response(self, HTTPStatus.NOT_FOUND, headers, body)
                return
        except urllib.error.HTTPError as err:
            if err.code == 404:
                payload = {"code": "inventory_not_found", "message": f"product {product_id} not found"}
                status = HTTPStatus.NOT_FOUND
            elif err.code == 409:
                payload = {"code": "insufficient_stock", "message": "inventory not enough"}
                status = HTTPStatus.CONFLICT
            else:
                payload = {"code": "inventory_error", "message": "reserve failed"}
                status = HTTPStatus.SERVICE_UNAVAILABLE
            headers, body = write_json(payload, status)
            json_response(self, status, headers, body)
            return
        except urllib.error.URLError:
            payload = {"code": "inventory_unreachable", "message": "inventory API not reachable"}
            headers, body = write_json(payload, HTTPStatus.SERVICE_UNAVAILABLE)
            json_response(self, HTTPStatus.SERVICE_UNAVAILABLE, headers, body)
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
