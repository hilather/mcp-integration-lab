#!/usr/bin/env python3
"""stdlib MCP streamable-HTTP client for the lab gateway."""
import time

# All network operations share the CLI invocation's absolute deadline.
_cli_started = time.monotonic() if __name__ == "__main__" else None

import argparse
import http.client
import io
import ipaddress
import json
import os
import re
import socket
import ssl
import sys
import threading
from array import array
from contextlib import contextmanager
from functools import lru_cache
from urllib.parse import urlsplit
CLIENT_NAME = "labctl"
CLIENT_VERSION = "0.1.0"
PROTOCOL_VERSION = "2025-11-25"
DEFAULT_TIMEOUT = 60.0
DEFAULT_MAX_PAGES = 10000
DEFAULT_MAX_TOOLS = 100000
JSONRPC = "2.0"
_BODY_LIMIT = 8 * 1024 * 1024
_DIAGNOSTIC_LIMIT = 1024
_MAX_REDACT_LAYERS = 16
_MIME_TOKEN = r"[A-Za-z0-9!#$%&'*+.^_`|~-]+"
_MIME_PARAM = _MIME_TOKEN + r'=(?:' + _MIME_TOKEN + r'|"(?:[^"\\]|\\.)*")'
_RPC_CONTENT_TYPE = re.compile(r"(application/json|text/event-stream) *(?:; *(?:" + _MIME_PARAM + r" *)?)*",
                               re.IGNORECASE | re.ASCII)
_getaddrinfo = socket.getaddrinfo
_token_to_redact = None
_rpc_id = 0
class LabctlError(Exception):
    def __init__(self, kind, message, exit_code=1, extra=None):
        message = redact(message, diagnostic=True)
        Exception.__init__(self, message)
        self.kind = kind
        self.message = message
        self.exit_code = exit_code
        self.extra = {k: _redact_obj(v, diagnostic=True) for k, v in (extra or {}).items()}
class DeadlineExceeded(LabctlError):
    def __init__(self, message="request timed out"):
        LabctlError.__init__(self, "timeout", message, exit_code=1)
def _is_finite(value):
    return not isinstance(value, float) or (value == value and value not in (float("inf"), float("-inf")))
def _reject_nonfinite(obj, where):
    if isinstance(obj, float) and not _is_finite(obj):
        raise LabctlError("usage_error", "%s contains a non-finite number" % where, exit_code=2)
    if isinstance(obj, list):
        for item in obj:
            _reject_nonfinite(item, where)
    elif isinstance(obj, dict):
        for item in obj.values():
            _reject_nonfinite(item, where)
@lru_cache(maxsize=1)
def _token_pattern(token):
    # Match raw and percent-escaped UTF-8 characters, including mixed encodings.
    uri = []
    for char in token:
        encoded = "".join("%%%02X" % byte for byte in char.encode("utf-8", "surrogatepass"))
        alternatives = ["(?i:%s)" % encoded, re.escape(char)]
        if char == " ":
            alternatives.append(r"\+")
        uri.append("(?:%s)" % "|".join(alternatives))
    return re.compile("".join(uri))

_JSON_ESCAPE = re.compile(r'\\(?:u[dD][89aAbB][0-9a-fA-F]{2}\\u[dD][c-fC-F][0-9a-fA-F]{2}|'
                          r'u[0-9a-fA-F]{4}|["\\/bfnrt])')

@lru_cache(maxsize=128)
def _unescape(escape):
    return json.loads('"' + escape + '"')

def _redact_token(text, pattern):
    if not _JSON_ESCAPE.search(text):
        return pattern.sub("***", text)
    # Each decoded character keeps its boundaries in the original string.
    # Mark matches at every depth, then edit only their original spellings.
    layer = text
    bounds = range(len(text) + 1)
    hidden = bytearray(len(text))
    for depth in range(_MAX_REDACT_LAYERS + 1):
        for match in pattern.finditer(layer):
            start, end = bounds[match.start()], bounds[match.end()]
            hidden[start:end] = b"\1" * (end - start)
        decoded = io.StringIO()
        next_bounds = array("I", [0])
        pos = 0
        for escape in _JSON_ESCAPE.finditer(layer):
            if depth == _MAX_REDACT_LAYERS:
                return "***"
            decoded.write(layer[pos:escape.start()])
            decoded.write(_unescape(escape.group()))
            next_bounds.extend(bounds[pos + 1:escape.start() + 1])
            pos = escape.end()
            next_bounds.append(bounds[pos])
        if not pos:
            break
        decoded.write(layer[pos:])
        next_bounds.extend(bounds[pos + 1:])
        layer, bounds = decoded.getvalue(), next_bounds
    parts, pos = [], 0
    for match in re.finditer(b"\1+", hidden):
        parts.extend((text[pos:match.start()], "***"))
        pos = match.end()
    return "".join(parts) + text[pos:]

