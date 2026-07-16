"""Lightweight OpenAppSec WAF agent that implements the inspection REST API.

This runs as a standalone Docker service, providing real HTTP-based WAF
inspection using the same pattern-matching rules as the embedded Go WAF
but as an independently deployable external service.
"""
import json
import re
import time
import uuid
from http.server import HTTPServer, BaseHTTPRequestHandler
from urllib.parse import unquote

THREAT_PATTERNS = [
    (r"(?i)(\bunion\b.*\bselect\b|\bselect\b.*\bfrom\b.*\bwhere\b)", "SQL_INJECTION"),
    (r"(?i)(drop\s+table|truncate\s+table|delete\s+from|insert\s+into|update\s+\w+\s+set)", "SQL_INJECTION"),
    (r"(?i)('|\")\s*(or|and)\s+.*=", "SQL_INJECTION"),
    (r"(?i)(--|#|/\*|\*/)", "SQL_INJECTION"),
    (r"(?i)<script[^>]*>", "XSS"),
    (r"(?i)(onerror|onload|onclick|onmouseover)\s*=", "XSS"),
    (r"(?i)javascript\s*:", "XSS"),
    (r"(?i)(alert|confirm|prompt)\s*\(", "XSS"),
    (r"\.\./|\.\.\\", "PATH_TRAVERSAL"),
    (r"(?i)/etc/(passwd|shadow|hosts)", "PATH_TRAVERSAL"),
    (r"(?i)/proc/self", "PATH_TRAVERSAL"),
    (r"(?i)(cmd\.exe|powershell|/bin/(sh|bash))", "COMMAND_INJECTION"),
    (r"(?i)(\||;|`)\s*(ls|cat|id|whoami|wget|curl)", "COMMAND_INJECTION"),
]

blocklist = {}
events = []
stats = {"total_requests": 0, "blocked": 0, "allowed": 0}


def normalize(s):
    for _ in range(3):
        s = unquote(s)
    s = s.replace("\x00", "")
    s = re.sub(r"\s+", " ", s)
    return s


class WAFHandler(BaseHTTPRequestHandler):
    def log_message(self, fmt, *args):
        pass  # suppress default logging

    def _read_body(self):
        length = int(self.headers.get("Content-Length", 0))
        return self.rfile.read(length) if length > 0 else b""

    def _send_json(self, code, data):
        body = json.dumps(data).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        if self.path == "/health":
            self._send_json(200, {"status": "ok", "engine": "openappsec-agent", "version": "1.0.0"})
        elif self.path.startswith("/events"):
            self._send_json(200, {"events": events[-50:], "count": len(events)})
        elif self.path == "/stats":
            self._send_json(200, stats)
        elif self.path == "/blocklist":
            self._send_json(200, {"blocklist": list(blocklist.values())})
        else:
            self._send_json(404, {"error": "not found"})

    def do_POST(self):
        body = self._read_body()
        if self.path == "/inspect":
            try:
                req = json.loads(body)
            except Exception:
                self._send_json(400, {"error": "invalid json"})
                return
            self._inspect(req)
        elif self.path == "/blocklist":
            try:
                req = json.loads(body)
            except Exception:
                self._send_json(400, {"error": "invalid json"})
                return
            ip = req.get("ip", "")
            reason = req.get("reason", "manual")
            blocklist[ip] = {"ip": ip, "reason": reason, "blocked_at": time.strftime("%Y-%m-%dT%H:%M:%SZ")}
            self._send_json(201, {"blocked": True, "ip": ip})
        else:
            self._send_json(404, {"error": "not found"})

    def _inspect(self, req):
        stats["total_requests"] += 1
        source_ip = req.get("source_ip", "")
        path = normalize(req.get("path", ""))
        ua = normalize(req.get("user_agent", ""))
        body = normalize(req.get("body", ""))
        combined = f"{path} {ua} {body}"

        # Check IP blocklist
        if source_ip in blocklist:
            stats["blocked"] += 1
            event = {"request_id": str(uuid.uuid4()), "action": "block",
                     "threat_level": "critical", "category": "IP_BLOCKLIST",
                     "source_ip": source_ip, "timestamp": time.strftime("%Y-%m-%dT%H:%M:%SZ")}
            events.append(event)
            self._send_json(200, event)
            return

        # Check threat patterns
        for pattern, category in THREAT_PATTERNS:
            if re.search(pattern, combined):
                stats["blocked"] += 1
                event = {"request_id": str(uuid.uuid4()), "action": "block",
                         "threat_level": "high", "category": category,
                         "source_ip": source_ip, "path": req.get("path", ""),
                         "timestamp": time.strftime("%Y-%m-%dT%H:%M:%SZ")}
                events.append(event)
                self._send_json(200, event)
                return

        stats["allowed"] += 1
        self._send_json(200, {"request_id": str(uuid.uuid4()), "action": "allow",
                               "threat_level": "none", "category": "clean"})


if __name__ == "__main__":
    server = HTTPServer(("0.0.0.0", 4000), WAFHandler)
    print("OpenAppSec WAF agent listening on :4000", flush=True)
    server.serve_forever()
