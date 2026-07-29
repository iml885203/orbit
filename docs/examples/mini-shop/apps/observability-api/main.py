import json
import os
import time
import urllib.error
import urllib.request
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import Any

ORDER_API_URL = os.environ.get("ORDER_API_URL", "http://127.0.0.1:3002")
SHIPPING_API_URL = os.environ.get("SHIPPING_API_URL", "http://127.0.0.1:3008")
NOTIFICATION_API_URL = os.environ.get("NOTIFICATION_API_URL", "").strip()
REFRESH_INTERVAL_SECONDS = float(os.environ.get("REFRESH_INTERVAL_SECONDS", "1.5"))


def write_json(payload: Any, status=HTTPStatus.OK):
    body = json.dumps(payload, ensure_ascii=False).encode("utf-8")
    return [
        ("Content-Type", "application/json; charset=utf-8"),
        ("Cache-Control", "no-store"),
    ], body


def json_response(handler: BaseHTTPRequestHandler, status: int, headers, body: bytes) -> None:
    handler.send_response(status)
    for key, value in headers:
        handler.send_header(key, value)
    handler.send_header("Content-Length", str(len(body)))
    handler.send_header("Access-Control-Allow-Origin", "*")
    handler.end_headers()
    handler.wfile.write(body)


def fetch_json(url: str, timeout: float = 1.0) -> tuple[dict | list | None, str | None]:
    try:
        with urllib.request.urlopen(url, timeout=timeout) as response:
            if response.status != HTTPStatus.OK:
                return None, f"status_{response.status}"
            payload = json.load(response)
            return payload, None
    except (OSError, ValueError, json.JSONDecodeError):
        return None, "unreachable"


def service_status(
    orders_payload: tuple[dict | list | None, str | None],
    shipments_payload: tuple[dict | list | None, str | None],
    notifications_payload: tuple[dict | list | None, str | None],
):
    orders_ok = orders_payload[0] is not None
    shipments_ok = shipments_payload[0] is not None
    notifications_ok = notifications_payload[0] is not None or NOTIFICATION_API_URL == ""
    if orders_ok and shipments_ok and notifications_ok:
        return "ok"
    if orders_ok or shipments_ok:
        return "degraded"
    return "unavailable"


