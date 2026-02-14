import asyncio
import os
import signal
import stat
import subprocess
import sys

import httpx
from fastapi import FastAPI, Request, WebSocket, WebSocketDisconnect
from fastapi.middleware.cors import CORSMiddleware
from fastapi.responses import Response
from app.analytics import router as analytics_router

GO_BACKEND_URL = "http://127.0.0.1:8088"
go_process = None

app = FastAPI(title="INEC Election Platform", version="5.0",
              description="Next-Generation Blockchain-Based Election Results Platform")

# Disable CORS. Do not remove this for full-stack development.
app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)

app.include_router(analytics_router)

http_client: httpx.AsyncClient = None


@app.on_event("startup")
async def startup():
    global go_process, http_client
    http_client = httpx.AsyncClient(base_url=GO_BACKEND_URL, timeout=30.0)
    db_path = os.environ.get("DB_PATH", "/data/app.db")
    os.environ["DB_PATH"] = db_path
    binary = os.path.join(os.path.dirname(os.path.dirname(__file__)), "inec-go-backend")
    if not os.path.isfile(binary):
        binary = "/app/inec-go-backend"
    os.chmod(binary, os.stat(binary).st_mode | stat.S_IXUSR | stat.S_IXGRP | stat.S_IXOTH)
    env = {**os.environ, "PORT": "8088", "DB_PATH": db_path}
    go_process = subprocess.Popen([binary], env=env, stdout=sys.stdout, stderr=sys.stderr)
    for _ in range(50):
        await asyncio.sleep(0.2)
        try:
            resp = await http_client.get("/healthz")
            if resp.status_code == 200:
                print("Go backend is ready")
                return
        except Exception:
            pass
    print("WARNING: Go backend did not become ready in 10s")


@app.on_event("shutdown")
async def shutdown():
    global go_process, http_client
    if http_client:
        await http_client.aclose()
    if go_process:
        go_process.send_signal(signal.SIGTERM)
        try:
            go_process.wait(timeout=5)
        except subprocess.TimeoutExpired:
            go_process.kill()


@app.api_route("/{path:path}", methods=["GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "HEAD"])
async def proxy(request: Request, path: str):
    url = f"/{path}"
    if request.url.query:
        url += f"?{request.url.query}"

    headers = {}
    for k, v in request.headers.items():
        if k.lower() not in ("host", "content-length", "transfer-encoding"):
            headers[k] = v
    headers.pop("accept-encoding", None)

    body = await request.body()

    try:
        resp = await http_client.request(
            method=request.method,
            url=url,
            headers=headers,
            content=body if body else None,
        )
    except Exception as e:
        return Response(content=f'{{"detail":"go backend unavailable: {e}"}}',
                        status_code=502, media_type="application/json")

    excluded = {"transfer-encoding", "content-encoding", "content-length", "connection"}
    resp_headers = {k: v for k, v in resp.headers.items() if k.lower() not in excluded}

    return Response(
        content=resp.content,
        status_code=resp.status_code,
        headers=resp_headers,
        media_type=resp.headers.get("content-type"),
    )


@app.websocket("/results/ws/updates")
async def ws_proxy(websocket: WebSocket):
    await websocket.accept()
    import websockets
    ws_url = GO_BACKEND_URL.replace("http://", "ws://") + "/results/ws/updates"
    try:
        async with websockets.connect(ws_url) as go_ws:
            async def forward_to_client():
                try:
                    async for msg in go_ws:
                        await websocket.send_text(msg)
                except Exception:
                    pass

            async def forward_to_go():
                try:
                    while True:
                        data = await websocket.receive_text()
                        await go_ws.send(data)
                except WebSocketDisconnect:
                    pass
                except Exception:
                    pass

            await asyncio.gather(forward_to_client(), forward_to_go())
    except Exception:
        try:
            await websocket.close()
        except Exception:
            pass
