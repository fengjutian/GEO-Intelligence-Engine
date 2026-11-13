#!/usr/bin/env python3
"""GEO Intelligence Engine native development process manager."""

from __future__ import annotations

import argparse
import json
import os
import shutil
import signal
import subprocess
import time
import urllib.error
import urllib.request
from pathlib import Path
from typing import IO, Any

ROOT = Path(__file__).resolve().parents[1]
RUN_DIR = ROOT / ".run"
LOG_DIR = RUN_DIR / "logs"
STATE_FILE = RUN_DIR / "dev-processes.json"

SERVICES = {
    "api": {
        "command": ["go", "run", "./cmd/api"],
        "cwd": ROOT,
        "health": "http://127.0.0.1:8080/healthz",
    },
    "web": {
        "command": ["pnpm", "dev:web"],
        "cwd": ROOT,
        "health": "http://127.0.0.1:5173/",
    },
    "crawler": {
        "command": ["pnpm", "dev:crawler"],
        "cwd": ROOT,
        "health": "http://127.0.0.1:8082/healthz",
    },
    "judge": {
        "command": [
            "uv",
            "run",
            "uvicorn",
            "geo_judge.app:app",
            "--reload",
            "--port",
            "8081",
        ],
        "cwd": ROOT / "apps" / "judge-worker",
        "health": "http://127.0.0.1:8081/healthz",
    },
}


def load_dotenv(path: Path, environment: dict[str, str]) -> None:
    if not path.exists():
        return
    for line_number, raw_line in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
        line = raw_line.strip()
        if not line or line.startswith("#"):
            continue
        if line.startswith("export "):
            line = line[7:].lstrip()
        if "=" not in line:
            raise SystemExit(f"{path}:{line_number}: expected KEY=VALUE")
        key, value = line.split("=", 1)
        key = key.strip()
        value = value.strip()
        if value[:1] == value[-1:] and value.startswith(("'", '"')):
            value = value[1:-1]
        environment.setdefault(key, value)


def resolve_command(command: list[str]) -> list[str]:
    executable = shutil.which(command[0])
    if executable is None:
        raise SystemExit(f"缺少命令：{command[0]}，请先安装并加入 PATH。")
    return [executable, *command[1:]]


def run_checked(command: list[str], cwd: Path, environment: dict[str, str]) -> None:
    display = " ".join(command)
    print(f"执行：{display}")
    result = subprocess.run(resolve_command(command), cwd=cwd, env=environment, check=False)
    if result.returncode != 0:
        raise SystemExit(f"命令失败（{result.returncode}）：{display}")