def build_insights(
    orders_payload: dict | list | None,
    shipments_payload: dict | list | None,
    notifications_payload: dict | list | None,
    request_id: int,
) -> dict[str, Any]:
    orders = []
    if isinstance(orders_payload, dict):
        orders = orders_payload.get("orders", []) or []
    elif isinstance(orders_payload, list):
        orders = orders_payload

    shipments = []
    if isinstance(shipments_payload, dict):
        shipments = shipments_payload.get("shipments", []) or []
    elif isinstance(shipments_payload, list):
        shipments = shipments_payload

    shipment_by_order = {int(item.get("order_id", 0)): item for item in shipments if isinstance(item, dict)}
    matched = sum(1 for item in orders if int(item.get("id", 0)) in shipment_by_order)
    total_orders = len(orders)
    now = int(time.time())

    latest_order = orders[0] if orders else None
    latest_shipment = shipments[0] if shipments else None

    notifications = []
    if isinstance(notifications_payload, dict):
        notifications = notifications_payload.get("notifications", []) or []
    elif isinstance(notifications_payload, list):
        notifications = notifications_payload

    events = []
    for item in orders[:10]:
        if not isinstance(item, dict):
            continue
        order_id = int(item.get("id", 0))
        match = shipment_by_order.get(order_id)
        events.append(
            {
                "type": "correlated_checkout" if match else "pending_correlation",
                "order_id": order_id,
                "status": "ok" if match else "waiting_shipment",
                "customer_id": item.get("customer_id", 0),
                "shipment_id": match.get("id") if match else None,
                "tracking_no": match.get("tracking_no") if match else None,
                "timestamp": now,
            }
        )

    for item in notifications[:10]:
        if not isinstance(item, dict):
            continue
        events.append(
            {
                "type": "notification_emitted",
                "order_id": item.get("order_id", 0),
                "status": "ok",
                "customer_id": item.get("customer_id", 0),
                "title": item.get("title", "notification"),
                "message": item.get("message", ""),
                "tracking_no": None,
                "timestamp": item.get("created_at", now),
            }
        )

    correlation_ratio = (matched / total_orders) if total_orders > 0 else 1.0
    notifications_count = len(notifications)

    return {
        "service": "observability-api",
        "status": "ok",
        "request_id": request_id,
        "request_time": now,
        "refresh_interval_seconds": REFRESH_INTERVAL_SECONDS,
        "totals": {
            "orders": total_orders,
            "shipments": len(shipments),
            "notifications": notifications_count,
        },
        "correlation": {
            "matched_orders": matched,
            "correlation_ratio": round(correlation_ratio, 4),
        },
        "notifications": notifications[:10],
        "latest_order": latest_order,
        "latest_shipment": latest_shipment,
        "events": events,
    }


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def do_OPTIONS(self):
        self.send_response(HTTPStatus.NO_CONTENT)
        self.send_header("Access-Control-Allow-Origin", "*")
        self.send_header("Access-Control-Allow-Methods", "GET, OPTIONS")
        self.send_header("Access-Control-Allow-Headers", "Content-Type")
        self.end_headers()

    def do_GET(self):
        if self.path == "/health":
            orders_payload, orders_err = fetch_json(f"{ORDER_API_URL}/orders")
            shipments_payload, shipments_err = fetch_json(f"{SHIPPING_API_URL}/shipments")
            if NOTIFICATION_API_URL:
                notifications_payload, notifications_err = fetch_json(f"{NOTIFICATION_API_URL}/notifications")
            else:
                notifications_payload, notifications_err = None, None
            status = service_status(
                (orders_payload, orders_err),
                (shipments_payload, shipments_err),
                (notifications_payload, notifications_err),
            )
            http_status = HTTPStatus.OK if status == "ok" else HTTPStatus.SERVICE_UNAVAILABLE
            payload = {
                "service": "observability-api",
                "status": status,
                "dependencies": {
                    "order_api": {
                        "ready": orders_payload is not None,
                        "error": orders_err,
                        "url": ORDER_API_URL,
                    },
                    "shipping_api": {
                        "ready": shipments_payload is not None,
                        "error": shipments_err,
                        "url": SHIPPING_API_URL,
                    },
                    "notification_api": {
                        "ready": NOTIFICATION_API_URL == "" or (notifications_payload is not None),
                        "error": None if NOTIFICATION_API_URL == "" else notifications_err,
                        "url": NOTIFICATION_API_URL,
                        "enabled": bool(NOTIFICATION_API_URL),
                    },
                },
            }
            headers, body = write_json(payload)
            json_response(self, http_status, headers, body)
            return

        if self.path == "/insights" or self.path == "/events":
            request_id = int(time.time())
            orders_payload, _ = fetch_json(f"{ORDER_API_URL}/orders")
            shipments_payload, _ = fetch_json(f"{SHIPPING_API_URL}/shipments")
            if NOTIFICATION_API_URL:
                notifications_payload, notifications_err = fetch_json(f"{NOTIFICATION_API_URL}/notifications")
            else:
                notifications_payload, notifications_err = None, None
            _ = notifications_err
            payload = build_insights(
                orders_payload=orders_payload,
                shipments_payload=shipments_payload,
                notifications_payload=notifications_payload,
                request_id=request_id,
            )
            if self.path == "/events":
                payload = {"request_id": request_id, "events": payload.get("events", [])}
            headers, body = write_json(payload)
            json_response(self, HTTPStatus.OK, headers, body)
            return

        payload = {"code": "not_found", "message": "route not found"}
        headers, body = write_json(payload, HTTPStatus.NOT_FOUND)
        json_response(self, HTTPStatus.NOT_FOUND, headers, body)

    def log_message(self, fmt, *args):  # pragma: no cover
        return


def main() -> None:
    server = ThreadingHTTPServer(("0.0.0.0", 3010), Handler)
    server.serve_forever()


if __name__ == "__main__":
    main()
