#!/usr/bin/env python3
"""Stub tests for labctl.py."""
import importlib.util
import io
import json
import shutil
import select
import os
import socket
import ssl
import subprocess
import sys
import tempfile
import threading
import time
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from unittest import mock
from urllib.parse import quote, urlparse
from urllib.request import ProxyHandler, Request, build_opener
HERE = Path(__file__).resolve().parent
LABCTL = HERE / "labctl.py"
ROOT = HERE.parents[1]
PROTO = "2025-11-25"
SENTINEL = "tok_sentinel_value_9f3a"
PROXY_ENV = (
    "http_proxy",
    "https_proxy",
    "HTTP_PROXY",
    "HTTPS_PROXY",
    "ALL_PROXY",
    "all_proxy",
    "no_proxy",
    "NO_PROXY",
)
TOKEN_ENV = ("LABCTL_TOKEN", "LABCTL_TOKEN_FILE", "LABCTL_URL", "LABCTL_PROXY", "LABCTL_TIMEOUT", "LABCTL_MAX_PAGES", "LABCTL_MAX_TOOLS")
def _load_client():
    spec = importlib.util.spec_from_file_location("labctl", str(LABCTL))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module

def json_equal(left, right):
    if isinstance(left, bool) or isinstance(right, bool):
        return isinstance(left, bool) and isinstance(right, bool) and left is right
    if isinstance(left, dict) and isinstance(right, dict):
        if set(left.keys()) != set(right.keys()):
            return False
        return all(json_equal(left[k], right[k]) for k in left)
    if isinstance(left, list) and isinstance(right, list):
        if len(left) != len(right):
            return False
        return all(json_equal(a, b) for a, b in zip(left, right))
    if isinstance(left, (int, float)) and isinstance(right, (int, float)):
        return left == right
    return left == right and type(left) is type(right)
def _clean_env(extra=None):
    env = os.environ.copy()
    for key in list(PROXY_ENV) + list(TOKEN_ENV):
        env.pop(key, None)
    if extra:
        env.update(extra)
    return env
def run_cli(args, env=None, timeout=20, isolated=True, cwd=None):
    cmd = [sys.executable]
    if isolated:
        cmd += ["-I", "-S"]
    cmd += [str(LABCTL)] + list(args)
    return subprocess.run(
        cmd,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        env=_clean_env(env),
        timeout=timeout,
        universal_newlines=True,
        cwd=cwd or str(ROOT),
    )
def load_json(proc):
    text = proc.stdout.strip()
    if not text:
        return None
    return json.loads(text)
def build_child_env(local_token_file=None, extra=None):
    """Live-helper child env: clear tokens and ambient proxies, then set only the local token file."""
    env = _clean_env(extra)
    env.pop("LABCTL_TOKEN", None)
    env.pop("LABCTL_TOKEN_FILE", None)
    for key in PROXY_ENV:
        env.pop(key, None)
    if local_token_file:
        env["LABCTL_TOKEN_FILE"] = local_token_file
    return env
class _StubState(object):
    def __init__(self):
        self.lock = threading.Lock()
        self.records = []
        self.session = "sess-test-1"
        self.mount = "/mcp"
        self.mode = "json"  # json | sse
        self.initialized = False
        self.require_handshake = True
        self.require_caps = True
        self.require_session = True
        self.require_bearer = None
        self.protocol = PROTO
        self.pages = None
        self.list_calls = 0
        self.call_result = {"content": [{"type": "text", "text": "ok"}]}
        self.call_is_error = False
        self.call_rpc_error = None
        self.delete_status = 200
        self.redirect_to = None
        self.http_status = None
        self.http_body = b""
        self.trickle = None
        self.hold_after_sse = 0.0
        self.prepend_sse_event = False
        self.reject_other_paths = True
        self.tools = None
        self.repeat_cursor = None
        self.seen_methods = []
        self.chunked = False
        self.omit_jsonrpc = False
        self.sse_inject_request = False
        self.switch_session_after_init = None
        self.list_result_override = None
        self.notify_status = None
        self.notify_body = b""
        self.stall_delete = False
        self.list_delay = 0
        self.delete_closed = threading.Event()
        self.trickle_bytes = 0
def _tool(name, desc, ro=True):
    tool = {"name": name, "description": desc, "inputSchema": {"type": "object", "properties": {}}}
    if ro:
        tool["annotations"] = {"readOnlyHint": True}
    return tool
def _default_tools():
    return [
        _tool("labinfo__endpoints_list", "List endpoints"),
        _tool("labmail__mail_state_get", "Mail state"),
        _tool("labdns__dns_state_get", "DNS state", ro=False),
    ]
class _StubHandler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"
    def log_message(self, fmt, *args):
        return
    def _state(self):
        return self.server.state
    def _record(self, method, body):
        st = self._state()
        rec = {
            "method": method,
            "path": self.path,
            "received": time.monotonic(),
            "headers": {k.lower(): v for k, v in self.headers.items()},
            "body": body,
        }
        with st.lock:
            st.records.append(rec)
            st.seen_methods.append(method)
        return rec
    def _read_body(self):
        length = int(self.headers.get("Content-Length") or 0)
        if length <= 0:
            return b""
        return self.rfile.read(length)
    def _reject_mount(self):
        st = self._state()
        path_only = self.path.split("?", 1)[0]
        if st.reject_other_paths and path_only != st.mount:
            self._send_raw(404, b"wrong path", "text/plain")
            return True
        return False
    def do_POST(self):
        st = self._state()
        body = self._read_body()
        rec = self._record("POST", body)
        if st.redirect_to:
            self.send_response(302)
            self.send_header("Location", st.redirect_to)
            self.send_header("Content-Length", "0")
            self.end_headers()
            return
        if st.http_status is not None:
            self._send_raw(st.http_status, st.http_body, "text/plain")
            return
        if self._reject_mount():
            return
        if st.require_bearer is not None:
            auth = rec["headers"].get("authorization", "")
            if auth != "Bearer %s" % st.require_bearer:
                self._send_raw(401, b"bad bearer", "text/plain")
                return
        if st.trickle:
            self._trickle(st)
            return
        try:
            msg = json.loads(body.decode("utf-8") or "null")
        except ValueError:
            self._send_raw(400, b"bad json", "text/plain")
            return
        method = msg.get("method")
        req_id = msg.get("id")
        params = msg.get("params") or {}
        if method == "initialize":
            self._handle_initialize(st, rec, msg, req_id, params)
            return
        if st.require_session and rec["headers"].get("mcp-session-id") != st.session:
            self._send_raw(400, b"missing session", "text/plain")
            return
        if method == "notifications/initialized":
            if st.notify_status is not None:
                self._send_raw(st.notify_status, st.notify_body, "application/json")
                return
            if req_id is not None:
                self._rpc(st, req_id, {"ok": True})
                return
            st.initialized = True
            self.send_response(202)
            self.send_header("Content-Length", "0")
            if st.session is not None:
                self.send_header("Mcp-Session-Id", st.session)
            self.end_headers()
            return
        if st.require_handshake and not st.initialized:
            self._send_raw(400, b"out of order", "text/plain")
            return
        if method == "tools/list":
            self._handle_list(st, req_id, params)
            return
        if method == "tools/call":
            self._handle_call(st, req_id, params)
            return
        self._send_raw(400, b"unknown method", "text/plain")
    def do_DELETE(self):
        st = self._state()
        self._record("DELETE", self._read_body())
        if self._reject_mount():
            return
        if st.stall_delete:
            try:
                self.connection.settimeout(2.0)
                self.rfile.read(1)
            finally:
                st.delete_end = time.monotonic()
                st.delete_closed.set()
            return
        self._send_raw(st.delete_status, b"", "text/plain")
    def do_GET(self):
        self._record("GET", self._read_body()); self._send_raw(405, b"no", "text/plain")
    def do_PUT(self):
        self._record("PUT", self._read_body()); self._send_raw(405, b"no", "text/plain")
    def do_HEAD(self):
        self._record("HEAD", b""); self.send_response(405); self.end_headers()
    def _handle_initialize(self, st, rec, msg, req_id, params):
        info = params.get("clientInfo") or {}
        if st.require_caps and (params.get("capabilities") != {} or not info.get("name") or not info.get("version")):
            self._send_raw(400, b"bad initialize", "text/plain")
            return
        self._rpc(st, req_id, {"protocolVersion": st.protocol, "capabilities": {}, "serverInfo": {"name": "stub", "version": "0"}}, session=True)
        if st.switch_session_after_init:
            st.session = st.switch_session_after_init
    def _tools_payload(self, st, params):
        if st.list_result_override is not None:
            return st.list_result_override
        tools = st.tools if st.tools is not None else _default_tools()
        if st.repeat_cursor:
            return {"tools": tools, "nextCursor": st.repeat_cursor}
        if st.pages is None:
            return {"tools": tools}
        st.list_calls += 1
        cursor = params.get("cursor")
        idx = 0
        if cursor and cursor.startswith("page-") and cursor[5:].isdigit():
            idx = int(cursor[5:]) - 1
        page = st.pages[idx] if 0 <= idx < len(st.pages) else []
        out = {"tools": page}
        if idx + 1 < len(st.pages):
            out["nextCursor"] = "page-%d" % (idx + 2)
        return out
    def _handle_list(self, st, req_id, params):
        time.sleep(st.list_delay)
        self._rpc(st, req_id, self._tools_payload(st, params))
    def _handle_call(self, st, req_id, params):
        if st.call_rpc_error is not None:
            self._rpc_error(st, req_id, st.call_rpc_error)
            return
        result = st.call_result
        if st.call_is_error and isinstance(result, dict):
            result = dict(result)
            result["isError"] = True
        self._rpc(st, req_id, result)
    def _rpc(self, st, req_id, result, session=False):
        self._send_msg(st, {"jsonrpc": "2.0", "id": req_id, "result": result}, session=session)
    def _rpc_error(self, st, req_id, error):
        self._send_msg(st, {"jsonrpc": "2.0", "id": req_id, "error": error})
    def _send_msg(self, st, msg, session=False):
        payload = dict(msg)
        if st.omit_jsonrpc:
            payload.pop("jsonrpc", None)
        if st.mode == "sse":
            parts = []
            if st.prepend_sse_event:
                parts.append("event: ping\ndata: {\"method\":\"notifications/progress\"}\n\n")
            if st.sse_inject_request:
                parts.append("data: %s\n\n" % json.dumps({"jsonrpc": "2.0", "id": msg.get("id"), "method": "ping"}))
            parts.append("data: %s\r\n\r\n" % json.dumps(payload))
            raw, ctype = "".join(parts).encode("utf-8"), "text/event-stream"
        else:
            raw, ctype = json.dumps(payload).encode("utf-8"), "application/json"
        self.send_response(200)
        self.send_header("Content-Type", ctype)
        if st.session is not None:
            self.send_header("Mcp-Session-Id", st.session)
        hold = st.hold_after_sse if st.mode == "sse" else 0.0
        try:
            if st.chunked:
                self.send_header("Transfer-Encoding", "chunked")
                self.end_headers()
                mid = max(1, len(raw) // 2)
                self.wfile.write(b"%x\r\n%s\r\n" % (len(raw[:mid]), raw[:mid]))
                if raw[mid:]:
                    self.wfile.write(b"%x\r\n%s\r\n" % (len(raw[mid:]), raw[mid:]))
                self.wfile.flush()
                if hold:
                    time.sleep(hold)
                self.wfile.write(b"0\r\n\r\n")
            else:
                pad = b"\n" if hold else b""
                self.send_header("Content-Length", str(len(raw) + len(pad)))
                self.end_headers()
                self.wfile.write(raw)
                self.wfile.flush()
                if hold:
                    time.sleep(hold)
                    self.wfile.write(pad)
            self.wfile.flush()
        except (BrokenPipeError, ConnectionResetError, OSError):
            return
    def _send_raw(self, status, body, ctype):
        self.send_response(status)
        self.send_header("Content-Type", ctype)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)
    def _trickle(self, st):
        kind = st.trickle
        try:
            if kind in ("body", "sse"):
                ctype = "application/json" if kind == "body" else "text/event-stream"
                self.send_response(200)
                self.send_header("Content-Type", ctype)
                self.send_header("Content-Length", "200")
                self.end_headers()
                data = b" " * 200 if kind == "body" else b"data: " + b" " * 194
            else:
                if kind == "headers":
                    self.connection.sendall(b"HTTP/1.1 200 OK\r\n")
                data = b"Content-Type: application/json\r\n" if kind == "headers" else b"HTTP/1.1 200 OK\r\n"
            for byte in data:
                self.connection.sendall(bytes([byte]))
                st.trickle_bytes += 1
                time.sleep(0.05)
        except (BrokenPipeError, ConnectionResetError, OSError):
            return
