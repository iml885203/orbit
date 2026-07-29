import json
import os
import urllib.error
import urllib.request
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

CATALOG_API_URL = "http://127.0.0.1:3001"
INVENTORY_API_URL = "http://127.0.0.1:3003"
CUSTOMER_API_URL = "http://127.0.0.1:3004"
ORDER_API_URL = "http://127.0.0.1:3002"
CART_API_URL = "http://127.0.0.1:3005"
PAYMENT_API_URL = "http://127.0.0.1:3007"
SHIPPING_API_URL = "http://127.0.0.1:3008"
NOTIFICATION_API_URL = os.environ.get("NOTIFICATION_API_URL", "").strip()


def _get(url: str):
    with urllib.request.urlopen(url, timeout=2) as response:
        return response.status, json.load(response)


def _post(url: str, payload: dict, method: str = "POST"):
    request = urllib.request.Request(
        url,
        method=method,
        data=json.dumps(payload).encode("utf-8"),
        headers={"Content-Type": "application/json"},
    )
    with urllib.request.urlopen(request, timeout=2) as response:
        return response.status, json.load(response)


def health_check(url: str, expect_service: str | None = None) -> dict:
    try:
        status, payload = _get(f"{url}/health")
    except urllib.error.URLError:
        return {"ready": False, "url": url}
    except (ValueError, json.JSONDecodeError):
        return {"ready": False, "url": url}

    if status != 200:
        return {"ready": False, "url": url}

    if expect_service and payload.get("service") != expect_service:
        return {"ready": False, "url": url}

    return {"ready": payload.get("status") == "ok", "url": url}


def read_cart(customer_id: int) -> tuple[dict, str | None]:
    try:
        status, payload = _get(f"{CART_API_URL}/carts/{customer_id}")
        if status != 200:
            return {}, "cart_unreachable"
        return payload, None
    except urllib.error.URLError:
        return {}, "cart_unreachable"


def reserve_all(items: list[dict]) -> tuple[bool, str | None]:
    reserved = []
    try:
        for item in items:
            if item.get("quantity", 0) <= 0:
                continue
            status, payload = _post(
                f"{INVENTORY_API_URL}/reserve",
                {"product_id": item["product_id"], "quantity": item["quantity"]},
            )
            if status != 200:
                for reserved_item in reserved:
                    _post(
                        f"{INVENTORY_API_URL}/release",
                        {"product_id": reserved_item["product_id"], "quantity": reserved_item["quantity"]},
                    )
                return False, payload.get("code", "inventory_unavailable")
            reserved.append(item)
        return True, None
    except urllib.error.HTTPError as e:
        payload = {}
        try:
            payload = json.load(e)
        except Exception:
            payload = {"code": "inventory_unreachable"}
        for reserved_item in reserved:
            _post(
                f"{INVENTORY_API_URL}/release",
                {"product_id": reserved_item["product_id"], "quantity": reserved_item["quantity"]},
            )
        return False, payload.get("code", "inventory_unavailable")
    except urllib.error.URLError:
        for reserved_item in reserved:
            _post(
                f"{INVENTORY_API_URL}/release",
                {"product_id": reserved_item["product_id"], "quantity": reserved_item["quantity"]},
            )
        return False, "inventory_unreachable"


def release_reserved(items: list[dict]) -> None:
    for item in items:
        try:
            _post(
                f"{INVENTORY_API_URL}/release",
                {"product_id": item["product_id"], "quantity": item["quantity"]},
            )
        except urllib.error.URLError:
            pass


def pay_total(customer_id: int, total: float, method: str, force_decline: bool) -> tuple[dict | None, str | None]:
    try:
        status, payload = _post(
            f"{PAYMENT_API_URL}/payments",
            {
                "customer_id": customer_id,
                "amount": total,
                "method": method,
                "force_decline": force_decline,
            },
        )
        if status != 201:
            return None, payload.get("code", "payment_failed")
        return payload, None
    except urllib.error.HTTPError as e:
        try:
            payload = json.load(e)
        except Exception:
            payload = {"code": "payment_failed"}
        return None, payload.get("code", "payment_failed")
    except urllib.error.URLError:
        return None, "payment_unreachable"


def create_order(customer_id: int, item: dict) -> tuple[dict | None, str | None]:
    try:
        status, payload = _post(
            f"{ORDER_API_URL}/orders",
            {
                "product_id": item["product_id"],
                "quantity": item["quantity"],
                "customer_id": customer_id,
            },
        )
        if status != 201:
            return None, payload.get("code", "order_failed")
        return payload, None
    except urllib.error.HTTPError as e:
        try:
            payload = json.load(e)
        except Exception:
            payload = {"code": "order_failed"}
        return None, payload.get("code", "order_failed")
    except urllib.error.URLError:
        return None, "order_unreachable"


def create_shipment(customer_id: int, order_id: int) -> dict | None:
    try:
        status, payload = _post(
            f"{SHIPPING_API_URL}/shipments",
            {"customer_id": customer_id, "order_id": order_id},
        )
        if status == 201:
            return payload
    except urllib.error.URLError:
        return None
    return None


def _notify(customer_id: int, order_id: int, title: str, message: str) -> bool:
    if not NOTIFICATION_API_URL:
        return False

    try:
        _, payload = _post(
            f"{NOTIFICATION_API_URL}/notifications",
            {
                "customer_id": customer_id,
                "order_id": order_id,
                "title": title,
                "message": message,
            },
        )
    except urllib.error.URLError:
        return False
    except (ValueError, json.JSONDecodeError):
        return False

    return isinstance(payload, dict) and payload.get("status") == "ok"