def redact(text, diagnostic=False):
    if _token_to_redact:
        text = _redact_token(text, _token_pattern(_token_to_redact))
    if diagnostic and len(text) > _DIAGNOSTIC_LIMIT:
        return text[:_DIAGNOSTIC_LIMIT] + "..."
    return text

def _redact_obj(obj, diagnostic=False):
    if isinstance(obj, str):
        return redact(obj, diagnostic)
    if isinstance(obj, list):
        return [_redact_obj(item, diagnostic) for item in obj]
    if isinstance(obj, dict):
        out = {}
        for key, val in obj.items():
            base = name = redact(key, diagnostic)
            suffix = 1
            while name in out:
                suffix += 1
                name = "%s_%s" % (base, suffix)
            out[name] = _redact_obj(val, diagnostic)
        return out
    return obj

def _json_dumps(obj):
    return json.dumps(_redact_obj(obj), sort_keys=True, ensure_ascii=True, separators=(",", ":"), allow_nan=False)

def _response_number(text):
    number = float(text)
    if not _is_finite(number):
        raise LabctlError("transport", "response contains a non-finite number")
    return number

def _decode_utf8(data):
    try:
        return data.decode("utf-8")
    except UnicodeDecodeError:
        raise LabctlError("transport", "response is not valid UTF-8")

def _load_response(text):
    return json.loads(text, parse_float=_response_number, parse_constant=_response_number)

def _has_ctl(text):
    return any(ord(c) < 32 or ord(c) == 127 for c in text)
def emit(obj, exit_code=0):
    data = (_json_dumps(obj) + "\n").encode("utf-8")
    out = getattr(sys.stdout, "buffer", None)
    if out is not None:
        out.write(data)
        out.flush()
    else:
        sys.stdout.write(data.decode("ascii"))
        sys.stdout.flush()
    if exit_code:
        raise SystemExit(exit_code)
    return 0
def fail(kind, message, exit_code=1, extra=None):
    body = {"type": kind, "message": redact(message, diagnostic=True)}
    if extra:
        body.update(extra)
    emit({"error": body}, exit_code=exit_code)
def _remaining(deadline):
    rem = deadline - time.monotonic()
    if rem <= 0:
        raise DeadlineExceeded()
    return rem
def _env_positive_int(name, default):
    raw = os.environ.get(name, "")
    if raw == "":
        return default
    try:
        value = int(raw, 10)
    except ValueError:
        value = 0
    if value < 1:
        raise LabctlError("usage_error", "%s must be a positive integer" % name, exit_code=2)
    return value
def _is_loopback_host(host):
    if not host:
        return False
    h = host.strip().lower().strip("[]")
    if h in ("localhost", "localhost."):
        return True
    try:
        return ipaddress.ip_address(h).is_loopback
    except ValueError:
        return False
def _addr_is_loopback(sockaddr):
    host = sockaddr[0]
    if isinstance(host, bytes):
        host = host.decode("ascii", "replace")
    try:
        return ipaddress.ip_address(host).is_loopback
    except ValueError:
        return False
def validate_proxy_url(proxy):
    if _has_ctl(proxy):
        raise LabctlError("usage_error", "proxy URL must be a single line", exit_code=2)
    parsed = _parse_url(proxy)
    ok = parsed.scheme in ("http", "https") and parsed.hostname and (
        parsed.scheme == "https" or _is_loopback_host(parsed.hostname)
        or parsed.hostname.lower().strip("[]") in ("ip6-localhost", "ip6-loopback", "localhost", "localhost.")
    )
    if not ok:
        raise LabctlError(
            "usage_error",
            "proxy URL must be an https proxy or a loopback http/https proxy",
            exit_code=2,
        )
    return parsed
def _host_header(parsed):
    hostname = parsed.hostname
    if not hostname:
        raise LabctlError("usage_error", "URL is missing a host", exit_code=2)
    port = parsed.port
    default = 443 if parsed.scheme == "https" else 80
    host = hostname if hostname.startswith("[") or ":" not in hostname else "[" + hostname + "]"
    if port is not None and port != default:
        return "%s:%s" % (host, port)
    return host
def _target_port(parsed):
    if parsed.port is not None:
        return parsed.port
    return 443 if parsed.scheme == "https" else 80
