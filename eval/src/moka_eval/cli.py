"""moka-eval CLI — 計測ランブックの入り口(argparse サブコマンド)."""

import argparse
import contextlib
import json
import sys
from datetime import UTC, datetime

from moka_eval import bench, dataset, embed, generate, judge, report
from moka_eval.client import LlamaClient
from moka_eval.config import (
    CHAT_PORT,
    EMBED_PORT,
    LLAMACPP_BUILD,
    MODELS,
    RESULTS_DIR,
    model,
)
from moka_eval.gpu import format_snapshot, mem_snapshot
from moka_eval.server import llama_server, measure_load_times

_STOP_REMINDER = (
    "注意: 計測前に `docker compose stop llm` を推奨(同一iGPUを常駐LFM2.5と取り合う)。"
    "終了後は `docker compose start llm` で復帰。"
)


def _now_iso() -> str:
    return datetime.now(UTC).isoformat(timespec="seconds")


def _write_summary(name: str, payload: dict[str, object]) -> None:
    out_dir = RESULTS_DIR / "summary"
    out_dir.mkdir(parents=True, exist_ok=True)
    path = out_dir / f"{name}.json"
    path.write_text(json.dumps(payload, ensure_ascii=False, indent=2), encoding="utf-8")
    print(f"wrote {path}")


def cmd_smoke(_: argparse.Namespace) -> int:
    print(_STOP_REMINDER)
    spec = model("lfm25")
    print(f"baseline: {format_snapshot(mem_snapshot())}")
    with llama_server(spec) as handle:
        print(f"ready in {handle.load_time_s:.1f}s: {format_snapshot(mem_snapshot())}")
        with LlamaClient(handle.base_url) as client:
            result = client.chat("1+1は?数字のみで答えて。", sampling=spec.sampling, max_tokens=256)
            print(f"answer={result.answer!r} cot_len={len(result.cot or '')}")
            print(f"ttft={result.ttft_ms:.0f}ms total={result.total_ms:.0f}ms")
    print("smoke OK")
    return 0


def cmd_bench(args: argparse.Namespace) -> int:
    print(_STOP_REMINDER)
    spec = model(args.model)
    np: int = args.np
    with llama_server(spec, np=np) as handle:
        print(f"load {handle.load_time_s:.1f}s, {format_snapshot(mem_snapshot())}")
        if np > 1:
            summary = bench.run_np_bench(handle.base_url, spec, np=np)
            print(json.dumps(summary, ensure_ascii=False, indent=2))
            _write_summary(f"bench-np{np}-{spec.key}", summary)
            return 0
        with LlamaClient(handle.base_url) as client:
            records = bench.run_bench(client, spec)
    for record in records:
        print(
            f"  pp {record.pp_tps} t/s | tg {record.tg_tps} t/s | VRAM {record.vram_used_gib} GiB"
        )
    _write_summary(
        f"bench-{spec.key}",
        {
            "model_key": spec.key,
            "model_hf": spec.hf,
            "llamacpp_build": LLAMACPP_BUILD,
            "reps": [r.model_dump() for r in records],
            "created_at": _now_iso(),
        },
    )
    return 0


def cmd_loadtime(args: argparse.Namespace) -> int:
    print(_STOP_REMINDER)
    spec = model(args.model)
    times = measure_load_times(spec, n=args.n)
    print(f"{spec.key}: {[round(t, 1) for t in times]} s")
    _write_summary(
        f"loadtime-{spec.key}",
        {
            "model_key": spec.key,
            "model_hf": spec.hf,
            "load_times_s": [round(t, 1) for t in times],
            "note": "1回目=コールド寄り、以降=ページキャッシュ次第(ホストRAM16GB)",
            "created_at": _now_iso(),
        },
    )
    return 0


