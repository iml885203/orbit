#!/usr/bin/env python3

import pathlib
import signal
import socket
import sys
import time


def bind_ports(port_values: list[str]) -> list[socket.socket]:
    listeners = []
    try:
        for value in port_values:
            port = int(value)
            listener = socket.socket()
            listener.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
            try:
                listener.bind(("127.0.0.1", port))
            except OSError as error:
                listener.close()
                raise RuntimeError(f"test fixture could not own port {port}: {error}") from error
            listener.listen()
            listeners.append(listener)
    except BaseException:
        for listener in listeners:
            listener.close()
        raise
    return listeners


def main() -> int:
    if len(sys.argv) < 3 or sys.argv[1] not in {"check", "hold"}:
        print("usage: hold-test-ports.py check <port...> | hold <ready> <error> <port...>", file=sys.stderr)
        return 2

    if sys.argv[1] == "check":
        ports = sys.argv[2:]
        ready_path = None
        error_path = None
    else:
        if len(sys.argv) < 5:
            print("hold mode requires ready path, error path, and ports", file=sys.stderr)
            return 2
        ready_path = pathlib.Path(sys.argv[2])
        error_path = pathlib.Path(sys.argv[3])
        ports = sys.argv[4:]

    try:
        listeners = bind_ports(ports)
    except (RuntimeError, ValueError) as error:
        if error_path is not None:
            error_path.write_text(f"{error}\n", encoding="utf-8")
        print(error, file=sys.stderr)
        return 1

    if ready_path is None:
        for listener in listeners:
            listener.close()
        return 0

    ready_path.touch()
    stopped = False

    def stop_holding(_signum, _frame):
        nonlocal stopped
        stopped = True

    signal.signal(signal.SIGTERM, stop_holding)
    try:
        deadline = time.monotonic() + 600
        while not stopped and time.monotonic() < deadline:
            time.sleep(0.1)
    finally:
        for listener in listeners:
            listener.close()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
