"""amdgpu の sysfs メモリカウンタ読み取り(VRAM実プローブ用)."""

from pathlib import Path

_COUNTERS = ("vram_total", "vram_used", "gtt_total", "gtt_used", "vis_vram_total")


class GpuSysfsError(RuntimeError):
    """amdgpu の sysfs カウンタが見つからない."""


def _device_dir() -> Path:
    for candidate in sorted(Path("/sys/class/drm").glob("card*/device")):
        if (candidate / "mem_info_vram_total").is_file():
            return candidate
    msg = "no /sys/class/drm/card*/device with mem_info_vram_total"
    raise GpuSysfsError(msg)


def mem_snapshot() -> dict[str, int]:
    """バイト単位の {vram_total, vram_used, gtt_total, gtt_used, vis_vram_total}."""
    device = _device_dir()
    snapshot: dict[str, int] = {}
    for name in _COUNTERS:
        path = device / f"mem_info_{name}"
        if path.is_file():
            snapshot[name] = int(path.read_text().strip())
    return snapshot


def gib(n_bytes: int) -> float:
    return round(n_bytes / 2**30, 2)


def format_snapshot(snap: dict[str, int]) -> str:
    vram = f"VRAM {gib(snap['vram_used'])}/{gib(snap['vram_total'])} GiB"
    gtt = f"GTT {gib(snap['gtt_used'])}/{gib(snap['gtt_total'])} GiB"
    return f"{vram}, {gtt}"
