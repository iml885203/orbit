import json
import os
import random
import sqlite3
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

DATA_DIR = os.path.abspath(os.environ.get("DATA_DIR", "."))
DB_PATH = os.path.join(DATA_DIR, os.environ.get("DATABASE_PATH", "shipping.db"))


def _ensure_db() -> None:
    os.makedirs(DATA_DIR, exist_ok=True)
    conn = sqlite3.connect(DB_PATH)
    cur = conn.cursor()
    cur.execute(
        """
        CREATE TABLE IF NOT EXISTS shipments (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            customer_id INTEGER NOT NULL,
            order_id INTEGER NOT NULL,
            tracking_no TEXT NOT NULL,
            status TEXT NOT NULL,
            courier TEXT NOT NULL
        )
        """
    )
    conn.commit()
    conn.close()


def db_ready() -> bool:
    try:
        with sqlite3.connect(DB_PATH) as conn:
            conn.execute("SELECT id FROM shipments LIMIT 1")
        return True
    except sqlite3.DatabaseError:
        return False


def create_shipment(customer_id: int, order_id: int) -> dict:
    tracking = f"TK{random.randint(100000, 999999)}"
    courier = random.choice(["Orbit Express", "Nova Post", "BlueSky Logistics"])
    conn = sqlite3.connect(DB_PATH)
    cur = conn.cursor()
    cur.execute(
        "INSERT INTO shipments (customer_id, order_id, tracking_no, status, courier) VALUES (?, ?, ?, ?, ?)",
        (customer_id, order_id, tracking, "pending", courier),
    )
    sid = cur.lastrowid
    conn.commit()
    conn.close()

    return {
        "id": sid,
        "order_id": order_id,
        "tracking_no": tracking,
        "courier": courier,
        "status": "pending",
        "customer_id": customer_id,
    }


def list_shipments(customer_id: int | None = None) -> list[dict]:
    conn = sqlite3.connect(DB_PATH)
    cur = conn.cursor()
    if customer_id is None:
        cur.execute(
            "SELECT id, customer_id, order_id, tracking_no, status, courier FROM shipments ORDER BY id DESC"
        )
    else:
        cur.execute(
            "SELECT id, customer_id, order_id, tracking_no, status, courier FROM shipments "
            "WHERE customer_id = ? ORDER BY id DESC",
            (customer_id,),
        )
    rows = [
        {
            "id": row[0],
            "customer_id": row[1],
            "order_id": row[2],
            "tracking_no": row[3],
            "status": row[4],
            "courier": row[5],
        }
        for row in cur.fetchall()
    ]
    conn.close()
    return rows


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
            payload = {
                "service": "shipping-api",
                "status": "ok" if db_ready() else "degraded",
                "dependencies": {
                    "sqlite": {"ready": db_ready(), "path": DB_PATH},
                },
            }
            status = HTTPStatus.OK if payload["status"] == "ok" else HTTPStatus.SERVICE_UNAVAILABLE
            headers, body = write_json(payload, status)
            json_response(self, status, headers, body)
            return

        if self.path.startswith("/shipments"):
            qs = self.path.split("?", 1)
            customer_id = None
            if len(qs) == 2 and qs[1].startswith("customer_id="):
                try:
                    customer_id = int(qs[1].split("=", 1)[1])
                except ValueError:
                    customer_id = None
            payload = {"shipments": list_shipments(customer_id)}
            headers, body = write_json(payload)
            json_response(self, HTTPStatus.OK, headers, body)
            return

        payload = {"code": "not_found", "message": "route not found"}
        headers, body = write_json(payload, HTTPStatus.NOT_FOUND)
        json_response(self, HTTPStatus.NOT_FOUND, headers, body)

    def do_POST(self):
        if self.path != "/shipments":
            payload = {"code": "not_found", "message": "route not found"}
            headers, body = write_json(payload, HTTPStatus.NOT_FOUND)
            json_response(self, HTTPStatus.NOT_FOUND, headers, body)
            return

        size = int(self.headers.get("Content-Length", "0"))
        raw = self.rfile.read(size)
        try:
            data = json.loads(raw or b"{}")
            customer_id = int(data.get("customer_id", 0))
            order_id = int(data.get("order_id", 0))
        except (TypeError, ValueError, json.JSONDecodeError):
            payload = {"code": "bad_request", "message": "invalid payload"}
            headers, body = write_json(payload, HTTPStatus.BAD_REQUEST)
            json_response(self, HTTPStatus.BAD_REQUEST, headers, body)
            return

        if customer_id <= 0 or order_id <= 0:
            payload = {"code": "bad_request", "message": "customer_id and order_id must be positive"}
            headers, body = write_json(payload, HTTPStatus.BAD_REQUEST)
            json_response(self, HTTPStatus.BAD_REQUEST, headers, body)
            return

        payload = create_shipment(customer_id=customer_id, order_id=order_id)
        headers, body = write_json(payload, HTTPStatus.CREATED)
        json_response(self, HTTPStatus.CREATED, headers, body)

    def log_message(self, fmt, *args):  # pragma: no cover
        return


def main() -> None:
    _ensure_db()
    server = ThreadingHTTPServer(("0.0.0.0", 3008), Handler)
    server.serve_forever()


if __name__ == "__main__":
    main()