def _parse_url(url):
    if _has_ctl(url):
        raise LabctlError("usage_error", "URL must be a single line", exit_code=2)
    try:
        parsed = urlsplit(url)
        if parsed.username is not None or parsed.password is not None:
            raise LabctlError("usage_error", "URL must not contain userinfo", exit_code=2)
        if parsed.scheme in ("http", "https") and parsed.hostname and _target_port(parsed) <= 65535:
            return parsed
    except ValueError:
        pass
    raise LabctlError("usage_error", "URL must be http or https and include a valid host and port", exit_code=2)
def _check_token(token):
    if token and _has_ctl(token):
        raise LabctlError("usage_error", "token contains a line break", exit_code=2)
    return token
def _check_session_id(sid):
    if not sid or any(ord(c) < 0x21 or ord(c) > 0x7E for c in sid):
        raise LabctlError("transport", "invalid Mcp-Session-Id")
    return sid
def _resolve(host, port, deadline):
    _remaining(deadline)
    box = {"res": None, "err": None}
    def worker():
        try:
            box["res"] = _getaddrinfo(host, port, 0, socket.SOCK_STREAM)
        except Exception as exc:
            box["err"] = exc
    thread = threading.Thread(target=worker, daemon=True)
    thread.start()
    thread.join(timeout=_remaining(deadline))
    if thread.is_alive():
        raise DeadlineExceeded()
    if box["err"] is not None:
        raise box["err"]
    if not box["res"]:
        raise LabctlError("transport", "name resolution failed for %s" % host)
    return box["res"]
def _connect_tcp(host, port, deadline, loopback_only=False):
    infos = _resolve(host, port, deadline)
    last = None
    for family, typ, proto, _canon, sockaddr in infos:
        if loopback_only and not _addr_is_loopback(sockaddr):
            last = LabctlError(
                "usage_error",
                "cleartext proxy must resolve to a loopback address",
                exit_code=2,
            )
            continue
        rem = _remaining(deadline)
        sock = socket.socket(family, typ, proto)
        try:
            sock.settimeout(rem)
            sock.connect(sockaddr)
            return sock
        except socket.timeout:
            last = DeadlineExceeded()
            sock.close()
        except OSError as exc:
            last = exc
            sock.close()
    if last is not None:
        raise last
    raise LabctlError("transport", "connection failed")

class _HTTPReader(io.BufferedReader):
    def readline(self, limit=-1):
        # http.client reads lines only for HTTP metadata. Reject EOF and CTLs
        # here, before the email header parser can discard field whitespace.
        line = super().readline(limit)
        if not line.endswith(b"\n"):
            raise LabctlError("transport", "incomplete HTTP metadata line")
        value = line[:-2] if line.endswith(b"\r\n") else line[:-1]
        if re.search(rb"[\x00-\x1f\x7f]", value):
            raise LabctlError("transport", "control character in HTTP metadata")
        return line

class _DeadlineSocket:
    def __init__(self, sock, deadline):
        self.sock = sock
        self.deadline = deadline
        self.readers = 0
        self.closed = False

    def __getattr__(self, name):
        return getattr(self.sock, name)

    def gettimeout(self):
        # HTTPSConnection's wrap_socket inherits this timeout for the handshake.
        return _remaining(self.deadline)

    def _io(self, operation, *args):
        self.sock.settimeout(self.gettimeout())
        result = operation(*args)
        _remaining(self.deadline)
        return result

    def recv(self, *args):
        return self._io(self.sock.recv, *args)

    def recv_into(self, *args):
        return self._io(self.sock.recv_into, *args)

    def sendall(self, *args):
        return self._io(self.sock.sendall, *args)

    def makefile(self, mode):
        self.readers += 1
        return _HTTPReader(socket.SocketIO(self, mode))

    def _decref_socketios(self):
        # HTTPConnection can close before its HTTPResponse has read the body.
        self.readers -= 1
        if self.closed and not self.readers:
            self.sock.close()

    def close(self):
        self.closed = True
        if not self.readers:
            self.sock.close()

class _TLSTunnel:
    def __init__(self, sock, server_hostname, context):
        self.sock = sock
        self.incoming = ssl.MemoryBIO()
        self.outgoing = ssl.MemoryBIO()
        self.tls = context.wrap_bio(self.incoming, self.outgoing, server_hostname=server_hostname)
        self._io(self.tls.do_handshake)

    def _io(self, operation, *args):
        while True:
            try:
                return operation(*args)
            except ssl.SSLWantReadError:
                pass
            finally:
                if self.outgoing.pending:
                    self.sock.sendall(self.outgoing.read())
            data = self.sock.recv(65536)
            if data:
                self.incoming.write(data)
            else:
                self.incoming.write_eof()

    def sendall(self, data):
        view = memoryview(data)
        while view:
            view = view[self._io(self.tls.write, view):]

    def recv(self, size):
        return self._io(self.tls.read, size)

    def recv_into(self, buffer):
        return self._io(self.tls.read, len(buffer), buffer)

    def settimeout(self, timeout):
        self.sock.settimeout(timeout)

    def close(self):
        self.sock.close()