class StubServer(object):
    def __init__(self, mount="/mcp"):
        self.state = _StubState()
        self.state.mount = mount
        self._httpd = ThreadingHTTPServer(("127.0.0.1", 0), _StubHandler)
        self._httpd.state = self.state
        self._thread = threading.Thread(target=self._httpd.serve_forever, daemon=True)
        self.port = self._httpd.server_address[1]
    def url(self, query=""):
        q = ("?" + query) if query else ""
        return "http://127.0.0.1:%d%s%s" % (self.port, self.state.mount, q)
    def start(self):
        self._thread.start()
        return self
    def stop(self):
        self._httpd.shutdown()
        self._httpd.server_close()
    def __enter__(self):
        return self.start()
    def __exit__(self, *args):
        self.stop()
        return False
    def recs(self, method=None):
        with self.state.lock:
            recs = list(self.state.records)
        if method:
            recs = [r for r in recs if r["method"] == method]
        return recs
def _read_len(handler):
    n = int(handler.headers.get("Content-Length") or 0)
    return handler.rfile.read(n) if n else b""
class SilentHandler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"
    def log_message(self, fmt, *args):
        return
class _PortServer(object):
    def __init__(self, handler):
        self._httpd = ThreadingHTTPServer(("127.0.0.1", 0), handler)
        self.port = self._httpd.server_address[1]
        self._thread = threading.Thread(target=self._httpd.serve_forever, daemon=True)
    def start(self):
        self._thread.start()
        return self
    def stop(self):
        self._httpd.shutdown()
        self._httpd.server_close()
    @property
    def url(self):
        return "http://127.0.0.1:%d" % self.port
def raw_http(post_fn, delete_fn=None):
    class H(SilentHandler):
        def do_POST(self):
            post_fn(self, _read_len(self))
        def do_DELETE(self):
            body = _read_len(self)
            if delete_fn:
                return delete_fn(self, body)
            self.send_response(200)
            self.send_header("Content-Length", "0")
            self.end_headers()
    srv = _PortServer(H).start()
    return srv, "http://127.0.0.1:%d/mcp" % srv.port
class SecondListener(_PortServer):
    def __init__(self):
        self.hits = 0
        self.methods = []
        self.lock = threading.Lock()
        outer = self
        class H(SilentHandler):
            def _hit(self, method):
                with outer.lock:
                    outer.hits += 1
                    outer.methods.append(method)
                if method == "HEAD":
                    self.send_response(200)
                    self.end_headers()
                    return
                self.send_response(200)
                self.send_header("Content-Length", "0")
                self.end_headers()
            def do_POST(self):
                _read_len(self)
                self._hit("POST")
            def do_HEAD(self):
                self._hit("HEAD")
        for name, method in (("do_GET", "GET"), ("do_PUT", "PUT"), ("do_DELETE", "DELETE")):
            setattr(H, name, lambda self, method=method: self._hit(method))
        _PortServer.__init__(self, H)
    @property
    def url(self):
        return "http://127.0.0.1:%d/redirected" % self.port
class DummyProxy(_PortServer):
    def __init__(self):
        self.hits = 0
        self.lock = threading.Lock()
        outer = self
        class H(SilentHandler):
            def _hit(self, *a, **k):
                with outer.lock:
                    outer.hits += 1
                _read_len(self)
                self.send_response(502)
                self.send_header("Content-Length", "0")
                self.end_headers()
        for name in ("do_GET", "do_POST", "do_CONNECT", "do_HEAD", "do_PUT", "do_DELETE"):
            setattr(H, name, H._hit)
        _PortServer.__init__(self, H)
class RecordingProxy(_PortServer):
    def __init__(self):
        self.hits = 0
        self.paths = []
        self.lock = threading.Lock()
        outer = self
        class H(SilentHandler):
            def do_POST(self):
                with outer.lock:
                    outer.hits += 1
                    outer.paths.append(self.path)
                body = _read_len(self)
                target = urlparse(self.path)
                try:
                    sock = socket.create_connection((target.hostname, target.port or 80), timeout=5)
                    path = target.path or "/"
                    if target.query:
                        path += "?" + target.query
                    hdrs = ["POST %s HTTP/1.1" % path, "Host: %s" % target.netloc]
                    for k, v in self.headers.items():
                        if k.lower() not in ("host", "proxy-connection"):
                            hdrs.append("%s: %s" % (k, v))
                    sock.sendall(("\r\n".join(hdrs) + "\r\n\r\n").encode("iso-8859-1") + body)
                    sock.settimeout(5)
                    data = b""
                    while True:
                        chunk = sock.recv(65536)
                        if not chunk:
                            break
                        data += chunk
                    sock.close()
                except Exception:
                    self.send_response(502)
                    self.send_header("Content-Length", "0")
                    self.end_headers()
                    return
                self.connection.sendall(data)
        _PortServer.__init__(self, H)
def _assert_trickle_timeout(test, stub, url, proxy_args=(), env=None):
    proc = run_cli(["--url", url, "--timeout", "0.3"] + list(proxy_args) + ["tools"], env=env)
    end = time.monotonic()
    test.assertNotEqual(proc.returncode, 0)
    test.assertEqual(load_json(proc)["error"]["type"], "timeout", proc.stdout + proc.stderr)
    test.assertGreaterEqual(stub.state.trickle_bytes, 2)
    elapsed = end - stub.recs("POST")[0]["received"]
    test.assertLess(elapsed, 0.5, "POST to exit %.3fs" % elapsed)