def cmd_probe(args: argparse.Namespace) -> int:
    """常駐セット+35Bの同時ロードで VRAM 実挙動を確認する(D2).

    KVキャッシュのデフォルト確保(モデル最大コンテキスト分)が実態を歪める+
    ホストRAM 16GB を圧迫して OOM を招くため、コンテキストは --ctx で明示的に
    制限する(初回プローブは無制限で 5 モデル目のベンチ中にホスト OOM を実測済み)。
    """
    print(_STOP_REMINDER)
    resident_keys = ["lfm25", "qwen35-4b", "gemma4-e4b", "gptoss20b"]
    swap_key: str = args.swap
    ctx_flags = ["-c", str(args.ctx)] if args.ctx else []
    steps: list[dict[str, object]] = []
    baseline = mem_snapshot()
    print(f"baseline: {format_snapshot(baseline)} (ctx={args.ctx or 'model-default'})")
    steps.append({"step": "baseline", "ctx": args.ctx, "mem": baseline})
    with contextlib.ExitStack() as stack:
        port = CHAT_PORT
        handles = {}
        for key in [*resident_keys, swap_key]:
            spec = model(key)
            handle = stack.enter_context(llama_server(spec, port=port, extra=ctx_flags))
            snap = mem_snapshot()
            handles[key] = handle
            print(f"+{key} (load {handle.load_time_s:.0f}s): {format_snapshot(snap)}")
            steps.append(
                {"step": f"+{key}", "load_time_s": round(handle.load_time_s, 1), "mem": snap}
            )
            port += 1
        # 全常駐状態での劣化チェック: 最初と最後のモデルで軽くベンチ
        for key in (resident_keys[0], swap_key):
            with LlamaClient(handles[key].base_url) as client:
                records = bench.run_bench(client, model(key), reps=2)
            steps.append(
                {
                    "step": f"bench-under-full-residency:{key}",
                    "reps": [r.model_dump() for r in records],
                }
            )
            print(f"bench {key}: tg {[r.tg_tps for r in records]} t/s")
    _write_summary("vram-probe", {"steps": steps, "created_at": _now_iso()})
    return 0


def cmd_dataset(args: argparse.Namespace) -> int:
    if args.action == "fetch":
        articles, feed_items = dataset.fetch_dataset()
        dataset.save_dataset(articles, feed_items)
        print(f"saved {len(articles)} articles, {len(feed_items)} feed items")
        return 0
    if args.action == "verify":
        problems = dataset.verify_dataset()
        if problems:
            print("\n".join(problems))
            return 1
        print("dataset OK")
        return 0
    if args.action == "recap":
        weeks = dataset.build_recap_weeks()
        print(f"built {len(weeks)} recap weeks")
        return 0
    print(f"unknown action {args.action}")
    return 2


def cmd_generate(args: argparse.Namespace) -> int:
    print(_STOP_REMINDER)
    spec = model(args.model)
    task: str = args.task
    run_id: str = args.run_id
    seeds: list[int] = [int(s) for s in args.seeds.split(",")]
    template, prompt_sha = generate.load_prompt(task)
    if task == "recap":
        items: list[tuple[str, str]] = [
            (w.week_id, generate.render_recap_prompt(template, w))
            for w in dataset.load_recap_weeks()
        ]
        # thinking系35BはリキャップでCoTが4096超になる実測(qwen35-35bで3/3打ち切り)。
        # 品質最優先バッチなのでCoT予算を大きく取る
        max_tokens = 8192
    else:
        articles = dataset.load_articles()
        if args.limit:
            articles = articles[: args.limit]
        items = [(a.id, generate.render_article_prompt(template, a)) for a in articles]
        max_tokens = 2048 if spec.thinking else 1024
    schema = generate.TagResult.model_json_schema() if task == "tags" else None

    with llama_server(spec) as handle:
        server_flags = [f"-hf {spec.hf}", "-ngl 99"]
        generate.write_run_meta(
            run_id,
            {
                "run_id": run_id,
                "model_key": spec.key,
                "model_hf": spec.hf,
                "task": task,
                "llamacpp_build": LLAMACPP_BUILD,
                "seeds": seeds,
                "prompt_sha256": prompt_sha,
                "n_items": len(items),
                "load_time_s": round(handle.load_time_s, 1),
                "started_at": _now_iso(),
            },
        )
        with LlamaClient(handle.base_url) as client:
            client.chat("ウォームアップ。", sampling=spec.sampling, max_tokens=32)  # 捨てる
            total = len(items) * len(seeds)
            done = 0
            for seed in seeds:
                for item_id, prompt in items:
                    record = generate.generate_one(
                        client,
                        spec,
                        run_id=run_id,
                        task=task,
                        item_id=item_id,
                        prompt=prompt,
                        prompt_sha=prompt_sha,
                        seed=seed,
                        max_tokens=max_tokens,
                        server_flags=server_flags,
                        json_schema=schema,
                    )
                    generate.append_record(run_id, record)
                    done += 1
                    print(
                        f"[{done}/{total}] {item_id} seed={seed} "
                        f"{record.total_ms / 1000:.1f}s ans={record.answer_tokens}tok "
                        f"cot={record.cot_tokens}tok eff={record.effective_answer_tps}t/s"
                    )
    print(f"run {run_id} complete")
    return 0