class _HTTPSConnection(http.client.HTTPSConnection):
    def _tunnel(self):
        # Older Python versions need brackets on CONNECT, but TLS needs the bare IP.
        host = self._tunnel_host
        if ":" in host:
            self._tunnel_host = "[" + host + "]"
        try:
            super()._tunnel()
        finally:
            self._tunnel_host = host

class _HTTPSProxyTunnel(_HTTPSConnection):
    def connect(self):
        self.sock = self._create_connection((self.host, self.port), self.timeout)
        deadline = self.sock.deadline
        proxy = self._context.wrap_socket(self.sock, server_hostname=self.host)
        self.sock = _DeadlineSocket(proxy, deadline)
        self._tunnel()
        origin = _TLSTunnel(self.sock, self._tunnel_host, self._context)
        self.sock = _DeadlineSocket(origin, deadline)

class _Response(http.client.HTTPResponse):
    def _read_status(self):
        while True:
            version, status, reason = super()._read_status()
            if version not in ("HTTP/1.0", "HTTP/1.1"):
                raise LabctlError("transport", "unsupported HTTP version")
            if status == 101 or status > 599:
                raise LabctlError("transport", "unsupported HTTP status %d" % status)
            if status >= 200:
                return version, status, reason
            self._validate_headers(http.client.parse_headers(self.fp))

    @staticmethod
    def _validate_headers(headers):
        if headers.defects or any(_has_ctl(v) for _, v in headers.raw_items()):
            raise LabctlError("transport", "invalid HTTP response headers")

    def begin(self):
        super().begin()
        self._validate_headers(self.headers)
        if self.status in (204, 304):
            self.chunked = False  # length is already zero, even with misleading headers.
            return
        encodings = self.headers.get_all("Transfer-Encoding", [])
        lengths = self.headers.get_all("Content-Length", [])
        if encodings:
            if len(encodings) != 1 or not self.chunked or lengths:
                raise LabctlError("transport", "unsupported or ambiguous HTTP transfer encoding")
        elif len(lengths) != 1 or self.length is None or not re.fullmatch(r"[0-9]+", lengths[0].strip(" ")):
            raise LabctlError("transport", "missing or invalid Content-Length")

    def _read_next_chunk_size(self):
        line = self.fp.readline(http.client._MAXLINE + 1)
        if len(line) > http.client._MAXLINE:
            raise http.client.LineTooLong("chunk size")
        size, separator, _ = line.rstrip(b"\r\n").partition(b";")
        if separator:
            size = size.rstrip(b" ")  # Optional whitespace before a chunk extension.
        if not re.fullmatch(b"[0-9A-Fa-f]+", size):
            raise LabctlError("transport", "invalid HTTP chunk size")
        return int(size, 16)

def _read_body(response):
    data = response.read(_BODY_LIMIT + 1)
    if len(data) > _BODY_LIMIT:
        raise LabctlError("transport", "response exceeded size limit")
    if response.length:
        raise LabctlError("transport", "truncated HTTP response body")
    return data

@contextmanager
def http_request(url, method, headers, body, deadline, proxy_url=None):
    origin = _parse_url(url)
    proxy = validate_proxy_url(proxy_url) if proxy_url else None
    peer = proxy or origin
    target = origin.path or "/"
    if "?" in url.split("#", 1)[0]:
        target += "?" + origin.query
    connect_tunnel = proxy and origin.scheme == "https"
    address = (peer.hostname, _target_port(peer))
    if peer.scheme == "https" or connect_tunnel:
        cls = _HTTPSProxyTunnel if connect_tunnel and peer.scheme == "https" else _HTTPSConnection
        conn = cls(*address, context=ssl.create_default_context(), timeout=_remaining(deadline))
    else:
        conn = http.client.HTTPConnection(*address, timeout=_remaining(deadline))

    def dial(address, timeout, source_address=None):
        sock = _connect_tcp(*address, deadline, loopback_only=proxy and proxy.scheme == "http")
        return _DeadlineSocket(sock, deadline)

    conn._create_connection = dial
    conn.response_class = _Response
    try:
        if connect_tunnel:
            port = _target_port(origin)
            host = origin.hostname
            authority = "[%s]:%d" % (host, port) if ":" in host else "%s:%d" % (host, port)
            conn.set_tunnel(host, port, headers={"Host": authority})
        elif proxy:
            target = "%s://%s%s" % (origin.scheme, origin.netloc, target)
        if any(_has_ctl(k) or _has_ctl(v) for k, v in headers.items()):
            raise LabctlError("usage_error", "HTTP headers must be a single line", exit_code=2)
        conn.connect()
        if not isinstance(conn.sock, _DeadlineSocket):
            conn.sock = _DeadlineSocket(conn.sock, deadline)
        conn.sock.settimeout(_remaining(deadline))
        conn.request(method, target, body, dict(headers, Host=_host_header(origin), Connection="close"))
        conn.sock.settimeout(_remaining(deadline))
        with conn.getresponse() as response:
            _remaining(deadline)
            yield response
    except (socket.timeout, TimeoutError):
        raise DeadlineExceeded()
    except http.client.HTTPException as exc:
        raise LabctlError("transport", "invalid HTTP response (%s)" % type(exc).__name__)
    finally:
        conn.close()

