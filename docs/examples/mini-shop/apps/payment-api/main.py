import json
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


def _payment_ready() -> bool:
    return True


def _new_payment(payment_id: int, customer_id: int, amount: float, method: str, status: str) -> dict:
    return {
        "id": payment_id,
        "customer_id": customer_id,
        "amount": amount,
        "method": method,
        "status": status,
    }


def _validate_request(body: bytes) -> tuple[dict | None, str | None]:
    if not body:
        return None, "invalid_payload"
    try:
        payload = json.loads(body)
        customer_id = int(payload.get("customer_id", 0))
        amount = float(payload.get("amount", 0))
        method = payload.get("method", "mock_card")
    except (TypeError, ValueError, json.JSONDecodeError):
        return None, "invalid_payload"

    if customer_id <= 0:
        return None, "invalid_customer"
    if amount <= 0:
        return None, "invalid_amount"

    return {
        "customer_id": customer_id,
        "amount": amount,
        "method": method,
        "force_decline": bool(payload.get("force_decline", False)),
        "fail_threshold": float(payload.get("fail_threshold", 0.0)),
    }, None


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


def parse_fail_reason(payload: dict) -> str | None:
    if payload.get("method") == "decline" or payload.get("force_decline"):
        return "forced_decline"
    fail_threshold = float(payload.get("fail_threshold", 0.0) or 0.0)
    if fail_threshold > 0 and payload.get("amount") > fail_threshold:
        return "insufficient_funds"
    return None


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def do_OPTIONS(self):
        self.send_response(HTTPStatus.NO_CONTENT)
        self.send_header("Access-Control-Allow-Origin", "*")
        self.send_header("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
        self.send_header("Access-Control-Allow-Headers", "Content-Type")
        self.end_headers()

    def do_GET(self):
        if self.path != "/health":
            payload = {"code": "not_found", "message": "route not found"}
            headers, body = write_json(payload, HTTPStatus.NOT_FOUND)
            json_response(self, HTTPStatus.NOT_FOUND, headers, body)
            return

        payload = {
            "service": "payment-api",
            "status": "ok" if _payment_ready() else "degraded",
            "dependencies": {},
        }
        status = HTTPStatus.OK if payload["status"] == "ok" else HTTPStatus.SERVICE_UNAVAILABLE
        headers, body = write_json(payload, status)
        json_response(self, status, headers, body)

    def do_POST(self):
        if self.path != "/payments":
            payload = {"code": "not_found", "message": "route not found"}
            headers, body = write_json(payload, HTTPStatus.NOT_FOUND)
            json_response(self, HTTPStatus.NOT_FOUND, headers, body)
            return

        body = self.rfile.read(int(self.headers.get("Content-Length", "0")))
        payload, err = _validate_request(body)
        if err:
            status = HTTPStatus.BAD_REQUEST
            message = {
                "invalid_payload": "invalid payload",
                "invalid_customer": "customer_id must be positive",
                "invalid_amount": "amount must be greater than 0",
            }.get(err, "invalid payload")
            json_payload = {"code": err, "message": message}
            headers, body = write_json(json_payload, status)
            json_response(self, status, headers, body)
            return

        fail_reason = parse_fail_reason(payload)
        if fail_reason:
            status = HTTPStatus.PAYMENT_REQUIRED
            json_payload = {
                "code": fail_reason,
                "message": "payment declined",
            }
            headers, body = write_json(json_payload, status)
            json_response(self, status, headers, body)
            return

        payment = _new_payment(
            payment_id=abs(hash((payload["customer_id"], payload["amount"])) % 100000),
            customer_id=payload["customer_id"],
            amount=payload["amount"],
            method=payload["method"],
            status="paid",
        )
        headers, body = write_json(payment, HTTPStatus.CREATED)
        json_response(self, HTTPStatus.CREATED, headers, body)

    def log_message(self, fmt, *args):  # pragma: no cover
        return


def main() -> None:
    server = ThreadingHTTPServer(("0.0.0.0", 3007), Handler)
    server.serve_forever()


if __name__ == "__main__":
    main()