class LabctlTests(unittest.TestCase):
    def _run(self, stub, extra_args, env=None, timeout=20):
        args = ["--url", stub.url(), "--timeout", "5"] + extra_args
        return run_cli(args, env=env, timeout=timeout)
    def test_json_initialize_list_call(self):
        with StubServer() as stub:
            proc = self._run(stub, ["call", "labmail__mail_state_get", "--json", "{}"])
            self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
            out = load_json(proc)
            self.assertEqual(out.get("content")[0]["text"], "ok")
            methods = [r["body"] and json.loads(r["body"]).get("method") for r in stub.recs("POST")]
            self.assertIn("initialize", methods)
            self.assertIn("notifications/initialized", methods)
            self.assertIn("tools/call", methods)
            self.assertTrue(any(r["method"] == "DELETE" for r in stub.recs()))
    def test_sse_initialize_list_call_and_hold(self):
        for chunked in (False, True):
            with StubServer() as stub:
                stub.state.mode, stub.state.chunked, stub.state.prepend_sse_event, stub.state.hold_after_sse = "sse", chunked, True, 1.2
                t0 = time.monotonic()
                proc = self._run(stub, ["tools"])
                elapsed = time.monotonic() - t0
                self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
                self.assertLess(elapsed, 1.0)
                tools = load_json(proc)["tools"]
                self.assertTrue(any(t["name"] == "labinfo__endpoints_list" for t in tools))
    def test_handshake_and_empty_202_on_id(self):
        with StubServer() as stub:
            proc = self._run(stub, ["tools"])
            self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
            inits = [json.loads(r["body"]) for r in stub.recs("POST") if r["body"] and json.loads(r["body"]).get("method") == "initialize"]
            self.assertEqual(inits[0]["params"]["capabilities"], {})
            self.assertTrue(inits[0]["params"]["clientInfo"]["name"])
            self.assertTrue(inits[0]["params"]["clientInfo"]["version"])
            notes = [json.loads(r["body"]) for r in stub.recs("POST") if r["body"] and json.loads(r["body"]).get("method") == "notifications/initialized"]
            self.assertNotIn("id", notes[0])
        def post202(handler, body):
            handler.send_response(202); handler.send_header("Content-Length", "0"); handler.end_headers()
        srv, url = raw_http(post202)
        try:
            proc = run_cli(["--url", url, "tools"], timeout=8)
            self.assertNotEqual(proc.returncode, 0)
            self.assertIn("202", proc.stdout)
        finally:
            srv.stop()
    def test_mismatched_id_and_bad_version(self):
        def post_wrong(handler, body, rid):
            msg = json.loads(body.decode("utf-8"))
            result = {"protocolVersion": PROTO, "capabilities": {}, "serverInfo": {"name": "s", "version": "0"}} if msg.get("method") == "initialize" else {}
            payload = {"jsonrpc": "2.0", "result": result}
            if rid is not None:
                payload["id"] = rid
            raw = json.dumps(payload).encode("utf-8")
            handler.send_response(200)
            for k, v in (("Content-Type", "application/json"), ("Mcp-Session-Id", "s"), ("Content-Length", str(len(raw)))):
                handler.send_header(k, v)
            handler.end_headers()
            handler.wfile.write(raw)
        for rid in (999, None, True):
            srv, url = raw_http(lambda h, b, rid=rid: post_wrong(h, b, rid))
            try:
                proc = run_cli(["--url", url, "tools"], timeout=8)
                self.assertNotEqual(proc.returncode, 0)
                self.assertIn("does not match", proc.stdout)
            finally:
                srv.stop()
        with StubServer() as stub:
            stub.state.protocol = "2026-07-28"
            proc = self._run(stub, ["tools"])
            self.assertNotEqual(proc.returncode, 0)
            self.assertIn("unsupported protocol version", proc.stdout)
    def test_deadlines_trickle_process_exit(self):
        for kind in ("status", "headers", "body", "sse"):
            with self.subTest(kind=kind), StubServer() as stub:
                stub.state.trickle = kind
                _assert_trickle_timeout(self, stub, stub.url())
    def test_delayed_dns_process_exit(self):
        timeout, slack = 0.3, 1.5
        code = "import socket,sys,time\nsys.path.insert(0, %r)\nimport labctl\nlabctl._getaddrinfo=lambda *a,**k:(time.sleep(5) or socket.getaddrinfo('127.0.0.1',1,0,socket.SOCK_STREAM))\nsys.argv=['labctl','--url','http://example.invalid:9/mcp','--timeout',%r,'tools']\nsys.exit(labctl.main())\n" % (str(HERE), str(timeout))
        t0 = time.monotonic()
        denv = _clean_env(); denv["PYTHONDONTWRITEBYTECODE"] = "1"
        proc = subprocess.run([sys.executable, "-B", "-c", code], stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=denv, timeout=8, universal_newlines=True)
        elapsed = time.monotonic() - t0
        self.assertNotEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertIn("timeout", proc.stdout)
        self.assertLess(elapsed, timeout + slack)
    def test_pagination_and_schema_and_filter(self):
        page2 = [_tool("labgraph__scenario_list", "scenarios")]
        page2[0]["inputSchema"] = {"type": "object", "properties": {"x": {"type": "string"}}}
        with StubServer() as stub:
            stub.state.pages = [_default_tools(), page2]
            proc = self._run(stub, ["tools", "--server", "labgraph"])
            self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
            tools = load_json(proc)["tools"]
            self.assertEqual(len(tools), 1)
            self.assertEqual(tools[0]["name"], "labgraph__scenario_list")
            proc = self._run(stub, ["schema", "labgraph__scenario_list"])
            self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
            self.assertEqual(load_json(proc)["properties"]["x"]["type"], "string")
            proc = self._run(stub, ["schema", "nope__missing"])
            self.assertNotEqual(proc.returncode, 0)
    def test_repeating_cursor_named(self):
        with StubServer() as stub:
            stub.state.repeat_cursor = "loop-me"
            proc = self._run(stub, ["tools"])
            self.assertNotEqual(proc.returncode, 0, proc.stdout)
            self.assertIn("loop-me", proc.stdout)
            self.assertIn("repeated", proc.stdout.lower())
    def test_session_required_and_endpoints_and_args(self):
        with StubServer() as stub:
            proc = self._run(stub, ["endpoints"])
            self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
            calls = [json.loads(r["body"]) for r in stub.recs("POST") if r["body"] and json.loads(r["body"]).get("method") == "tools/call"]
            self.assertEqual(calls[0]["params"]["name"], "labinfo__endpoints_list")
            for rec in stub.recs("POST"):
                msg = json.loads(rec["body"]) if rec["body"] else {}
                if msg.get("method") != "initialize":
                    self.assertEqual(rec["headers"].get("mcp-session-id"), stub.state.session)
        with StubServer() as stub:
            stub.state.call_result = {"content": [{"type": "text", "text": "from-arg"}]}
            proc = self._run(stub, ["call", "labmail__mail_state_get", "--arg", "id=alice", "--arg", "n=1"])
            self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
            calls = [json.loads(r["body"]) for r in stub.recs("POST") if r["body"] and json.loads(r["body"]).get("method") == "tools/call"]
            self.assertEqual(calls[0]["params"]["arguments"]["id"], "alice")
            self.assertEqual(calls[0]["params"]["arguments"]["n"], 1)
        with tempfile.TemporaryDirectory() as td:
            path = str(Path(td) / "args.json")
            Path(path).write_text('{"k":"v"}', encoding="utf-8")
            with StubServer() as stub:
                proc = self._run(stub, ["call", "labmail__mail_state_get", "--json", "@" + path])
                self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
                calls = [json.loads(r["body"]) for r in stub.recs("POST") if r["body"] and json.loads(r["body"]).get("method") == "tools/call"]
                self.assertEqual(calls[0]["params"]["arguments"]["k"], "v")
        with StubServer() as stub:
            proc = self._run(stub, ["call", "t", "--json", "{}", "--arg", "k=v"])
            self.assertNotEqual(proc.returncode, 0)
            self.assertIn("do not mix", proc.stdout)
    def test_errors_redaction_and_tokens(self):
        with StubServer() as stub:
            stub.state.call_is_error = True
            stub.state.call_result = {"isError": True, "content": [{"type": "text", "text": "failed " + SENTINEL}]}
            proc = run_cli(
                ["--url", stub.url(), "call", "labmail__mail_state_get", "--json", "{}"],
                env={"LABCTL_TOKEN": SENTINEL},
            )
            self.assertNotEqual(proc.returncode, 0)
            self.assertNotIn(SENTINEL, proc.stdout + proc.stderr)
            self.assertIn("***", proc.stdout)
        with StubServer() as stub:
            stub.state.call_rpc_error = {"code": -32000, "message": "rpc " + SENTINEL}
            proc = run_cli(
                ["--url", stub.url(), "call", "labmail__mail_state_get", "--json", "{}"],
                env={"LABCTL_TOKEN": SENTINEL},
            )
            self.assertNotEqual(proc.returncode, 0)
            self.assertNotIn(SENTINEL, proc.stdout + proc.stderr)
        with StubServer() as stub:
            stub.state.http_status = 500
            stub.state.http_body = ("server exploded " + SENTINEL).encode("utf-8")
            proc = run_cli(["--url", stub.url(), "tools"], env={"LABCTL_TOKEN": SENTINEL})
            self.assertNotEqual(proc.returncode, 0)
            self.assertNotIn(SENTINEL, proc.stdout + proc.stderr)
            self.assertIn("***", proc.stdout)
        proc = run_cli(["--url", "http://127.0.0.1:1/mcp", "tools"], env={"LABCTL_TOKEN": SENTINEL})
        self.assertNotEqual(proc.returncode, 0)
        self.assertNotIn(SENTINEL, proc.stdout + proc.stderr)
        with StubServer() as stub:
            stub.state.require_bearer = SENTINEL
            proc = run_cli(["--url", stub.url(), "tools"], env={"LABCTL_TOKEN": SENTINEL})
            self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        with tempfile.TemporaryDirectory() as td:
            path, other = str(Path(td) / "tok"), str(Path(td) / "other")
            Path(path).write_text(SENTINEL + "\n", encoding="utf-8")
            Path(other).write_text("wrong-token\n", encoding="utf-8")
            for args, env in (
                (["--token-file", path, "tools"], None),
                (["tools"], {"LABCTL_TOKEN_FILE": path}),
                (["tools"], {"LABCTL_TOKEN": SENTINEL, "LABCTL_TOKEN_FILE": other}),
            ):
                with StubServer() as stub:
                    stub.state.require_bearer = SENTINEL
                    proc = run_cli(["--url", stub.url()] + args, env=env)
                    self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        with StubServer() as stub:
            proc = self._run(stub, ["tools"])
            self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
            auths = [r["headers"].get("authorization") for r in stub.recs("POST")]
            self.assertTrue(all(a is None for a in auths))
    def test_token_flags_rejected(self):
        for args in (
            ["--token", SENTINEL, "--url", "http://127.0.0.1:1/mcp", "tools"],
            ["--url", "http://127.0.0.1:1/mcp", "tools", "--token", SENTINEL],
            ["--bearer", SENTINEL, "--url", "http://127.0.0.1:1/mcp", "tools"],
            ["--url", "http://127.0.0.1:1/mcp", "call", "t", "--bearer", SENTINEL],
            ["--token=" + SENTINEL, "tools"],
        ):
            proc = run_cli(args)
            blob = proc.stdout + proc.stderr
            self.assertNotEqual(proc.returncode, 0, args)
            self.assertNotIn(SENTINEL, blob)
            self.assertTrue("--token" in blob or "--bearer" in blob or "unrecognized" in blob)
    def test_post_302_not_followed(self):
        second = SecondListener().start()
        try:
            with StubServer() as stub:
                stub.state.redirect_to = second.url
                proc = run_cli(
                    ["--url", stub.url(), "tools"],
                    env={"LABCTL_TOKEN": SENTINEL},
                )
                self.assertNotEqual(proc.returncode, 0)
                self.assertIn("302", proc.stdout)
                self.assertEqual(second.hits, 0, second.methods)
                self.assertNotIn(SENTINEL, proc.stdout + proc.stderr)
                auths = [r["headers"].get("authorization") for r in stub.recs("POST")]
                self.assertIn("Bearer " + SENTINEL, auths)
        finally:
            second.stop()
    def test_group_path_and_query_and_delete(self):
        with StubServer(mount="/v0/groups/integration/mcp") as stub:
            url = stub.url(query="q=lab")
            proc = run_cli(["--url", url, "tools"])
            self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
            proc = run_cli(["--url", url, "call", "labmail__mail_state_get", "--json", "{}"], env={"LABCTL_TOKEN": SENTINEL})
            self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
            paths = [r["path"] for r in stub.recs()]
            self.assertTrue(paths)
            for path in paths:
                self.assertTrue(path.startswith("/v0/groups/integration/mcp"), path)
                self.assertIn("q=lab", path)
                self.assertFalse(path.startswith("/mcp?") or path == "/mcp")
            for rec in stub.recs():
                if rec["method"] == "DELETE" or json.loads(rec["body"])["method"] != "initialize":
                    self.assertEqual(rec["headers"].get("mcp-protocol-version"), PROTO)
            authed = [r for r in stub.recs("DELETE") if r["headers"].get("authorization")]
            self.assertTrue(authed)
            for rec in authed:
                self.assertEqual(rec["headers"].get("mcp-session-id"), stub.state.session)
                self.assertEqual(rec["headers"].get("authorization"), "Bearer " + SENTINEL)
            self.assertTrue(any(r["method"] == "POST" and json.loads(r["body"]).get("method") == "initialize" for r in stub.recs("POST")))
        with StubServer(mount="/mcp;view=compact") as stub:
            url = "http://127.0.0.1:%d/mcp;view=compact?q=lab" % stub.port
            proc = run_cli(["--url", url, "tools"])
            self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
            for rec in stub.recs():
                self.assertTrue(rec["path"].startswith("/mcp;view=compact"), rec["path"])
                self.assertIn("q=lab", rec["path"])
        with StubServer() as stub:
            stub.state.delete_status = 405
            self.assertEqual(self._run(stub, ["tools"]).returncode, 0)
            stub.state.call_is_error = True
            stub.state.call_result = {"isError": True, "content": []}
            self.assertNotEqual(self._run(stub, ["call", "labmail__mail_state_get", "--json", "{}"]).returncode, 0)
    def test_ambient_proxy_ignored_and_opt_in(self):
        dummy = DummyProxy().start()
        try:
            with StubServer() as stub:
                stub.state.require_bearer = SENTINEL
                env = {k: dummy.url for k in PROXY_ENV}
                env["LABCTL_TOKEN"] = SENTINEL
                proc = run_cli(["--url", stub.url(), "tools"], env=env)
                self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
                self.assertEqual(dummy.hits, 0)
                self.assertTrue(stub.recs("POST"))
        finally:
            dummy.stop()
        rec = RecordingProxy().start()
        try:
            with StubServer() as stub:
                before = rec.hits
                proc = run_cli(["--url", stub.url(), "--proxy", rec.url, "tools"])
                self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
                self.assertGreater(rec.hits, before)
            with StubServer() as stub:
                before = rec.hits
                proc = run_cli(["--url", stub.url(), "tools"], env={"LABCTL_PROXY": rec.url})
                self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
                self.assertGreater(rec.hits, before)
        finally:
            rec.stop()
        with StubServer() as stub:
            proc = run_cli(["--url", stub.url(), "--proxy", "http://example.com:8080", "tools"])
            self.assertNotEqual(proc.returncode, 0)
            self.assertIn("proxy", proc.stdout.lower())
            self.assertFalse(stub.recs())
    def test_child_env_clears_ambient_token_and_proxy(self):
        with tempfile.TemporaryDirectory() as td:
            local = str(Path(td) / "local")
            ambient = str(Path(td) / "ambient")
            Path(local).write_text("local-bearer\n", encoding="utf-8")
            Path(ambient).write_text("ambient-file\n", encoding="utf-8")
            dummy = DummyProxy().start()
            try:
                with StubServer() as stub:
                    stub.state.require_bearer = "local-bearer"
                    child = build_child_env(local_token_file=local)
                    child["LABCTL_URL"] = stub.url()
                    child["LABCTL_TOKEN"] = "ambient-bearer"
                    child["LABCTL_TOKEN_FILE"] = ambient
                    child["http_proxy"] = dummy.url
                    child["https_proxy"] = dummy.url
                    child["ALL_PROXY"] = dummy.url
                    built = build_child_env(local_token_file=local, extra={"LABCTL_TOKEN": "ambient-bearer", "http_proxy": dummy.url, "ALL_PROXY": dummy.url, "LABCTL_TOKEN_FILE": ambient})
                    self.assertEqual(built.get("LABCTL_TOKEN_FILE"), local)
                    self.assertNotIn("LABCTL_TOKEN", built)
                    self.assertNotIn("http_proxy", built)
                    proc = run_cli(["--url", stub.url(), "tools"], env=built)
                    self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
                    self.assertEqual(dummy.hits, 0)
                    auths = [r["headers"].get("authorization") for r in stub.recs("POST")]
                    self.assertTrue(all(a == "Bearer local-bearer" for a in auths if a), auths)
            finally:
                dummy.stop()
    def test_json_type_aware_equality(self):
        base = {"name": "labinfo__endpoints_list", "annotations": {"readOnlyHint": True}}
        self.assertTrue(json_equal(base, dict(base)))
        self.assertFalse(json_equal(base, {"name": "labinfo__endpoints_list", "annotations": {"readOnlyHint": 1}}))
        self.assertTrue(True == 1)
        self.assertFalse(json_equal(True, 1))
    def test_redaction_json_and_header_and_cleanup(self):
        token = "true"
        with StubServer() as stub:
            stub.state.call_result = {"content": [{"type": "text", "text": "ok"}], "flag": True}
            proc = run_cli(["--url", stub.url(), "call", "labmail__mail_state_get", "--json", "{}"], env={"LABCTL_TOKEN": token})
            self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
            json.loads(proc.stdout)
            self.assertIn("true", proc.stdout)
        slash = "abc\\def"
        with StubServer() as stub:
            stub.state.http_status = 500
            stub.state.http_body = ("err " + slash).encode("utf-8")
            proc = run_cli(["--url", stub.url(), "tools"], env={"LABCTL_TOKEN": slash})
            self.assertNotEqual(proc.returncode, 0)
            self.assertNotIn(slash, proc.stdout + proc.stderr)
            self.assertIn("***", proc.stdout)
        with tempfile.TemporaryDirectory() as td:
            path = str(Path(td) / "tok")
            Path(path).write_text("one\ntwo\n", encoding="utf-8")
            proc = run_cli(["--url", "http://127.0.0.1:1/mcp", "--token-file", path, "tools"])
            self.assertEqual(proc.returncode, 2)
            self.assertIn("line break", proc.stdout)
        def stall(handler, body):
            time.sleep(8)
            handler.send_response(200)
            handler.send_header("Content-Length", "0")
            handler.end_headers()
        srv, url = raw_http(stall)
        try:
            t0 = time.monotonic()
            proc = run_cli(["--url", url, "--timeout", "0.5", "tools"], timeout=8)
            elapsed = time.monotonic() - t0
            self.assertNotEqual(proc.returncode, 0)
            self.assertIn("timeout", proc.stdout)
            self.assertLess(elapsed, 2.0)
        finally:
            srv.stop()
    def test_sliceA_code1_folds(self):
        def _cli(stub, args, env=None):
            return run_cli(["--url", stub.url()] + args, env=env)
        with StubServer() as stub:
            stub.state.call_rpc_error = {"code": -32000, "data": {SENTINEL: "leak", "ok": 1}}
            proc = _cli(stub, ["call", "labmail__mail_state_get", "--json", "{}"], {"LABCTL_TOKEN": SENTINEL})
            data = (((load_json(proc) or {}).get("error") or {}).get("data")) or {}
            self.assertNotEqual(proc.returncode, 0)
            self.assertNotIn(SENTINEL, proc.stdout + proc.stderr)
            self.assertNotIn(SENTINEL, data)
            self.assertIn("***", data)
        slash = "abc\\def"
        with StubServer() as stub:
            stub.state.http_status, stub.state.http_body = 500, json.dumps({"message": "fail " + slash}).encode("utf-8")
            proc = _cli(stub, ["tools"], {"LABCTL_TOKEN": slash})
            self.assertNotEqual(proc.returncode, 0)
            self.assertNotIn(slash, proc.stdout + proc.stderr)
            self.assertIn("***", proc.stdout)
        with StubServer() as stub:
            stub.state.require_session, stub.state.switch_session_after_init = False, "sess-test-2"
            proc = self._run(stub, ["tools"], env={"LABCTL_TOKEN": SENTINEL})
            self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
            self.assertEqual(stub.recs("DELETE")[0]["headers"].get("mcp-session-id"), "sess-test-1")
            for rec in stub.recs("POST"):
                msg = json.loads(rec["body"]) if rec["body"] else {}
                if msg.get("method") != "initialize":
                    self.assertEqual(rec["headers"].get("mcp-session-id"), "sess-test-1")
        targets = []
        class ConnH(SilentHandler):
            def do_CONNECT(self):
                targets.append(self.path)
                self.send_response(400)
                self.send_header("Content-Length", "0")
                self.end_headers()
        probe = _PortServer(ConnH).start()
        try:
            proc = run_cli(["--url", "https://[::1]:443/mcp", "--proxy", probe.url, "--timeout", "2", "tools"])
            self.assertTrue(any(t.split()[0] == "[::1]:443" or t == "[::1]:443" for t in targets), (targets, proc.stdout))
        finally:
            probe.stop()
        def _init(rid):
            return {"jsonrpc": "2.0", "id": rid, "result": {"protocolVersion": PROTO, "capabilities": {}, "serverInfo": {"name": "s", "version": "0"}}}
        def _sse(handler, raw):
            handler.send_response(200)
            handler.send_header("Content-Type", "text/event-stream")
            handler.send_header("Mcp-Session-Id", "sess-test-1")
            handler.send_header("Content-Length", str(len(raw)))
            handler.end_headers()
            handler.wfile.write(raw)
        def post_cr(handler, body):
            msg = json.loads(body.decode("utf-8"))
            rid = msg.get("id")
            if msg.get("method") == "initialize":
                _sse(handler, ("data: %s\r\rdata: %s\r\r" % (json.dumps({"jsonrpc": "2.0", "method": "notifications/progress"}), json.dumps(_init(rid)))).encode("utf-8"))
            elif msg.get("method") == "notifications/initialized":
                handler.send_response(202)
                handler.send_header("Content-Length", "0")
                handler.end_headers()
            else:
                _sse(handler, ("data: %s\r\r" % json.dumps({"jsonrpc": "2.0", "id": rid, "result": {"tools": []}})).encode("utf-8"))
        srv, url = raw_http(post_cr)
        try:
            proc = run_cli(["--url", url, "tools"], timeout=8)
            self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
            self.assertEqual(load_json(proc)["tools"], [])
        finally:
            srv.stop()
        def post_incomplete(handler, body):
            _sse(handler, ("data: %s" % json.dumps(_init(json.loads(body.decode("utf-8")).get("id")))).encode("utf-8"))
        srv, url = raw_http(post_incomplete)
        try:
            proc = run_cli(["--url", url, "tools"], timeout=8)
            self.assertNotEqual(proc.returncode, 0)
            self.assertIn("SSE", proc.stdout)
        finally:
            srv.stop()
        with StubServer() as stub:
            stub.state.mode, stub.state.sse_inject_request = "sse", True
            self.assertEqual(self._run(stub, ["tools"]).returncode, 0)
        with StubServer() as stub:
            stub.state.omit_jsonrpc = True
            proc = self._run(stub, ["tools"])
            self.assertNotEqual(proc.returncode, 0)
            self.assertIn("jsonrpc", proc.stdout)
        labctl = _load_client()
        connects, real_sock = [], labctl.socket.socket
        labctl._getaddrinfo = lambda host, port, *a, **k: [(socket.AF_INET, socket.SOCK_STREAM, 6, "", ("203.0.113.1", int(port)))]
        labctl.socket.socket = lambda *a, **k: (_ for _ in ()).throw(AssertionError("no connect"))
        try:
            with self.assertRaises(labctl.LabctlError) as ctx:
                labctl._connect_tcp("localhost", 8080, time.monotonic() + 2, loopback_only=True)
            self.assertEqual(ctx.exception.kind, "usage_error")
            self.assertEqual(connects, [])
        finally:
            labctl.socket.socket = real_sock
        with StubServer() as stub:
            stub.state.call_result = ["not-an-object"]
            proc = self._run(stub, ["call", "labmail__mail_state_get", "--json", "{}"])
            self.assertNotEqual(proc.returncode, 0)
            self.assertIn("object", proc.stdout.lower())
        with StubServer() as stub:
            stub.state.pages = [_default_tools(), [_tool("labgraph__scenario_list", "scenarios")]]
            proc = run_cli(["--url", stub.url(), "tools"], env={"LABCTL_MAX_PAGES": "1"})
            self.assertNotEqual(proc.returncode, 0)
            self.assertIn("page", proc.stdout.lower())
        with StubServer() as stub:
            stub.state.tools = [_tool("a", "a"), _tool("b", "b")]
            proc = run_cli(["--url", stub.url(), "tools"], env={"LABCTL_MAX_TOOLS": "1"})
            self.assertNotEqual(proc.returncode, 0)
            self.assertIn("tool", proc.stdout.lower())
        with StubServer() as stub:
            stub.state.stall_delete = True
            t0 = time.monotonic()
            proc = run_cli(["--url", stub.url(), "--timeout", "0.5", "tools"], timeout=8)
            self.assertNotEqual(proc.returncode, 0, proc.stdout + proc.stderr)
            self.assertLess(time.monotonic() - t0, 2.0)
        with StubServer() as stub:
            stub.state.notify_status, stub.state.notify_body = 200, json.dumps({"jsonrpc": "2.0", "error": {"code": -1, "message": "no"}}).encode("utf-8")
            proc = self._run(stub, ["tools"])
            self.assertNotEqual(proc.returncode, 0)
            self.assertIn("202", proc.stdout)
        with StubServer() as stub:
            stub.state.list_result_override = {}
            proc = self._run(stub, ["tools"])
            self.assertNotEqual(proc.returncode, 0)
            self.assertIn("array", proc.stdout.lower())
        proc = run_cli(["--url", "http://127.0.0.1:1/mcp", "tools"], env={"LABCTL_TOKEN": "ok\x1ftok"})
        self.assertEqual(proc.returncode, 2)
        self.assertIn("line break", proc.stdout)
        proc = run_cli(["--url", "http://127.0.0.1:1/mcp", "tools"])
        self.assertNotEqual(proc.returncode, 0)
        self.assertIn("transport", proc.stdout)
        self.assertNotIn("Traceback", proc.stderr)
        for raw in ("nan", "inf", "-inf"):
            self.assertEqual(run_cli(["--url", "http://127.0.0.1:1/mcp", "--timeout", raw, "tools"]).returncode, 2, raw)
        with StubServer(mount="/mcp") as stub:
            stub.state.reject_other_paths = False
            proc = run_cli(["--url", "http://127.0.0.1:%d/mcp;?" % stub.port, "tools"])
            self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
            for rec in stub.recs():
                self.assertTrue(rec["path"].startswith("/mcp;?"), rec["path"])
        with StubServer() as stub:
            proc = self._run(stub, ["call", "labmail__mail_state_get", "--arg", "value=NaN"])
            self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
            calls = [json.loads(r["body"]) for r in stub.recs("POST") if r["body"] and json.loads(r["body"]).get("method") == "tools/call"]
            self.assertEqual(calls[0]["params"]["arguments"]["value"], "NaN")
        with StubServer() as stub:
            self.assertNotEqual(self._run(stub, ["call", "labmail__mail_state_get", "--json", '{"x": NaN}']).returncode, 0)
    def test_stdlib_only_isolated_cli(self):
        with StubServer() as stub:
            proc = run_cli(["--url", stub.url(), "tools"], isolated=True)
            self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        banned = ("requests", "httpx", "aiohttp", "urllib3", "yaml")
        for line in LABCTL.read_text(encoding="utf-8").splitlines():
            s = line.strip()
            if s.startswith("import ") or s.startswith("from "):
                self.assertNotIn(s.split()[1].split(".")[0], banned, line)