def _iter_sse_events(body, deadline):
    partial = bytearray()
    data_lines = []
    pending_lf = False
    total = 0
    while True:
        _remaining(deadline)
        chunk = body.read1(65536)
        if not chunk:
            return
        total += len(chunk)
        if total > _BODY_LIMIT:
            raise LabctlError("transport", "response exceeded size limit")
        if pending_lf and chunk.startswith(b"\n"):
            chunk = chunk[1:]
        pending_lf = chunk.endswith(b"\r")
        lines = re.split(rb"\r\n|\r|\n", chunk)
        for line in lines[:-1]:
            partial.extend(line)
            field = _decode_utf8(partial)
            if not field and data_lines:
                yield "\n".join(data_lines)
                data_lines = []
            elif field.startswith("data:"):
                start = 6 if field.startswith("data: ") else 5
                data_lines.append(field[start:])
            partial.clear()
        partial.extend(lines[-1])

def _ids_match(expected, got):
    if expected is None:
        return True
    return type(got) is type(expected) and got == expected
class Client(object):
    def __init__(self, url, token, timeout, proxy_url=None, started=None):
        self.url = url
        self.token = _check_token(token)
        self.timeout = timeout
        self.proxy_url = proxy_url
        self.session_id = None
        self.protocol_version = PROTOCOL_VERSION
        self._closed = False
        self._end = (time.monotonic() if started is None else started) + timeout
    def _deadline(self):
        return self._end
    def _headers(self, with_session=True):
        headers = {
            "Content-Type": "application/json",
            "Accept": "application/json, text/event-stream",
        }
        if self.token:
            headers["Authorization"] = "Bearer %s" % self.token
        if with_session and self.session_id:
            headers["Mcp-Session-Id"] = self.session_id
        if with_session:
            headers["MCP-Protocol-Version"] = self.protocol_version
        return headers
    def _next_id(self):
        global _rpc_id
        _rpc_id += 1
        return _rpc_id
    def request(self, method, params, notification=False):
        deadline = self._deadline()
        payload = {"jsonrpc": JSONRPC, "method": method}
        if params is not None:
            payload["params"] = params
        req_id = None
        if not notification:
            req_id = self._next_id()
            payload["id"] = req_id
        try:
            body = json.dumps(payload, separators=(",", ":"), allow_nan=False).encode("utf-8")
        except (TypeError, ValueError) as exc:
            raise LabctlError("usage_error", "request is not valid JSON: %s" % exc, exit_code=2)
        headers = self._headers(with_session=(method != "initialize"))
        result = self._round_trip(headers, body, req_id, deadline, notification, method == "initialize")
        _remaining(deadline)
        return result
    def _round_trip(self, headers, body, req_id, deadline, notification, initializing):
        with http_request(self.url, "POST", headers, body, deadline, self.proxy_url) as stream:
            status = stream.status
            resp_headers = stream.headers
            if 300 <= status < 400:
                raise LabctlError(
                    "redirect",
                    "refusing to follow HTTP %s redirect" % status,
                    extra={"status": status},
                )
            if status >= 400:
                text = _decode_utf8(_read_body(stream))
                try:
                    text = json.dumps(_redact_obj(_load_response(text), diagnostic=True), ensure_ascii=True)
                except ValueError:
                    pass
                raise LabctlError("transport", "HTTP %s: %s" % (status, text), extra={"status": status})
            if initializing and "Mcp-Session-Id" in resp_headers:
                ids = resp_headers.get_all("Mcp-Session-Id")
                if len(ids) != 1:
                    raise LabctlError("transport", "ambiguous Mcp-Session-Id")
                self.session_id = _check_session_id(ids[0].strip(" "))
            if notification:
                if status != 202:
                    raise LabctlError("transport", "notifications/initialized expected empty HTTP 202, got HTTP %d"
                                      % status, extra={"status": status})
                if _read_body(stream):
                    raise LabctlError("transport", "notifications/initialized got a nonempty HTTP 202", extra={"status": 202})
            elif status == 202:
                raise LabctlError("transport", "HTTP 202 on a JSON-RPC request that has an id", extra={"status": 202})
            types = resp_headers.get_all("Content-Type", [])
            if notification and not types:
                return None
            if len(types) != 1:
                raise LabctlError("transport", "expected exactly one Content-Type field")
            media = _RPC_CONTENT_TYPE.fullmatch(types[0])
            if not media:
                raise LabctlError("transport", "unexpected Content-Type: %s" % types[0])
            if notification:
                return None
            if media.group(1).lower() == "text/event-stream":
                return self._read_sse_result(stream, req_id)
            return self._rpc_result(_read_body(stream), req_id)
    def _rpc_result(self, body, req_id):
        try:
            msg = _load_response(_decode_utf8(body))
        except ValueError:
            raise LabctlError("transport", "response is not valid JSON")
        return self._rpc_message(msg, req_id)
    def _rpc_message(self, msg, req_id):
        if not isinstance(msg, dict):
            raise LabctlError("transport", "JSON-RPC response must be an object")
        if msg.get("jsonrpc") != JSONRPC:
            raise LabctlError("transport", "JSON-RPC response is missing jsonrpc 2.0")
        if req_id is not None:
            if "id" not in msg or not _ids_match(req_id, msg.get("id")):
                raise LabctlError("transport", "JSON-RPC response id does not match request id")
        if "error" in msg and msg["error"] is not None:
            raise LabctlError(
                "rpc",
                "JSON-RPC error",
                extra={"rpc_error": msg["error"]},
            )
        if "result" not in msg:
            raise LabctlError("transport", "JSON-RPC response is missing result")
        return msg["result"]
    def _read_sse_result(self, body, req_id):
        for data in _iter_sse_events(body, self._end):
            try:
                msg = _load_response(data)
            except ValueError:
                continue
            if not isinstance(msg, dict) or "method" in msg:
                continue
            if "result" not in msg and "error" not in msg:
                continue
            if req_id is not None and "id" not in msg:
                continue
            if req_id is not None and not _ids_match(req_id, msg.get("id")):
                continue
            return self._rpc_message(msg, req_id)
        raise LabctlError("transport", "SSE stream ended without a matching response")
    def initialize(self):
        result = self.request(
            "initialize",
            {
                "protocolVersion": PROTOCOL_VERSION,
                "capabilities": {},
                "clientInfo": {"name": CLIENT_NAME, "version": CLIENT_VERSION},
            },
        )
        if not isinstance(result, dict):
            raise LabctlError("transport", "initialize result must be an object")
        version = result.get("protocolVersion")
        if version != PROTOCOL_VERSION:
            raise LabctlError("transport", "unsupported protocol version")
        self.protocol_version = version
        return result
    def notify_initialized(self):
        self.request("notifications/initialized", {}, notification=True)
    def close(self):
        if self._closed:
            return
        self._closed = True
        deadline = self._deadline()
        _remaining(deadline)
        if not self.session_id:
            return
        headers = {"Mcp-Session-Id": self.session_id, "MCP-Protocol-Version": self.protocol_version}
        if self.token:
            headers["Authorization"] = "Bearer %s" % self.token
        try:
            with http_request(self.url, "DELETE", headers, b"", deadline, self.proxy_url) as response:
                _read_body(response)
                if not 200 <= response.status < 300:
                    raise LabctlError("transport", "HTTP %d" % response.status)
        except DeadlineExceeded:
            raise
        except Exception as exc:
            print(redact("warning: session DELETE failed: %s" % exc, diagnostic=True), file=sys.stderr)
        _remaining(deadline)
    def __enter__(self):
        try:
            self.initialize()
            self.notify_initialized()
        except BaseException:
            self.__exit__(*sys.exc_info())
            raise
        return self
    def __exit__(self, exc_type, exc, tb):
        try:
            self.close()
        except DeadlineExceeded:
            if exc is None:
                raise
        return False
    def list_tools(self):
        max_pages = _env_positive_int("LABCTL_MAX_PAGES", DEFAULT_MAX_PAGES)
        max_tools = _env_positive_int("LABCTL_MAX_TOOLS", DEFAULT_MAX_TOOLS)
        tools = []
        seen = set()
        cursor = None
        pages = 0
        while True:
            pages += 1
            if pages > max_pages:
                raise LabctlError("pagination", "tools/list exceeded page cap %s" % max_pages)
            params = {}
            if cursor is not None:
                params["cursor"] = cursor
            result = self.request("tools/list", params)
            if not isinstance(result, dict):
                raise LabctlError("transport", "tools/list result must be an object")
            batch = result.get("tools")
            if not isinstance(batch, list):
                raise LabctlError("transport", "tools/list tools must be an array")
            tools.extend(batch)
            if len(tools) > max_tools:
                raise LabctlError("pagination", "tools/list exceeded tool cap %s" % max_tools)
            nxt = result.get("nextCursor", "")
            if nxt == "":
                break
            if not isinstance(nxt, str):
                raise LabctlError("transport", "tools/list nextCursor must be a string")
            if nxt in seen:
                raise LabctlError(
                    "pagination",
                    "repeated tools/list cursor: %s" % nxt,
                    extra={"cursor": nxt},
                )
            seen.add(nxt)
            cursor = nxt
        return tools
    def call_tool(self, name, arguments):
        result = self.request("tools/call", {"name": name, "arguments": arguments})
        if not isinstance(result, dict):
            raise LabctlError("transport", "tools/call result must be an object")
        if result.get("isError"):
            raise LabctlError("tool", "tool returned isError", extra={"result": result})
        return result
