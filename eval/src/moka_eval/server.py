"""llama.cpp server のアドホック起動 — compose.yaml の llm サービス定義を継承する.

`docker compose run` を使うことで devices(/dev/dri)・group_add・LLAMA_CACHE・
イメージ pin(server-vulkan-b9859)の単一の真実を compose.yaml に保つ(ADR00006)。
"""

import subprocess
import time
from collections.abc import Iterator, Sequence
from contextlib import contextmanager
from dataclasses import dataclass

import httpx

from moka_eval.config import CHAT_PORT, REPO_ROOT, SERVER_LOG_DIR, ModelSpec


class ServerStartError(RuntimeError):
    """llama.cpp server が ready にならなかった."""


@dataclass
class ServerHandle:
    port: int
    base_url: str
    container: str
    load_time_s: float


def _container_name(port: int) -> str:
    return f"moka-eval-llm-{port}"


def build_command(
    spec: ModelSpec, *, port: int, np: int = 1, extra: Sequence[str] = ()
) -> list[str]:
    name = _container_name(port)
    cmd = [
        "docker",
        "compose",
        "--project-directory",
        str(REPO_ROOT),
        "run",
        "--rm",
        "--no-deps",
        "--name",
        name,
        "--publish",
        f"127.0.0.1:{port}:{port}",
        "llm",
        "-hf",
        spec.hf,
        "--host",
        "0.0.0.0",  # noqa: S104 — コンテナ内 bind、公開は 127.0.0.1 のみ
        "--port",
        str(port),
        "-ngl",
        "99",
    ]
    if np > 1:
        cmd += ["-np", str(np)]
    if spec.ctx is not None:
        cmd += ["-c", str(spec.ctx)]
    if spec.mode == "embed":
        cmd += ["--embedding"]
        if spec.pooling is not None:
            cmd += ["--pooling", spec.pooling]
    cmd += list(spec.extra_flags)
    cmd += list(extra)
    return cmd


def _force_remove(container: str) -> None:
    subprocess.run(
        ["docker", "rm", "-f", container],
        check=False,
        capture_output=True,
    )


@contextmanager
def llama_server(
    spec: ModelSpec,
    *,
    port: int = CHAT_PORT,
    np: int = 1,
    extra: Sequence[str] = (),
    ready_timeout_s: float = 1800.0,
) -> Iterator[ServerHandle]:
    """起動 → /health 200 まで待機 → yield → 確実に破棄。load_time_s を記録する."""
    name = _container_name(port)
    _force_remove(name)  # 前回の残骸対策
    SERVER_LOG_DIR.mkdir(parents=True, exist_ok=True)
    log_path = SERVER_LOG_DIR / f"{name}.log"
    cmd = build_command(spec, port=port, np=np, extra=extra)
    with log_path.open("ab") as log:
        log.write(f"\n=== {time.strftime('%F %T')} {' '.join(cmd)}\n".encode())
        proc = subprocess.Popen(cmd, stdout=log, stderr=subprocess.STDOUT)
        try:
            load_time_s = _wait_ready(proc, port, ready_timeout_s)
            yield ServerHandle(
                port=port,
                base_url=f"http://127.0.0.1:{port}",
                container=name,
                load_time_s=load_time_s,
            )
        finally:
            _force_remove(name)
            proc.terminate()
            try:
                proc.wait(timeout=30)
            except subprocess.TimeoutExpired:
                proc.kill()


def _wait_ready(proc: subprocess.Popen[bytes], port: int, timeout_s: float) -> float:
    url = f"http://127.0.0.1:{port}/health"
    started = time.monotonic()
    with httpx.Client(timeout=5.0) as http:
        while True:
            elapsed = time.monotonic() - started
            if elapsed > timeout_s:
                msg = f"server on :{port} not ready after {timeout_s:.0f}s"
                raise ServerStartError(msg)
            if proc.poll() is not None:
                msg = f"server process exited early (rc={proc.returncode}); see server log"
                raise ServerStartError(msg)
            try:
                if http.get(url).status_code == 200:
                    return time.monotonic() - started
            except httpx.HTTPError:
                pass
            time.sleep(1.0)


def measure_load_times(spec: ModelSpec, *, port: int = CHAT_PORT, n: int = 3) -> list[float]:
    """起動→ready の wall time を n 回。平均せず全回返す(初回はディスク律速)."""
    times: list[float] = []
    for _ in range(n):
        with llama_server(spec, port=port) as handle:
            times.append(handle.load_time_s)
    return times