def cmd_pairs(args: argparse.Namespace) -> int:
    gen_a = [r.model_dump() for r in generate.load_generations(args.run_a)]
    gen_b = [r.model_dump() for r in generate.load_generations(args.run_b)]
    task: str = args.task
    gen_a = [g for g in gen_a if g["task"] == task and g["seed"] == args.seed_of_runs]
    gen_b = [g for g in gen_b if g["task"] == task and g["seed"] == args.seed_of_runs]
    pairs, key = judge.make_pairs(gen_a, gen_b, seed=args.seed, task=task)
    judge.save_pairs(args.name, pairs, key)
    print(f"saved {len(pairs)} blinded pairs as {args.name}")
    return 0


def cmd_judge(args: argparse.Namespace) -> int:
    print(_STOP_REMINDER)
    judge_spec = model(args.judge)
    pairs, _ = judge.load_pairs(args.name)
    lenses = args.lenses.split(",") if args.lenses else list(judge.LENSES)
    template, prompt_sha = judge.load_judge_prompt()
    sources = _sources_for_pairs(pairs)
    with llama_server(judge_spec) as handle, LlamaClient(handle.base_url) as client:
        client.chat("ウォームアップ。", sampling=judge_spec.sampling, max_tokens=32)
        total = len(pairs) * len(lenses)
        done = 0
        for pair in pairs:
            for lens in lenses:
                record = judge.judge_pair(
                    client,
                    judge_spec,
                    pair,
                    lens=lens,
                    source=sources[pair.pair_id],
                    template=template,
                    prompt_sha=prompt_sha,
                )
                judge.append_verdict(args.name, judge_spec.key, record)
                done += 1
                print(
                    f"[{done}/{total}] {pair.pair_id} {lens}: {record.verdict} "
                    f"(orig={record.winner_orig}, swap={record.winner_swap})"
                )
    print(f"verdicts written for judge={judge_spec.key}")
    return 0


def _sources_for_pairs(pairs: list[judge.BlindPair]) -> dict[str, str]:
    """審判に見せる原文(要約系=記事本文、recap=週次エントリ一覧)."""
    sources: dict[str, str] = {}
    articles = {a.id: a for a in dataset.load_articles()}
    weeks = {}
    with contextlib.suppress(dataset.DatasetError):
        weeks = {w.week_id: w for w in dataset.load_recap_weeks()}
    for pair in pairs:
        if pair.article_id in articles:
            a = articles[pair.article_id]
            sources[pair.pair_id] = f"{a.title}\n\n{a.text}"
        elif pair.article_id in weeks:
            week = weeks[pair.article_id]
            sources[pair.pair_id] = "\n".join(f"- {i.title}: {i.summary}" for i in week.entries)
        else:
            msg = f"no source for {pair.pair_id}"
            raise judge.JudgeError(msg)
    return sources


