#!/usr/bin/env python3
"""Static frontend-to-backend route-contract audit for the primary INEC web app."""
from __future__ import annotations

import json
import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
FRONTEND = ROOT / "inec-frontend" / "src"
BACKEND = ROOT / "inec-go-backend"
OUT = ROOT / ".audit"
OUT.mkdir(exist_ok=True)

ROUTE_RE = re.compile(r'r\.Handle(?:Func)?\(\s*"([^"?]+)"')
REQUEST_RE = re.compile(r'(?:request|serviceRequest)\s*(?:<[^>]+>)?\s*\(\s*[`\"]([^`\"]+)[`\"]', re.MULTILINE)
FETCH_RE = re.compile(r'fetch\s*\(\s*[`\"]([^`\"]+)[`\"]', re.MULTILINE)
AXIOS_RE = re.compile(r'\.(?:get|post|put|patch|delete)\s*\(\s*[`\"]([^`\"]+)[`\"]', re.MULTILINE)


def normalize(path: str) -> str:
    path = path.strip()
    if not path.startswith("/"):
        return path
    # Conditional interpolation generates query strings or optional path suffixes;
    # preserve the statically known base in that case.
    path = re.split(r'\$\{[^}]*\?', path, maxsplit=1)[0]
    path = path.split("?", 1)[0]
    # Query strings are frequently prepared in a variable (`/users${qs}`),
    # whereas true dynamic path segments are slash-delimited (`/users/${id}`).
    # Remove the former and retain the latter as a normalized path parameter.
    path = re.sub(r'(?<!/)\$\{[^}]+\}', '', path)
    path = re.sub(r'\$\{[^}]+\}', '{param}', path)
    path = re.sub(r'\{[^}]+\}', '{param}', path)
    path = re.sub(r'/+', '/', path)
    return path.rstrip("/") or "/"


def route_pattern(path: str) -> re.Pattern[str]:
    normalized = normalize(path)
    escaped = re.escape(normalized).replace(re.escape('{param}'), r'[^/]+')
    return re.compile(r'^' + escaped + r'$')


def frontend_paths() -> dict[str, list[str]]:
    result: dict[str, list[str]] = {}
    for path in FRONTEND.rglob('*'):
        if not path.is_file() or path.suffix not in {'.ts', '.tsx'}:
            continue
        text = path.read_text(encoding='utf-8', errors='ignore')
        candidates = set(REQUEST_RE.findall(text) + FETCH_RE.findall(text) + AXIOS_RE.findall(text))
        for candidate in candidates:
            if candidate.startswith(('http://', 'https://')) or not candidate.startswith('/'):
                continue
            route = normalize(candidate)
            result.setdefault(route, []).append(str(path.relative_to(ROOT)))
    return result


def backend_routes() -> dict[str, list[str]]:
    result: dict[str, list[str]] = {}
    for path in BACKEND.rglob('*.go'):
        if 'testdata' in path.parts:
            continue
        text = path.read_text(encoding='utf-8', errors='ignore')
        for route in ROUTE_RE.findall(text):
            normalized = normalize(route)
            result.setdefault(normalized, []).append(str(path.relative_to(ROOT)))
    return result


def main() -> None:
    frontend = frontend_paths()
    backend = backend_routes()
    patterns = [(route, route_pattern(route)) for route in backend]
    unresolved: dict[str, list[str]] = {}
    for route, sources in frontend.items():
        if not any(pattern.match(route) for _, pattern in patterns):
            unresolved[route] = sorted(set(sources))

    payload = {
        'frontend_route_count': len(frontend),
        'backend_route_count': len(backend),
        'unresolved_frontend_routes': dict(sorted(unresolved.items())),
        'frontend_routes': {key: sorted(set(value)) for key, value in sorted(frontend.items())},
        'backend_routes': {key: sorted(set(value)) for key, value in sorted(backend.items())},
    }
    (OUT / 'route_contract_audit.json').write_text(json.dumps(payload, indent=2, sort_keys=True) + '\n', encoding='utf-8')
    lines = [
        '# Static Route Contract Audit',
        '',
        'This report compares statically discoverable primary-web paths with route strings registered by Go services. Query strings and conditional template fragments are stripped before matching. Dynamic bases, WebSocket URLs, and service-mesh routing require separate runtime validation.',
        '',
        '| Metric | Count |',
        '| --- | ---: |',
        f'| Frontend paths discovered | {len(frontend)} |',
        f'| Backend paths registered | {len(backend)} |',
        f'| Unresolved frontend paths | {len(unresolved)} |',
        '',
        '## Unresolved Frontend Paths',
        '',
    ]
    if unresolved:
        lines.extend(['| Frontend path | Source files |', '| --- | --- |'])
        for route, sources in sorted(unresolved.items()):
            shown = ', '.join(f'`{source}`' for source in sources[:8])
            suffix = ' …' if len(sources) > 8 else ''
            lines.append(f'| `{route}` | {shown}{suffix} |')
    else:
        lines.append('No statically discoverable frontend paths were unmatched.')
    (OUT / 'route_contract_audit.md').write_text('\n'.join(lines) + '\n', encoding='utf-8')
    print(json.dumps({'frontend': len(frontend), 'backend': len(backend), 'unresolved': len(unresolved)}))


if __name__ == '__main__':
    main()