def load_token(token_file_arg):
    env_token = os.environ.get("LABCTL_TOKEN")
    if env_token is not None and env_token != "":
        return _check_token(env_token)
    path = token_file_arg or os.environ.get("LABCTL_TOKEN_FILE")
    if not path:
        return None
    try:
        with open(path, encoding="utf-8", newline="") as source:
            data = source.read()
    except OSError as exc:
        raise LabctlError("usage_error", "cannot read token file: %s" % exc, exit_code=2)
    if data.endswith("\r\n"):
        data = data[:-2]
    elif data.endswith("\n"):
        data = data[:-1]
    return _check_token(data)
def _parse_timeout(raw):
    if raw is None or raw == "":
        return None
    try:
        value = float(raw)
    except ValueError:
        raise LabctlError("usage_error", "--timeout must be a positive number", exit_code=2)
    if not (value > 0 and value < float("inf")):
        raise LabctlError("usage_error", "--timeout must be a positive number", exit_code=2)
    return value
def _call_arguments(json_arg, arg_flags):
    if json_arg is not None and arg_flags:
        raise LabctlError("usage_error", "do not mix --json and --arg", exit_code=2)
    if json_arg is not None:
        if json_arg.startswith("@"):
            try:
                with open(json_arg[1:], encoding="utf-8") as source:
                    text = source.read()
            except OSError as exc:
                raise LabctlError("usage_error", "cannot read --json file: %s" % exc, exit_code=2)
        else:
            text = json_arg
        try:
            data = json.loads(text)
        except ValueError as exc:
            raise LabctlError("usage_error", "invalid --json: %s" % exc, exit_code=2)
        if not isinstance(data, dict):
            raise LabctlError("usage_error", "--json must be a JSON object", exit_code=2)
        _reject_nonfinite(data, "--json")
        return data
    out = {}
    for item in arg_flags:
        if "=" not in item:
            raise LabctlError("usage_error", "--arg values must be k=v", exit_code=2)
        key, value = item.split("=", 1)
        try:
            parsed = json.loads(value)
        except ValueError:
            out[key] = value
            continue
        if isinstance(parsed, float) and not _is_finite(parsed):
            out[key] = value
            continue
        _reject_nonfinite(parsed, "--arg")
        out[key] = parsed
    return out
