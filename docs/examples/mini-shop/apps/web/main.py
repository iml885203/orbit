import os
import http.server


class Handler(http.server.SimpleHTTPRequestHandler):
    catalog_api = os.environ.get("CATALOG_API_URL", "http://127.0.0.1:3001")
    order_api = os.environ.get("ORDER_API_URL", "http://127.0.0.1:3002")

    def do_GET(self):
        if self.path in ("/", "/index.html"):
            self.write_index()
            return
        super().do_GET()

    def write_index(self) -> None:
        index_path = os.path.join(os.path.dirname(__file__), "index.html")
        with open(index_path, "r", encoding="utf-8") as file:
            template = file.read()
        rendered = template.replace("{{CATALOG_API_URL}}", self.catalog_api).replace(
            "{{ORDER_API_URL}}", self.order_api
        )
        body = rendered.encode("utf-8")
        self.send_response(200)
        self.send_header("Content-Type", "text/html; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_OPTIONS(self):
        self.send_response(204)
        self.send_header("Access-Control-Allow-Origin", "*")
        self.send_header("Access-Control-Allow-Headers", "Content-Type")
        self.send_header("Access-Control-Allow-Methods", "GET, OPTIONS")
        self.end_headers()

    def end_headers(self):
        self.send_header("Cache-Control", "no-store")
        super().end_headers()

    def log_message(self, fmt, *args):  # pragma: no cover
        return


if __name__ == "__main__":
    os.chdir(os.path.dirname(__file__))
    port = int(os.environ.get("PORT", "3000"))
    http.server.ThreadingHTTPServer(("0.0.0.0", port), Handler).serve_forever()