def clear_cart(customer_id: int) -> None:
    try:
        _post(f"{CART_API_URL}/carts/{customer_id}/clear", {})
    except urllib.error.URLError:
        pass


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
        if self.path != "/health":
            payload = {"code": "not_found", "message": "route not found"}
            headers, body = write_json(payload, HTTPStatus.NOT_FOUND)
            json_response(self, HTTPStatus.NOT_FOUND, headers, body)
            return

        deps = {
            "catalog_api": health_check(CATALOG_API_URL, "catalog-api"),
            "inventory_api": health_check(INVENTORY_API_URL, "inventory-api"),
            "customer_api": health_check(CUSTOMER_API_URL, "customer-api"),
            "order_api": health_check(ORDER_API_URL, "order-api"),
            "cart_api": health_check(CART_API_URL, "cart-api"),
            "payment_api": health_check(PAYMENT_API_URL, "payment-api"),
            "shipping_api": health_check(SHIPPING_API_URL, "shipping-api"),
        }
        if NOTIFICATION_API_URL:
            deps["notification_api"] = health_check(NOTIFICATION_API_URL, "notification-api")
        ready = all(item.get("ready") for item in deps.values())
        payload = {
            "service": "checkout-api",
            "status": "ok" if ready else "degraded",
            "dependencies": deps,
        }
        status = HTTPStatus.OK if payload["status"] == "ok" else HTTPStatus.SERVICE_UNAVAILABLE
        headers, body = write_json(payload, status)
        json_response(self, status, headers, body)

    def do_POST(self):
        if not self.path.startswith("/checkout/"):
            payload = {"code": "not_found", "message": "route not found"}
            headers, body = write_json(payload, HTTPStatus.NOT_FOUND)
            json_response(self, HTTPStatus.NOT_FOUND, headers, body)
            return

        try:
            customer_id = int(self.path.split("/", 2)[2])
        except ValueError:
            payload = {"code": "bad_request", "message": "customer_id must be a number"}
            headers, body = write_json(payload, HTTPStatus.BAD_REQUEST)
            json_response(self, HTTPStatus.BAD_REQUEST, headers, body)
            return

        size = int(self.headers.get("Content-Length", "0"))
        raw = self.rfile.read(size)
        try:
            req = json.loads(raw or b"{}")
            method = req.get("method", "mock_card")
            force_decline = bool(req.get("force_decline", False))
        except (TypeError, ValueError, json.JSONDecodeError):
            payload = {"code": "bad_request", "message": "invalid payload"}
            headers, body = write_json(payload, HTTPStatus.BAD_REQUEST)
            json_response(self, HTTPStatus.BAD_REQUEST, headers, body)
            return

        cart, err = read_cart(customer_id)
        if err:
            payload = {"code": "cart_unreachable", "message": "cart API not ready"}
            headers, body = write_json(payload, HTTPStatus.SERVICE_UNAVAILABLE)
            json_response(self, HTTPStatus.SERVICE_UNAVAILABLE, headers, body)
            return

        items = cart.get("items", [])
        if not items:
            payload = {"code": "cart_empty", "message": "cart is empty"}
            headers, body = write_json(payload, HTTPStatus.BAD_REQUEST)
            json_response(self, HTTPStatus.BAD_REQUEST, headers, body)
            return

        ok, reason = reserve_all(items)
        if not ok:
            payload = {"code": reason, "message": "inventory reserve failed"}
            status = HTTPStatus.CONFLICT if reason == "insufficient_stock" else HTTPStatus.SERVICE_UNAVAILABLE
            headers, body = write_json(payload, status)
            json_response(self, status, headers, body)
            return

        payment, reason = pay_total(customer_id, cart["total"], method, force_decline)
        if reason:
            release_reserved(items)
            payload = {"code": reason, "message": "checkout aborted by payment service"}
            pay_status = HTTPStatus.PAYMENT_REQUIRED if reason in ("payment_declined", "insufficient_funds", "forced_decline", "payment_failed") else HTTPStatus.SERVICE_UNAVAILABLE
            headers, body = write_json(payload, pay_status)
            json_response(self, pay_status, headers, body)
            return

        orders = []
        for item in items:
            order, err = create_order(customer_id=customer_id, item=item)
            if err:
                release_reserved(items)
                payload = {"code": err, "message": "order creation failed after payment"}
                headers, body = write_json(payload, HTTPStatus.SERVICE_UNAVAILABLE)
                json_response(self, HTTPStatus.SERVICE_UNAVAILABLE, headers, body)
                return
            orders.append(order)

        shipments = []
        notifications_requested = 0
        notifications_sent = 0
        for order in orders:
            ship = create_shipment(customer_id=customer_id, order_id=order["id"])
            if ship:
                shipments.append(ship)
                if NOTIFICATION_API_URL:
                    notifications_requested += 1
                if _notify(
                    customer_id=customer_id,
                    order_id=order["id"],
                    title="order shipped",
                    message=f"order {order['id']} shipped with {ship.get('tracking_no', 'pending')}",
                ):
                    notifications_sent += 1

        clear_cart(customer_id)

        payload = {
            "code": "checkout_ok",
            "status": "ok",
            "customer_id": customer_id,
            "payment": payment,
            "orders": orders,
            "shipments": shipments,
            "total": cart["total"],
            "items_count": cart["item_count"],
            "notifications": {
                "enabled": bool(NOTIFICATION_API_URL),
                "requested": notifications_requested,
                "sent": notifications_sent,
            },
        }
        headers, body = write_json(payload, HTTPStatus.CREATED)
        json_response(self, HTTPStatus.CREATED, headers, body)

    def log_message(self, fmt, *args):  # pragma: no cover
        return


def main() -> None:
    server = ThreadingHTTPServer(("0.0.0.0", 3006), Handler)
    server.serve_forever()


if __name__ == "__main__":
    main()
