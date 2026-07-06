"""bench — -np 集計はデコード時間ベース(pp込み壁時間で tg t/s を過少報告しない)."""

import math
from typing import Any

from moka_eval.bench import _np_summary
from moka_eval.config import GREEDY, ModelSpec

_SPEC = ModelSpec(key="model-x", hf="x/y", sampling=GREEDY)


def _result(predicted_n: int, predicted_ms: float, tps: float) -> dict[str, Any]:
    return {
        "timings": {
            "predicted_n": predicted_n,
            "predicted_ms": predicted_ms,
            "predicted_per_second": tps,
            "prompt_ms": 1000.0,
        }
    }


def test_np_summary_aggregate_tg_uses_decode_time_only() -> None:
    # 各ストリーム 100tok / 2s デコード(pp 1s)、壁時間 3s。
    # pp込み壁時間だと 200/3=66.7 に過少化する — デコード窓 2s で 100.0 が正
    results = [_result(100, 2000.0, 50.0), _result(100, 2000.0, 50.0)]
    summary = _np_summary(_SPEC, np=2, results=results, wall_s=3.0)
    assert math.isclose(summary["aggregate_tg_tps"], 100.0)


def test_np_summary_reports_e2e_separately() -> None:
    results = [_result(100, 2000.0, 50.0), _result(100, 2000.0, 50.0)]
    summary = _np_summary(_SPEC, np=2, results=results, wall_s=3.0)
    assert math.isclose(summary["aggregate_e2e_tps"], round(200 / 3.0, 1))
    assert summary["per_stream_tg_tps"] == [50.0, 50.0]
    assert summary["wall_s"] == 3.0