def _write_tls_certs(td):
    td = Path(td)
    def _run(args):
        subprocess.check_call(args, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    ca_cnf = td / "ca.cnf"
    ca_cnf.write_text(
        "[req]\ndistinguished_name=dn\nx509_extensions=v3_ca\nprompt=no\n"
        "[dn]\nCN=labctl-test-ca\n"
        "[v3_ca]\nbasicConstraints=critical,CA:TRUE\nkeyUsage=critical,keyCertSign,cRLSign\n"
        "subjectKeyIdentifier=hash\n",
        encoding="ascii",
    )
    leaf_cnf = td / "leaf.cnf"
    leaf_cnf.write_text(
        "[req]\ndistinguished_name=dn\nprompt=no\n[dn]\nCN=127.0.0.1\n"
        "[v3_req]\nbasicConstraints=CA:FALSE\nkeyUsage=digitalSignature,keyEncipherment\n"
        "extendedKeyUsage=serverAuth\nsubjectAltName=IP:127.0.0.1,DNS:localhost\n",
        encoding="ascii",
    )
    ca_key, ca_crt = td / "ca.key", td / "ca.crt"
    _run(["openssl", "req", "-x509", "-newkey", "rsa:2048", "-keyout", str(ca_key), "-out", str(ca_crt),
          "-days", "1", "-nodes", "-config", str(ca_cnf)])
    leaves = {}
    for name in ("origin", "proxy"):
        key, csr, crt = td / ("%s.key" % name), td / ("%s.csr" % name), td / ("%s.crt" % name)
        _run(["openssl", "req", "-newkey", "rsa:2048", "-keyout", str(key), "-out", str(csr),
              "-nodes", "-config", str(leaf_cnf)])
        _run(["openssl", "x509", "-req", "-in", str(csr), "-CA", str(ca_crt), "-CAkey", str(ca_key),
              "-CAcreateserial", "-out", str(crt), "-days", "1", "-extfile", str(leaf_cnf),
              "-extensions", "v3_req"])
        leaves[name] = (str(crt), str(key))
    return str(ca_crt), leaves["origin"], leaves["proxy"]
class _TunnelProxy(_PortServer):
    def __init__(self, cert=None, key=None):
        self.hits = 0
        self.connects = []
        self.lock = threading.Lock()
        outer = self
        class H(SilentHandler):
            def do_CONNECT(self):
                with outer.lock:
                    outer.hits += 1
                    outer.connects.append(self.path)
                host, port = self.path.rsplit(":", 1)
                try:
                    dest = socket.create_connection((host, int(port)), timeout=5)
                except OSError:
                    self.send_response(502)
                    self.send_header("Content-Length", "0")
                    self.end_headers()
                    return
                self.send_response(200, "Connection Established")
                self.end_headers()
                self.close_connection = True
                try:
                    socks = [self.connection, dest]
                    while True:
                        ready, _, failed = select.select(socks, [], socks, 8)
                        if failed or not ready:
                            break
                        for src in ready:
                            data = src.recv(65536)
                            if not data:
                                return
                            (dest if src is self.connection else self.connection).sendall(data)
                except Exception:
                    return
                finally:
                    try:
                        dest.close()
                    except Exception:
                        pass
        _PortServer.__init__(self, H)
        self.scheme = "https" if cert else "http"
        if cert:
            ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
            ctx.load_cert_chain(cert, key)
            self._httpd.socket = ctx.wrap_socket(self._httpd.socket, server_side=True)
    @property
    def url(self):
        return "%s://127.0.0.1:%d" % (self.scheme, self.port)
def _tls_stub(cert, key):
    stub = StubServer()
    ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    ctx.load_cert_chain(cert, key)
    stub._httpd.socket = ctx.wrap_socket(stub._httpd.socket, server_side=True)
    return stub
def _rpc_ok(rid, result):
    return {"jsonrpc": "2.0", "id": rid, "result": result}
def _init_result():
    return {"protocolVersion": PROTO, "capabilities": {}, "serverInfo": {"name": "s", "version": "0"}}
def _send_json(handler, payload, session="sess-test-1"):
    raw = json.dumps(payload).encode("utf-8")
    handler.send_response(200)
    handler.send_header("Content-Type", "application/json")
    handler.send_header("Mcp-Session-Id", session)
    handler.send_header("Content-Length", str(len(raw)))
    handler.end_headers()
    handler.wfile.write(raw)
def _handshake_or(handler, body, on_rpc):
    msg = json.loads(body.decode("utf-8") or "null")
    method = msg.get("method")
    rid = msg.get("id")
    if method == "initialize":
        _send_json(handler, _rpc_ok(rid, _init_result()))
        return True
    if method == "notifications/initialized":
        handler.send_response(202)
        handler.send_header("Content-Length", "0")
        handler.end_headers()
        return True
    on_rpc(handler, msg, rid)
    return False
class LabctlCode2bTests(unittest.TestCase):
    def test_redact_nested_u005c_is_bounded(self):
        token = "x-canary"
        tail = "u005c" * 15 + "u0078-canary"
        field = r"\u005c" + tail + " "
        nested = (field * (200000 // len(field)) + token).ljust(200000)
        with StubServer() as stub:
            stub.state.require_bearer = token
            stub.state.call_result = {"at_limit": "keep \\" + tail, "over_limit": nested}
            start = time.monotonic()
            proc = run_cli(["--url", stub.url(), "--timeout", "3", "call", "x"], env={"LABCTL_TOKEN": token})
            self.assertLess(time.monotonic() - start, 4.0)
        self.assertEqual(proc.returncode, 0)
        self.assertNotIn(token, proc.stdout + proc.stderr)
        result = load_json(proc)
        self.assertEqual(result["at_limit"], "keep ***")
        self.assertTrue(result["over_limit"] == "***", "expected whole-string redaction")
        labctl = _load_client()
        labctl._token_to_redact = token
        with mock.patch.object(labctl, "_unescape", wraps=labctl._unescape) as decode:
            error = labctl.LabctlError("transport", field + token)
        self.assertLessEqual(decode.call_count, 16)
        self.assertEqual(str(error), "***")
        self.assertNotIn(token, repr(error))

    def test_blocker_01_token_absent_from_repr_log_and_exception(self):
        token = "abc\\leak_tok"
        def post(handler, body):
            def on_rpc(handler, msg, rid):
                _send_json(handler, {"jsonrpc": "2.0", "id": token, "result": {"tools": []}})
            _handshake_or(handler, body, on_rpc)
        srv, url = raw_http(post)
        try:
            proc = run_cli(["--url", url, "tools"], env={"LABCTL_TOKEN": token}, timeout=8)
            blob = (proc.stdout or "") + (proc.stderr or "")
            self.assertNotEqual(proc.returncode, 0)
            self.assertIn("does not match", blob)
            self.assertNotIn(token, blob)
            self.assertNotIn(repr(token), blob)
        finally:
            srv.stop()
        labctl = _load_client()
        labctl._token_to_redact = token
        with self.assertRaises(labctl.LabctlError) as ctx:
            labctl.Client("http://127.0.0.1:1/mcp", token, 1)._rpc_message(
                {"jsonrpc": "2.0", "id": token, "result": {"tools": []}}, 1
            )
        text = "%s %r" % (ctx.exception, ctx.exception)
        self.assertNotIn(token, text)
        self.assertNotIn(repr(token), text)
    def test_nested_json_token_redaction(self):
        token = 'lab/"a\\b&c'
        inner = json.dumps({"credential": token})
        with StubServer() as stub:
            stub.state.call_result = {"content": [{"type": "text", "text": inner}], "url": quote(token, safe="")}
            args = ["--url", stub.url(), "endpoints"]
            proc = run_cli(args, env={"LABCTL_TOKEN": token})
            self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
            result = load_json(proc)
            self.assertEqual(json.loads(result["content"][0]["text"]), {"credential": "***"})
            self.assertEqual(result["url"], "***")
            stub.state.call_result["url"] = "".join("%%%02x" % b for b in token.encode())
            self.assertEqual(load_json(run_cli(args, env={"LABCTL_TOKEN": token}))["url"], "***")
            stub.state.call_rpc_error = {"code": -1, "message": inner + quote(token) + "X" * 6000}
            proc = run_cli(args, env={"LABCTL_TOKEN": token})
            self.assertNotEqual(proc.returncode, 0)
            message = load_json(proc)["error"]["message"]
            self.assertTrue(message.startswith('{"credential": "***"}***'))
            self.assertLess(len(message), 1500)

            stub.state.http_status = 500
            stub.state.http_body = json.dumps(stub.state.call_result).encode()
            proc = run_cli(args, env={"LABCTL_TOKEN": token})
            self.assertNotEqual(proc.returncode, 0)
            diagnostic = json.loads(load_json(proc)["error"]["message"].split("HTTP 500: ")[1])
            self.assertEqual(json.loads(diagnostic["content"][0]["text"]), {"credential": "***"})

    def test_blocker_02_ctl_rejected_before_request_built(self):
        with StubServer() as stub:
            url = "http://127.0.0.1:%d/mcp\r\nX-Injected: 1" % stub.port
            proc = run_cli(["--url", url, "tools"], timeout=8)
            self.assertEqual(proc.returncode, 2, proc.stdout + proc.stderr)
            self.assertIn("single line", proc.stdout)
            self.assertFalse(stub.recs())
        with StubServer() as stub:
            stub.state.reject_other_paths = False
            url = "http://127.0.0.1:%d?view=/one" % stub.port
            proc = run_cli(["--url", url, "tools"], timeout=8)
            self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
            self.assertTrue(stub.recs())
            for rec in stub.recs():
                self.assertTrue(rec["path"].startswith("/?view=/one"), rec["path"])
    def test_proxy_and_session_ctl_rejected(self):
        with StubServer() as stub:
            proxy = stub.url().replace("127.0", "127.\t0")
            proc = run_cli(["--url", stub.url(), "--proxy", proxy, "tools"])
            self.assertEqual(proc.returncode, 2)
            self.assertFalse(stub.recs())
            for ctl in ("\t", "\x1f"):
                stub.state.session = "session" + ctl
                proc = run_cli(["--url", stub.url(), "tools"])
                self.assertNotEqual(proc.returncode, 0)
                self.assertEqual(load_json(proc)["error"]["type"], "transport")
            self.assertEqual([json.loads(rec["body"])["method"] for rec in stub.recs()], ["initialize"] * 2)

    def test_blocker_03_split_terminator_one_byte_at_a_time(self):
        def post(handler, body):
            def on_rpc(handler, msg, rid):
                raw = json.dumps(_rpc_ok(rid, {"tools": []})).encode()
                handler.wfile.write(b"HTTP/1.1 200 OK\r\nContent-Type: application/json\r\n"
                                    b"Transfer-Encoding: chunked\r\nTrailer: X-End\r\n\r\n")
                chunks = b"%x\r\n%s\r\n0\r\nX-End: yes\r\n" % (len(raw), raw)
                if not truncate:
                    chunks += b"\r\n"
                for byte in chunks:
                    handler.wfile.write(bytes([byte]))
                    time.sleep(0)
                handler.close_connection = True
            _handshake_or(handler, body, on_rpc)
        srv, url = raw_http(post)
        try:
            for truncate in (False, True):
                proc = run_cli(["--url", url, "tools"])
                if truncate:
                    self.assertNotEqual(proc.returncode, 0)
                    self.assertEqual(load_json(proc)["error"]["type"], "transport")
                else:
                    self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
                    self.assertEqual(load_json(proc)["tools"], [])
        finally:
            srv.stop()

    def test_one_byte_reads_and_late_recv(self):
        labctl = _load_client()
        wire = iter(b"HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n1\r\nz\r\n0\r\nX-End: yes\r\n\r\n")
        raw = mock.Mock()
        def read_byte(buf):
            buf[0] = next(wire)
            return 1
        raw.recv_into.side_effect = read_byte
        with mock.patch.object(labctl, "time") as clock:
            clock.monotonic.return_value = 1
            sock = labctl._DeadlineSocket(raw, 5)
            with labctl._Response(sock) as response:
                response.begin()
                self.assertEqual(labctl._read_body(response), b"z")
                self.assertEqual(list(wire), [])
            def late_read(size):
                clock.monotonic.return_value = 6
                return b"x"
            raw.recv.side_effect = late_read
            with self.assertRaises(labctl.DeadlineExceeded):
                sock.recv(1)
            raw.settimeout.assert_called_with(4)

    def test_blocker_04_cr_event_dispatched_body_held_open(self):
        release = threading.Event()
        def post(handler, body):
            def on_rpc(handler, msg, rid):
                raw = ("data: " + json.dumps(_rpc_ok(rid, {"tools": []}))).encode() + eol * 2
                handler.wfile.write(b"HTTP/1.1 200 OK\r\nContent-Type: text/event-stream\r\n"
                                    b"Transfer-Encoding: chunked\r\n\r\n")
                handler.wfile.write(b"%x\r\n%s\r\n%s" % (len(raw), raw, ending))
                release.wait(3)
            _handshake_or(handler, body, on_rpc)
        srv, url = raw_http(post)
        try:
            for eol in (b"\r", b"\n", b"\r\n"):
                for ending in (b"", b"0\r\nX-End: yes\r\n"):
                    proc = run_cli(["--url", url, "--timeout", "1", "tools"])
                    self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
                    self.assertEqual(load_json(proc)["tools"], [])
        finally:
            release.set()
            srv.stop()

    def test_blocker_05_one_deadline_covers_cleanup(self):
        with StubServer() as stub:
            stub.state.stall_delete = True
            stub.state.list_delay = 0.4
            proc = run_cli(["--url", stub.url(), "--timeout", "0.8", "tools"])
            self.assertNotEqual(proc.returncode, 0)
            self.assertEqual(load_json(proc)["error"]["type"], "timeout")
            self.assertTrue(stub.state.delete_closed.wait(1))
            delete_start = stub.recs("DELETE")[0]["received"]
            used = delete_start - stub.recs()[0]["received"]
            self.assertGreaterEqual(used, stub.state.list_delay)
            self.assertLess(stub.state.delete_end - delete_start, 0.8 - used + 0.1)
        with StubServer() as stub:
            stub.state.delete_status = 405
            proc = run_cli(["--url", stub.url(), "--timeout", "5", "tools"], timeout=8)
            self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        labctl = _load_client()
        for session in ("sess-test-1", None):
            with StubServer() as stub, mock.patch.object(labctl, "time") as clock:
                clock.monotonic.return_value = 10
                stub.state.session = session
                client = labctl.Client(stub.url(), None, 5)
                client.initialize()
                client.notify_initialized()
                self.assertTrue(client.list_tools())
                clock.monotonic.return_value = 16
                with self.assertRaises(labctl.DeadlineExceeded):
                    client.close()
                self.assertFalse(stub.recs("DELETE"))
    def test_blocker_06_nonempty_202_rejected(self):
        with StubServer() as stub:
            stub.state.notify_status, stub.state.notify_body = 202, b"nope\n"
            proc = run_cli(["--url", stub.url(), "tools"], timeout=8)
            self.assertNotEqual(proc.returncode, 0)
            blob = (proc.stdout or "") + (proc.stderr or "")
            self.assertIn("202", blob)
            self.assertIn("nonempty", blob.lower())
            self.assertIsNone(load_json(proc) if proc.returncode == 0 else None)
            self.assertFalse(any(
                r["body"] and json.loads(r["body"]).get("method") == "tools/list" for r in stub.recs("POST")
            ))
    def test_blocker_07_cursor_type_before_pagination(self):
        for cursor in (False, ["page-2"], None):
            with self.subTest(cursor=cursor), StubServer() as stub:
                stub.state.list_result_override = {"tools": [_default_tools()[0]], "nextCursor": cursor}
                proc = run_cli(["--url", stub.url(), "tools"])
                self.assertNotEqual(proc.returncode, 0)
                self.assertIn("nextCursor must be a string", proc.stdout)

    def test_blocker_08_nonfinite_number(self):
        def post(handler, body):
            def on_rpc(handler, msg, rid):
                raw = ('{"jsonrpc":"2.0","id":%d,"result":{"isError":%s,"n":1e400}}' % (rid, flag)).encode()
                handler.send_response(200)
                handler.send_header("Content-Type", "application/json")
                handler.send_header("Content-Length", str(len(raw)))
                handler.end_headers()
                handler.wfile.write(raw)
            _handshake_or(handler, body, on_rpc)
        srv, url = raw_http(post)
        try:
            for flag in ("false", "true"):
                proc = run_cli(["--url", url, "call", "labmail__mail_state_get"])
                self.assertNotEqual(proc.returncode, 0)
                self.assertIn("non-finite", load_json(proc)["error"]["message"])
                self.assertNotIn("null", proc.stdout)
                self.assertNotIn("Traceback", proc.stderr)
        finally:
            srv.stop()

    def test_sse_call_success_and_failure(self):
        with StubServer() as stub:
            stub.state.mode = "sse"
            for is_error in (False, True):
                stub.state.call_is_error = is_error
                proc = run_cli(["--url", stub.url(), "call", "labmail__mail_state_get"])
                self.assertEqual(proc.returncode, int(is_error), proc.stdout + proc.stderr)
                result = load_json(proc)
                self.assertEqual(result["content"], stub.state.call_result["content"])
                self.assertEqual(result.get("isError", False), is_error)

    def test_response_contract(self):
        def post(handler, body):
            handler.wfile.write(prefix)
            if valid:
                handler.protocol_version = version
                _handshake_or(handler, body, lambda h, msg, rid: _send_json(h, _rpc_ok(rid, {"tools": []})))
            handler.close_connection = True
        def delete(handler, body):
            handler.wfile.write(b"HTTP/1.1 204 No Content\r\n" + misleading + b"\r\n\r\n")
            release.wait(3)
        release = threading.Event()
        srv, url = raw_http(post, delete)
        try:
            valid = True
            prefix = b"HTTP/1.1 100 Continue\r\n\r\nHTTP/1.1 103 Early Hints\r\nLink: </mcp>\r\n\r\n"
            for version, misleading in (("HTTP/1.0", b"Content-Length: 9999"), ("HTTP/1.1", b"Transfer-Encoding: chunked")):
                proc = run_cli(["--url", url, "--timeout", "1", "tools"])
                self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
                self.assertEqual(load_json(proc), {"tools": []})
            valid = False
            for status, headers, message in (
                (b"HTTP/0.9 200 OK", b"", "version"), (b"HTTP/2 200 OK", b"", "version"),
                (b"HTTP/1.2 200 OK", b"", "version"), (b"HTTP/1.1 101 Switching", b"", "101"),
                (b"HTTP/1.1 600 Unexpected", b"", "600"),
                (b"HTTP/1.1 200 OK", b"Transfer-Encoding: gzip\r\n", "encoding"),
                (b"HTTP/1.1 200 OK", b"", "Content-Length"),
            ):
                prefix = status + b"\r\n" + headers + b"\r\n"
                proc = run_cli(["--url", url, "tools"])
                self.assertNotEqual(proc.returncode, 0)
                self.assertIn(message, load_json(proc)["error"]["message"])
        finally:
            release.set()
            srv.stop()

    def test_public_proxy_guard(self):
        labctl = _load_client()
        connects = []
        real_sock = labctl.socket.socket
        labctl._getaddrinfo = lambda host, port, *a, **k: [
            (socket.AF_INET, socket.SOCK_STREAM, 6, "", ("203.0.113.1", int(port)))
        ]
        def _wrapped(*a, **k):
            sock = real_sock(*a, **k)
            orig = sock.connect
            def connect(addr):
                connects.append(addr)
                raise OSError("blocked public connect")
            sock.connect = connect
            return sock
        labctl.socket.socket = _wrapped
        try:
            with self.assertRaises(labctl.LabctlError) as ctx:
                client = labctl.Client("http://127.0.0.1:9/mcp", None, 2, proxy_url="http://localhost:8080")
                client.initialize()
            self.assertEqual(ctx.exception.kind, "usage_error")
            self.assertEqual(connects, [])
        finally:
            labctl.socket.socket = real_sock
class LabctlFold0039Tests(unittest.TestCase):
    def rpc_server(self, reply, delete=None):
        def post(handler, body):
            _handshake_or(handler, body, lambda h, msg, rid: reply(h, rid))
        srv, url = raw_http(post, delete)
        self.addCleanup(srv.stop)
        return url

    def assert_transport(self, proc, message):
        self.assertNotEqual(proc.returncode, 0)
        error = load_json(proc)["error"]
        self.assertEqual(error["type"], "transport")
        self.assertIn(message.lower(), error["message"].lower())

    def test_bearer_in_independently_escaped_json(self):
        token = 'key/"q\\z&'
        escaped = json.dumps(token)[1:-1]
        encoded = [token, escaped, escaped.replace("/", r"\/"), r'k\u0065y\/\"q\\z\u0026']
        expected = ["***"] * len(encoded)
        nested, masked = '{"auth":"%s"}' % encoded[-1], '{"auth":"***"}'
        for _ in range(5):
            encoded.append(nested)
            expected.append(masked)
            nested, masked = (json.dumps({"nested": text}) for text in (nested, masked))
        labctl = _load_client()
        labctl._token_to_redact = token
        with StubServer() as stub:
            for is_error in (False, True):
                stub.state.call_is_error = is_error
                stub.state.call_result = {"forms": encoded}
                proc = run_cli(["--url", stub.url(), "endpoints"], env={"LABCTL_TOKEN": token})
                with self.subTest(is_error=is_error):
                    self.assertEqual(proc.returncode, int(is_error))
                    self.assertEqual(load_json(proc)["forms"], expected)
                    self.assertEqual(proc.stderr, "")
        for text, masked in zip(encoded, expected):
            exc = labctl.LabctlError("transport", text, extra={"detail": text})
            with self.subTest(text=text):
                self.assertEqual(str(exc), masked)
                self.assertEqual(repr(exc), repr(labctl.LabctlError("transport", masked)))
                self.assertEqual(exc.extra, {"detail": masked})

    def test_userinfo_rejected_before_target_or_proxy_request(self):
        with StubServer() as stub:
            url = stub.url()
            for userinfo in ("user:secret@", "user@", "@"):
                invalid = url.replace("://", "://" + userinfo)
                for target, proxy in ((invalid, url), (url, invalid)):
                    with self.subTest(target=target, proxy=proxy):
                        proc = run_cli(["--url", target, "--proxy", proxy, "tools"])
                        self.assertEqual(proc.returncode, 2)
                        self.assertEqual(load_json(proc)["error"]["type"], "usage_error")
                        self.assertNotIn("secret", proc.stdout + proc.stderr)
                        self.assertFalse(stub.recs())

    def test_token_file_removes_one_line_ending(self):
        labctl = _load_client()
        with tempfile.TemporaryDirectory() as td, mock.patch.dict(os.environ, {"LABCTL_TOKEN": ""}):
            path = Path(td) / "bearer"
            for ending in (b"", b"\n", b"\r\n"):
                path.write_bytes(b" token " + ending)
                with self.subTest(ending=ending):
                    self.assertEqual(labctl.load_token(path), " token ")
            for invalid in (b"\x1ftoken", b"token\x1f\n", b"token\n\n", b"token\r", b"token\r\n\r\n"):
                path.write_bytes(invalid)
                with self.subTest(invalid=invalid):
                    with self.assertRaises(labctl.LabctlError) as caught:
                        labctl.load_token(path)
                    self.assertEqual(caught.exception.kind, "usage_error")

    def test_delete_issued_session_on_invalid_initialize(self):
        for bad_version, message in ((True, "unsupported protocol version"), (False, "jsonrpc")):
            with self.subTest(message=message), StubServer() as stub:
                stub.state.protocol = "unsupported" if bad_version else PROTO
                stub.state.omit_jsonrpc = not bad_version
                proc = run_cli(["--url", stub.url(), "tools"], env={"LABCTL_TOKEN": SENTINEL})
                self.assert_transport(proc, message)
                self.assertEqual(stub.state.seen_methods, ["POST", "DELETE"])
                headers = stub.recs("DELETE")[0]["headers"]
                self.assertEqual(headers["mcp-session-id"], stub.state.session)
                self.assertEqual(headers["authorization"], "Bearer " + SENTINEL)

    def test_delete_failure_warns_and_preserves_primary_outcome(self):
        def disconnect(handler, body):
            handler.close_connection = True
        def reply(handler, rid):
            _send_json(handler, _rpc_ok(rid, {"tools": []}))
        url = self.rpc_server(reply, disconnect)
        for command, code in ((["tools"], 0), (["call", "t", "--arg", "invalid"], 2)):
            with self.subTest(command=command):
                proc = run_cli(["--url", url] + command)
                self.assertEqual(proc.returncode, code)
                if code:
                    self.assertEqual(load_json(proc)["error"]["type"], "usage_error")
                else:
                    self.assertEqual(load_json(proc), {"tools": []})
                self.assertIn("warning", proc.stderr.lower())
                self.assertIn("DELETE", proc.stderr)
        labctl = _load_client()
        client = labctl.Client(url, None, 5)
        client.session_id = "issued"
        with mock.patch.object(labctl, "http_request", side_effect=ConnectionResetError("reset")), \
                mock.patch.object(sys, "stderr", new_callable=io.StringIO) as stderr:
            client.close()
            self.assertIn("reset", stderr.getvalue())

    def test_chunk_size_requires_unsigned_hex_digits(self):
        def reply(handler, rid):
            data = json.dumps(_rpc_ok(rid, {"tools": []})).encode()
            handler.wfile.write(b"HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n"
                                b"Content-Type: application/json\r\n\r\n" + size.replace(b"HEX", b"%X" % len(data)) +
                                b"\r\n" + data + b"\r\n0\r\n\r\n")
            handler.close_connection = True
        url = self.rpc_server(reply)
        size = b"HEX ;note=ok"
        self.assertEqual(load_json(run_cli(["--url", url, "tools"])), {"tools": []})
        for size in (b"+HEX", b"0xHEX", b"-1", b"HEXg"):
            with self.subTest(size=size):
                self.assert_transport(run_cli(["--url", url, "tools"]), "chunk size")

    def test_invalid_utf8_json_and_sse(self):
        def reply(handler, rid):
            value = b"ok" if prefix else b"\xff"
            raw = b'{"jsonrpc":"2.0","id":%d,"result":{"value":"%s"}}' % (rid, value)
            if media == "text/event-stream":
                raw = prefix + b"data: " + raw + b"\n\n"
            handler.send_response(200)
            handler.send_header("Content-Type", media)
            handler.send_header("Content-Length", str(len(raw)))
            handler.end_headers()
            handler.wfile.write(raw)
        url = self.rpc_server(reply)
        for media, prefix in (("application/json", b""), ("text/event-stream", b""),
                              ("text/event-stream", b": \xff\n\n")):
            with self.subTest(media=media, prefix=prefix):
                self.assert_transport(run_cli(["--url", url, "endpoints"]), "UTF-8")

    def test_rpc_requires_one_advertised_content_type(self):
        def reply(handler, rid):
            data = json.dumps(_rpc_ok(rid, {"tools": []})).encode()
            handler.send_response(200)
            for value in types:
                handler.send_header("Content-Type", value)
            handler.send_header("Content-Length", str(len(data)))
            handler.end_headers()
            handler.wfile.write(data)
        url = self.rpc_server(reply)
        types = ['application/json; charset=utf-8; note="a,b"']
        self.assertEqual(load_json(run_cli(["--url", url, "tools"])), {"tools": []})
        for types in ([], ["application/problem+json"], ["application/json-rpc"], ["test/foo+json"],
                      ["application/json", "text/plain"], ["application/json"] * 2,
                      ["application/json; charset=utf-8, text/plain"]):
            with self.subTest(types=types):
                self.assert_transport(run_cli(["--url", url, "tools"]), "Content-Type")

    def test_empty_notification_rejects_unexpected_content_type(self):
        def post(handler, body):
            if json.loads(body)["method"] != "notifications/initialized":
                return _handshake_or(handler, body, lambda h, msg, rid: _send_json(h, _rpc_ok(rid, {"tools": []})))
            handler.send_response(202)
            handler.send_header("Content-Length", "0")
            handler.send_header("Content-Type", media)
            handler.end_headers()
        srv, url = raw_http(post)
        self.addCleanup(srv.stop)
        for media in ("application/json", "text/event-stream", "application/problem+json"):
            with self.subTest(media=media):
                proc = run_cli(["--url", url, "tools"])
                if media == "application/problem+json":
                    self.assert_transport(proc, "Content-Type")
                else:
                    self.assertEqual(proc.returncode, 0, proc.stdout)

    def test_schema_rejects_non_object(self):
        with StubServer() as stub:
            for key, value in (("inputSchema", "object"), ("input_schema", []), ("inputSchema", False)):
                stub.state.tools = [{"name": "t", key: value}]
                with self.subTest(key=key, value=value):
                    proc = run_cli(["--url", stub.url(), "schema", "t"])
                    self.assertNotEqual(proc.returncode, 0)
                    self.assertIn("object", load_json(proc)["error"]["message"])

@unittest.skipUnless(shutil.which("openssl"), "TLS fixtures require openssl")
class LabctlTLSTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls._td = tempfile.TemporaryDirectory()
        cls.ca_crt, cls.origin_pair, cls.proxy_pair = _write_tls_certs(cls._td.name)
    @classmethod
    def tearDownClass(cls):
        cls._td.cleanup()
    def test_https_origin_through_http_and_https_proxies(self):
        with _tls_stub(*self.origin_pair) as stub:
            env = {"SSL_CERT_FILE": self.ca_crt, "SSL_CERT_DIR": ""}
            url = "https://127.0.0.1:%d/mcp" % stub.port
            for certs in ((), self.proxy_pair):
                proxy = _TunnelProxy(*certs).start()
                try:
                    proc = run_cli(["--url", url, "--proxy", proxy.url, "--timeout", "8", "tools"], env=env)
                    self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
                    self.assertTrue(load_json(proc)["tools"])
                    self.assertEqual(proxy.connects, ["127.0.0.1:%d" % stub.port] * 4)
                    self.assertTrue(stub.recs("POST"))
                finally:
                    proxy.stop()

    def test_nested_tls_body_trickle(self):
        proxy = _TunnelProxy(*self.proxy_pair).start()
        try:
            with _tls_stub(*self.origin_pair) as stub:
                stub.state.trickle = "body"
                url = "https://127.0.0.1:%d/mcp" % stub.port
                _assert_trickle_timeout(self, stub, url, ["--proxy", proxy.url], {"SSL_CERT_FILE": self.ca_crt})
                self.assertEqual(proxy.connects, ["127.0.0.1:%d" % stub.port])
        finally:
            proxy.stop()
def main():
    unittest.main(verbosity=2)
    return 0
if __name__ == "__main__":
    sys.exit(main() or 0)