def _schema_of(tool):
    if "inputSchema" in tool:
        return tool["inputSchema"]
    return tool.get("input_schema")
class _Parser(argparse.ArgumentParser):
    def __init__(self, *args, **kwargs):
        kwargs.setdefault("allow_abbrev", False)
        argparse.ArgumentParser.__init__(self, *args, **kwargs)
    def error(self, message):
        fail("usage_error", "unrecognized arguments", exit_code=2)
def _reject_token_flags(argv):
    for arg in argv:
        name = arg.split("=", 1)[0]
        if name in ("--token", "--bearer"):
            raise LabctlError(
                "usage_error",
                "rejected option %s (tokens are only accepted from LABCTL_TOKEN, LABCTL_TOKEN_FILE, or --token-file)"
                % name,
                exit_code=2,
            )
def build_parser():
    parser = _Parser(prog="labctl", description="stdlib MCP streamable-HTTP client")
    parser.add_argument("--url", default=None, help="MCP URL (or LABCTL_URL)")
    parser.add_argument("--timeout", default=None, help="Deadline seconds (or LABCTL_TIMEOUT; default 60)")
    parser.add_argument("--token-file", dest="token_file", default=None, help="Token file (or LABCTL_TOKEN_FILE); LABCTL_TOKEN wins")
    parser.add_argument("--proxy", default=None, help="Opt-in proxy (or LABCTL_PROXY); https or loopback only")
    sub = parser.add_subparsers(dest="command", required=True)
    p_tools = sub.add_parser("tools", allow_abbrev=False, help="List tools")
    p_tools.add_argument("--server", default=None, help="Keep tools named SERVER__*")
    p_schema = sub.add_parser("schema", allow_abbrev=False, help="Print a tool input schema")
    p_schema.add_argument("tool")
    p_call = sub.add_parser("call", allow_abbrev=False, help="Call a tool")
    p_call.add_argument("tool")
    p_call.add_argument("--json", dest="json_args", default=None, help="JSON object or @file")
    p_call.add_argument("--arg", dest="arg_flags", action="append", default=[], help="k=v (JSON or string)")
    sub.add_parser("endpoints", allow_abbrev=False, help="Call labinfo__endpoints_list")
    return parser
