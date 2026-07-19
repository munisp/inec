"""OpenAppSec WAF agent — external inspection service for the INEC platform.

Implements the HTTP inspection API consumed by the Go backend's
mw_openappsec.go client:

    POST /api/v1/inspect     -> WAFDecision  {request_id, action, threat_level, rules_matched, score}
    GET  /api/v1/threats     -> {events: [WAFEvent]}
    GET  /api/v1/stats       -> WAFStats {total_requests, blocked_requests, threats_by_level, ...}
    POST /api/v1/blocklist   -> add IP blocklist entry
    GET  /api/v1/blocklist   -> {entries: [{ip, reason, added_at}]}
    GET  /api/v1/health      -> liveness

All routes also work without the /api/v1 prefix. Detection uses the same
pattern rules as the embedded Go WAF so behaviour is consistent whether the
platform runs the embedded or the external engine.
"""
import json
import os
import re
import time
import uuid
from http.server import HTTPServer, BaseHTTPRequestHandler
from urllib.parse import unquote, urlparse, parse_qs

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
_event_id = 0
stats = {"total_requests": 0, "blocked_requests": 0, "allowed": 0,
         "threats_by_level": {"critical": 0, "high": 0, "medium": 0, "low": 0}}


def normalize(s):
    for _ in range(3):
        s = unquote(s)
    s = s.replace("\x00", "")
    s = re.sub(r"\s+", " ", s)
    return s


def _route(path):
    """Strip a leading /api/v1 (or /api) prefix so both forms route identically."""
    p = urlparse(path).path
    if p.startswith("/api/v1"):
        p = p[len("/api/v1"):]
    elif p.startswith("/api"):
        p = p[len("/api"):]
    return p or "/"


def _query(path):
    return parse_qs(urlparse(path).query)


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
        route = _route(self.path)
        if route == "/health":
            self._send_json(200, {"status": "ok", "engine": "openappsec-agent", "version": "1.1.0"})
        elif route in ("/threats", "/events"):
            qs = _query(self.path)
            try:
                limit = int(qs.get("limit", ["50"])[0])
            except (TypeError, ValueError):
                limit = 50
            self._send_json(200, {"events": events[-limit:], "count": len(events)})
        elif route == "/stats":
            top_ips = {}
            top_vectors = {}
            for e in events:
                if e["action"] == "block":
                    top_ips[e["source_ip"]] = top_ips.get(e["source_ip"], 0) + 1
                    top_vectors[e["rule_id"]] = top_vectors.get(e["rule_id"], 0) + 1
            payload = dict(stats)
            payload["top_blocked_ips"] = [
                {"ip": ip, "count": c}
                for ip, c in sorted(top_ips.items(), key=lambda kv: -kv[1])[:10]
            ]
            payload["top_attack_vectors"] = [
                {"type": t, "count": c}
                for t, c in sorted(top_vectors.items(), key=lambda kv: -kv[1])[:10]
            ]
            self._send_json(200, payload)
        elif route == "/blocklist":
            self._send_json(200, {"entries": list(blocklist.values())})
        else:
            self._send_json(404, {"error": "not found"})

    def do_POST(self):
        route = _route(self.path)
        body = self._read_body()
        if route == "/inspect":
            try:
                req = json.loads(body)
            except Exception:
                self._send_json(400, {"error": "invalid json"})
                return
            self._inspect(req)
        elif route == "/blocklist":
            try:
                req = json.loads(body)
            except Exception:
                self._send_json(400, {"error": "invalid json"})
                return
            ip = req.get("ip", "")
            reason = req.get("reason", "manual")
            blocklist[ip] = {"ip": ip, "reason": reason,
                             "added_at": time.strftime("%Y-%m-%dT%H:%M:%SZ")}
            self._send_json(201, {"blocked": True, "ip": ip})
        else:
            self._send_json(404, {"error": "not found"})

    def _record_event(self, req, action, threat_level, rule_id, score):
        global _event_id
        _event_id += 1
        event = {
            "id": _event_id,
            "request_id": str(uuid.uuid4()),
            "source_ip": req.get("source_ip", ""),
            "method": req.get("method", ""),
            "path": req.get("path", ""),
            "rule_id": rule_id,
            "action": action,
            "threat_level": threat_level,
            "details": "score=%d" % score,
            "timestamp": time.strftime("%Y-%m-%dT%H:%M:%SZ"),
        }
        events.append(event)
        return event

    def _inspect(self, req):
        stats["total_requests"] += 1
        source_ip = req.get("source_ip", "")
        path = normalize(req.get("path", ""))
        query = normalize(req.get("query_string", ""))
        ua = normalize(req.get("user_agent", ""))
        body = normalize(req.get("body", ""))
        combined = f"{path} {query} {ua} {body}"

        # 1) IP blocklist — highest precedence, critical severity
        if source_ip in blocklist:
            stats["blocked_requests"] += 1
            stats["threats_by_level"]["critical"] += 1
            event = self._record_event(req, "block", "critical", "IP_BLOCKLIST", 100)
            self._send_json(200, {
                "request_id": event["request_id"], "action": "block",
                "threat_level": "critical", "rules_matched": ["IP_BLOCKLIST"],
                "score": 100,
            })
            return

        # 2) Pattern rules — score grows with number of matched rule classes
        matched = []
        for pattern, category in THREAT_PATTERNS:
            if re.search(pattern, combined) and category not in matched:
                matched.append(category)
        if matched:
            score = min(60 + 10 * len(matched), 100)
            stats["blocked_requests"] += 1
            stats["threats_by_level"]["high"] += 1
            event = self._record_event(req, "block", "high", matched[0], score)
            self._send_json(200, {
                "request_id": event["request_id"], "action": "block",
                "threat_level": "high", "rules_matched": matched,
                "score": score,
            })
            return

        # 3) Clean
        stats["allowed"] += 1
        self._send_json(200, {
            "request_id": str(uuid.uuid4()), "action": "allow",
            "threat_level": "none", "rules_matched": [], "score": 0,
        })


if __name__ == "__main__":
    port = int(os.environ.get("PORT", "4000"))
    server = HTTPServer(("0.0.0.0", port), WAFHandler)
    print("OpenAppSec WAF agent listening on :%d" % port, flush=True)
    server.serve_forever()