def cmd_score(args: argparse.Namespace) -> int:
    _, key = judge.load_pairs(args.name)
    verdicts_by_judge = {
        judge_key: judge.load_verdicts(args.name, judge_key) for judge_key in args.judges.split(",")
    }
    summary = report.score_decision(args.name, verdicts_by_judge, key)
    print(json.dumps(summary, ensure_ascii=False, indent=2))
    return 0


def cmd_embed(args: argparse.Namespace) -> int:
    print(_STOP_REMINDER)
    spec = model(args.model)
    dims = tuple(int(d) for d in args.dims.split(",")) if args.dims else ()
    with llama_server(spec, port=EMBED_PORT) as handle, LlamaClient(handle.base_url) as client:
        summary = embed.run_embed_eval(client, spec, mrl_dims=dims)
    print(json.dumps(summary, ensure_ascii=False, indent=2))
    return 0


def cmd_latency(args: argparse.Namespace) -> int:
    records = generate.load_generations(args.run_id)
    print(json.dumps(report.latency_summary(records), ensure_ascii=False, indent=2))
    return 0


def cmd_models(_: argparse.Namespace) -> int:
    for key, spec in MODELS.items():
        print(f"{key:12s} {spec.mode:5s} {spec.hf}")
    return 0


def main() -> int:
    parser = argparse.ArgumentParser(prog="moka-eval", description=__doc__)
    sub = parser.add_subparsers(dest="command", required=True)

    sub.add_parser("smoke").set_defaults(func=cmd_smoke)
    sub.add_parser("models").set_defaults(func=cmd_models)

    p = sub.add_parser("bench")
    p.add_argument("--model", required=True)
    p.add_argument("--np", type=int, default=1)
    p.set_defaults(func=cmd_bench)

    p = sub.add_parser("loadtime")
    p.add_argument("--model", required=True)
    p.add_argument("-n", type=int, default=3)
    p.set_defaults(func=cmd_loadtime)

    p = sub.add_parser("probe")
    p.add_argument("--swap", default="qwen36-35b")
    p.add_argument("--ctx", type=int, default=8192)
    p.set_defaults(func=cmd_probe)

    p = sub.add_parser("dataset")
    p.add_argument("action", choices=["fetch", "verify", "recap"])
    p.set_defaults(func=cmd_dataset)

    p = sub.add_parser("generate")
    p.add_argument("--model", required=True)
    p.add_argument("--task", required=True, choices=["summarize", "tags", "recap"])
    p.add_argument("--run-id", required=True)
    p.add_argument("--seeds", default="42")
    p.add_argument("--limit", type=int, default=0)
    p.set_defaults(func=cmd_generate)

    p = sub.add_parser("pairs")
    p.add_argument("--run-a", required=True)
    p.add_argument("--run-b", required=True)
    p.add_argument("--task", required=True)
    p.add_argument("--name", required=True)
    p.add_argument("--seed", type=int, default=7)
    p.add_argument("--seed-of-runs", type=int, default=42)
    p.set_defaults(func=cmd_pairs)

    p = sub.add_parser("judge")
    p.add_argument("--name", required=True)
    p.add_argument("--judge", required=True)
    p.add_argument("--lenses", default="")
    p.set_defaults(func=cmd_judge)

    p = sub.add_parser("score")
    p.add_argument("--name", required=True)
    p.add_argument("--judges", required=True, help="comma-separated judge keys")
    p.set_defaults(func=cmd_score)

    p = sub.add_parser("embed")
    p.add_argument("--model", required=True)
    p.add_argument("--dims", default="")
    p.set_defaults(func=cmd_embed)

    p = sub.add_parser("latency")
    p.add_argument("--run-id", required=True)
    p.set_defaults(func=cmd_latency)

    args = parser.parse_args()
    result: int = args.func(args)
    return result


if __name__ == "__main__":
    sys.exit(main())