def _filter_tools(tools, server):
    if not server:
        return tools
    prefix = server + "__"
    return [t for t in tools if isinstance(t, dict) and str(t.get("name", "")).startswith(prefix)]
def run_command(args, started=None):
    global _token_to_redact
    url = args.url or os.environ.get("LABCTL_URL")
    if not url:
        raise LabctlError(
            "usage_error",
            "LABCTL_URL or --url is required (for example http://127.0.0.1:8080/mcp)",
            exit_code=2,
        )
    timeout = _parse_timeout(args.timeout)
    if timeout is None:
        timeout = _parse_timeout(os.environ.get("LABCTL_TIMEOUT"))
    if timeout is None:
        timeout = DEFAULT_TIMEOUT
    proxy = args.proxy or os.environ.get("LABCTL_PROXY") or None
    if proxy:
        validate_proxy_url(proxy)
    token = load_token(args.token_file)
    _token_to_redact = token
    with Client(url, token, timeout, proxy_url=proxy, started=started) as client:
        if args.command == "tools":
            result = {"tools": _filter_tools(client.list_tools(), args.server)}
        elif args.command == "schema":
            for tool in client.list_tools():
                if isinstance(tool, dict) and tool.get("name") == args.tool:
                    schema = _schema_of(tool)
                    if schema is None:
                        raise LabctlError("tool", "tool %s has no inputSchema" % args.tool)
                    if not isinstance(schema, dict):
                        raise LabctlError("tool", "tool %s inputSchema must be an object" % args.tool)
                    result = schema
                    break
            else:
                raise LabctlError("tool", "unknown tool: %s" % args.tool)
        elif args.command == "call":
            result = client.call_tool(args.tool, _call_arguments(args.json_args, args.arg_flags))
        elif args.command == "endpoints":
            result = client.call_tool("labinfo__endpoints_list", {})
        else:
            raise LabctlError("usage_error", "unrecognized arguments", exit_code=2)
    return emit(result)
def main(argv=None, started=None):
    if started is None:
        started = time.monotonic()
    argv = list(sys.argv[1:] if argv is None else argv)
    try:
        _reject_token_flags(argv)
        return run_command(build_parser().parse_args(argv), started)
    except SystemExit as exc:
        code = exc.code
        if code is None:
            return 0
        if isinstance(code, int):
            return code
        fail("usage_error", "unrecognized arguments", exit_code=2)
        return 2
    except DeadlineExceeded as exc:
        fail(exc.kind, exc.message, exit_code=exc.exit_code, extra=exc.extra)
        return 1
    except LabctlError as exc:
        extra = dict(exc.extra)
        if exc.kind == "tool" and "result" in extra:
            emit(extra["result"], exit_code=exc.exit_code)
            return exc.exit_code
        if exc.kind == "rpc" and "rpc_error" in extra:
            emit({"error": extra["rpc_error"]}, exit_code=exc.exit_code)
            return exc.exit_code
        fail(exc.kind, exc.message, exit_code=exc.exit_code, extra={
            k: v for k, v in extra.items() if k not in ("result", "rpc_error")
        })
        return exc.exit_code
    except socket.timeout:
        fail("timeout", "request timed out")
        return 1
    except OSError as exc:
        fail("transport", str(exc))
        return 1
    except Exception as exc:
        fail("internal", str(exc))
        return 1
if __name__ == "__main__":
    sys.exit(main(started=_cli_started))