def read_state() -> dict[str, dict[str, Any]]:
    if not STATE_FILE.exists():
        return {}
    try:
        return json.loads(STATE_FILE.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as error:
        raise SystemExit(f"无法读取运行状态 {STATE_FILE}: {error}") from error


def write_state(state: dict[str, dict[str, Any]]) -> None:
    RUN_DIR.mkdir(parents=True, exist_ok=True)
    temporary = STATE_FILE.with_suffix(".tmp")
    temporary.write_text(json.dumps(state, ensure_ascii=False, indent=2), encoding="utf-8")
    temporary.replace(STATE_FILE)


def process_alive(pid: int) -> bool:
    if pid <= 0:
        return False
    try:
        os.kill(pid, 0)
    except (OSError, ProcessLookupError):
        return False
    return True


def spawn_service(
    name: str, specification: dict[str, Any], environment: dict[str, str]
) -> tuple[subprocess.Popen[bytes], IO[bytes], IO[bytes]]:
    LOG_DIR.mkdir(parents=True, exist_ok=True)
    stdout_file = (LOG_DIR / f"{name}.log").open("ab", buffering=0)
    stderr_file = (LOG_DIR / f"{name}.error.log").open("ab", buffering=0)
    options: dict[str, Any] = {
        "cwd": specification["cwd"],
        "env": environment,
        "stdout": stdout_file,
        "stderr": stderr_file,
        "stdin": subprocess.DEVNULL,
    }
    if os.name == "nt":
        options["creationflags"] = subprocess.CREATE_NEW_PROCESS_GROUP
    else:
        options["start_new_session"] = True
    process = subprocess.Popen(resolve_command(specification["command"]), **options)
    return process, stdout_file, stderr_file


def health_available(url: str) -> bool:
    try:
        with urllib.request.urlopen(url, timeout=1) as response:
            return 200 <= response.status < 500
    except (urllib.error.URLError, TimeoutError, OSError):
        return False


def wait_for_services(state: dict[str, dict[str, Any]], timeout: int) -> bool:
    deadline = time.monotonic() + timeout
    pending = set(state)
    while pending and time.monotonic() < deadline:
        for name in list(pending):
            item = state[name]
            if not process_alive(int(item["pid"])):
                print(f"失败：{name} 已退出，请查看 {item['stderr_log']}")
                return False
            if health_available(str(item["health"])):
                print(f"就绪：{name} -> {item['health']}")
                pending.remove(name)
        if pending:
            time.sleep(0.5)
    if pending:
        print(f"等待超时：{', '.join(sorted(pending))}；进程仍在运行，请查看 .run/logs。")
        return False
    return True


def start(args: argparse.Namespace, environment: dict[str, str]) -> int:
    existing = read_state()
    running = [name for name, item in existing.items() if process_alive(int(item["pid"]))]
    if running:
        print(f"已有服务运行：{', '.join(running)}。请先执行 stop。")
        return 1

    required = ["GEO_DATABASE_DSN", "GEO_MIGRATION_DATABASE_URL"]
    missing = [key for key in required if not environment.get(key, "").strip()]
    if missing:
        print(f"缺少环境变量：{', '.join(missing)}。请复制并配置 .env.example 为 .env。")
        return 1

    if args.install:
        run_checked(["pnpm", "install"], ROOT, environment)
        run_checked(["uv", "sync"], ROOT / "apps" / "judge-worker", environment)
    if not args.skip_migrate:
        run_checked(["go", "run", "./cmd/migrate", "up"], ROOT, environment)

    state: dict[str, dict[str, Any]] = {}
    handles: list[IO[bytes]] = []
    try:
        for name, specification in SERVICES.items():
            process, stdout_file, stderr_file = spawn_service(name, specification, environment)
            handles.extend((stdout_file, stderr_file))
            state[name] = {
                "pid": process.pid,
                "command": specification["command"],
                "health": specification["health"],
                "stdout_log": str(LOG_DIR / f"{name}.log"),
                "stderr_log": str(LOG_DIR / f"{name}.error.log"),
                "started_at": time.strftime("%Y-%m-%dT%H:%M:%S%z"),
            }
            write_state(state)
            print(f"已启动：{name} (PID {process.pid})")
    except BaseException:
        stop_processes(state)
        raise
    finally:
        for handle in handles:
            handle.close()

    healthy = wait_for_services(state, args.timeout)
    print(f"日志目录：{LOG_DIR}")
    return 0 if healthy else 1


def terminate_process_tree(pid: int) -> None:
    if not process_alive(pid):
        return
    if os.name == "nt":
        subprocess.run(
            ["taskkill", "/PID", str(pid), "/T", "/F"],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
            check=False,
        )
        return
    try:
        os.killpg(pid, signal.SIGTERM)
    except ProcessLookupError:
        return
    deadline = time.monotonic() + 5
    while process_alive(pid) and time.monotonic() < deadline:
        time.sleep(0.1)
    if process_alive(pid):
        os.killpg(pid, signal.SIGKILL)


def stop_processes(state: dict[str, dict[str, Any]]) -> None:
    for name, item in reversed(list(state.items())):
        pid = int(item["pid"])
        if process_alive(pid):
            terminate_process_tree(pid)
            print(f"已停止：{name} (PID {pid})")


def stop() -> int:
    state = read_state()
    if not state:
        print("没有已记录的开发服务。")
        return 0
    stop_processes(state)
    STATE_FILE.unlink(missing_ok=True)
    return 0


def status() -> int:
    state = read_state()
    if not state:
        print("没有已记录的开发服务。")
        return 1
    all_running = True
    for name, item in state.items():
        running = process_alive(int(item["pid"]))
        healthy = running and health_available(str(item["health"]))
        state_text = "healthy" if healthy else "starting" if running else "stopped"
        print(f"{name:8} {state_text:8} PID={item['pid']} {item['health']}")
        all_running = all_running and running and healthy
    return 0 if all_running else 1


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="管理 GEO 原生本地开发服务")
    parser.add_argument("action", choices=("start", "stop", "restart", "status"), nargs="?", default="start")
    parser.add_argument("--install", action="store_true", help="启动前安装 pnpm 和 uv 依赖")
    parser.add_argument("--skip-migrate", action="store_true", help="跳过数据库迁移")
    parser.add_argument("--timeout", type=int, default=45, help="服务就绪等待秒数，默认 45")
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    environment = os.environ.copy()
    load_dotenv(ROOT / ".env", environment)
    if args.action == "stop":
        return stop()
    if args.action == "status":
        return status()
    if args.action == "restart":
        stop()
    return start(args, environment)


if __name__ == "__main__":
    raise SystemExit(main())
