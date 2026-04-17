#!/usr/bin/env python3
from __future__ import annotations

import argparse
import re
from collections import defaultdict
from pathlib import Path
from statistics import mean, pstdev

import matplotlib.pyplot as plt

ENGINE_LINE = re.compile(
    r"^BenchmarkEngineComparison/(?P<engine>[^/]+)/size=(?P<size>\d+)-\d+\s+\d+\s+(?P<ns>[\d.]+)\s+ns/op\s+(?P<bop>[\d.]+)\s+B/op\s+(?P<alloc>[\d.]+)\s+allocs/op"
)

PARALLEL_LINE = re.compile(
    r"^BenchmarkParallelSearch-\d+\s+\d+\s+(?P<ns>[\d.]+)\s+ns/op\s+(?P<mbps>[\d.]+)\s+MB/s\s+(?P<bop>[\d.]+)\s+B/op\s+(?P<alloc>[\d.]+)\s+allocs/op"
)

ENGINE_ORDER = ["scalar", "kmp", "stdlib", "simd"]
ENGINE_LABEL = {
    "scalar": "Scalar",
    "kmp": "KMP",
    "stdlib": "Stdlib",
    "simd": "SIMD",
}
ENGINE_COLOR = {
    "scalar": "#495057",
    "kmp": "#c2255c",
    "stdlib": "#e67700",
    "simd": "#0b7285",
}

PARALLEL_COLOR = {
    "default": "#4c6ef5",
    "pcore6": "#2f9e44",
}


def size_label(size: int) -> str:
    if size == 4 * 1024:
        return "4KB"
    if size == 64 * 1024:
        return "64KB"
    if size == 1024 * 1024:
        return "1MB"
    if size == 16 * 1024 * 1024:
        return "16MB"
    if size % (1024 * 1024) == 0:
        return f"{size // (1024 * 1024)}MB"
    if size % 1024 == 0:
        return f"{size // 1024}KB"
    return str(size)


def parse_engine_results(path: Path):
    engine_samples: dict[tuple[str, int], dict[str, list[float]]] = defaultdict(
        lambda: {"ns": [], "bop": [], "alloc": []}
    )

    with path.open("r", encoding="utf-8") as f:
        for raw in f:
            line = raw.strip()
            if not line:
                continue

            m = ENGINE_LINE.match(line)
            if m:
                engine = m.group("engine").lower()
                size = int(m.group("size"))
                key = (engine, size)
                engine_samples[key]["ns"].append(float(m.group("ns")))
                engine_samples[key]["bop"].append(float(m.group("bop")))
                engine_samples[key]["alloc"].append(float(m.group("alloc")))

    return engine_samples


def parse_parallel_results(path: Path):
    samples = {"ns": [], "mbps": [], "bop": [], "alloc": []}

    with path.open("r", encoding="utf-8") as f:
        for raw in f:
            line = raw.strip()
            if not line:
                continue

            m = PARALLEL_LINE.match(line)
            if not m:
                continue

            samples["ns"].append(float(m.group("ns")))
            samples["mbps"].append(float(m.group("mbps")))
            samples["bop"].append(float(m.group("bop")))
            samples["alloc"].append(float(m.group("alloc")))

    return samples


def aggregate_engine(engine_samples):
    rows = []
    for (engine, size), data in engine_samples.items():
        ns_avg = mean(data["ns"])
        ns_std = pstdev(data["ns"]) if len(data["ns"]) > 1 else 0.0
        bop_avg = mean(data["bop"])
        alloc_avg = mean(data["alloc"])
        throughput_gbps = size / ns_avg
        rows.append(
            {
                "engine": engine,
                "size": size,
                "size_label": size_label(size),
                "ns_avg": ns_avg,
                "ns_std": ns_std,
                "bop_avg": bop_avg,
                "alloc_avg": alloc_avg,
                "gbps": throughput_gbps,
            }
        )

    rows.sort(key=lambda r: (r["size"], ENGINE_ORDER.index(r["engine"]) if r["engine"] in ENGINE_ORDER else 999))
    return rows


def aggregate_parallel(parallel_samples):
    if not parallel_samples["ns"]:
        return None

    return {
        "ns_avg": mean(parallel_samples["ns"]),
        "ns_std": pstdev(parallel_samples["ns"]) if len(parallel_samples["ns"]) > 1 else 0.0,
        "mbps_avg": mean(parallel_samples["mbps"]),
        "mbps_std": pstdev(parallel_samples["mbps"]) if len(parallel_samples["mbps"]) > 1 else 0.0,
        "bop_avg": mean(parallel_samples["bop"]),
        "alloc_avg": mean(parallel_samples["alloc"]),
    }


def format_ns(value: float) -> str:
    if value >= 1_000_000:
        return f"{value / 1_000_000:.2f} ms"
    if value >= 1_000:
        return f"{value / 1_000:.2f} us"
    return f"{value:.1f} ns"


def format_bytes(value: float) -> str:
    if value >= 1024 * 1024:
        return f"{value / (1024 * 1024):.2f} MiB"
    if value >= 1024:
        return f"{value / 1024:.2f} KiB"
    return f"{value:.1f} B"


def format_allocs(value: float) -> str:
    return f"{value:.2f}"


def plot_engine_metric_bars(rows, metric_key: str, title: str, x_label: str, formatter, out_file: Path):
    sizes = sorted({r["size"] for r in rows})
    if not sizes:
        return

    value_by_key = {(r["size"], r["engine"]): r[metric_key] for r in rows}

    plt.style.use("seaborn-v0_8-whitegrid")
    fig_height = max(5.0, len(sizes) * 2.3 + 1.4)
    fig, axes = plt.subplots(len(sizes), 1, figsize=(11.2, fig_height))
    fig.patch.set_facecolor("#f8f9fa")
    if len(sizes) == 1:
        axes = [axes]

    for ax, size in zip(axes, sizes):
        ax.set_facecolor("#ffffff")
        engines = [engine for engine in ENGINE_ORDER if (size, engine) in value_by_key]
        labels = [ENGINE_LABEL.get(engine, engine) for engine in engines]
        values = [value_by_key[(size, engine)] for engine in engines]
        colors = [ENGINE_COLOR.get(engine, "#495057") for engine in engines]

        bars = ax.barh(labels, values, color=colors, alpha=0.9)
        ax.invert_yaxis()
        ax.set_title(f"{size_label(size)}", loc="left", fontsize=12, fontweight="bold")
        ax.grid(axis="x", alpha=0.25)

        if values:
            max_value = max(values)
            x_padding = max_value * 0.18 if max_value > 0 else 1.0
            ax.set_xlim(0, max_value + x_padding)

            for bar, value in zip(bars, values):
                ax.text(
                    value + x_padding * 0.05,
                    bar.get_y() + bar.get_height() / 2,
                    formatter(value),
                    va="center",
                    ha="left",
                    fontsize=10,
                    fontweight="bold",
                    color="#111827",
                )

    fig.suptitle(title, fontsize=16, fontweight="bold")
    fig.supxlabel(x_label, fontsize=12, fontweight="bold")
    fig.tight_layout(rect=[0.02, 0.02, 1.0, 0.97])
    fig.savefig(out_file, dpi=220)
    plt.close(fig)


def plot_parallel_modes(default_summary, pcore_summary, out_file: Path):
    if default_summary is None or pcore_summary is None:
        return

    plt.style.use("seaborn-v0_8-whitegrid")
    fig, axes = plt.subplots(1, 2, figsize=(11.6, 5.1))
    fig.patch.set_facecolor("#f8f9fa")

    labels = ["Default", "GOMAXPROCS=6"]
    colors = [PARALLEL_COLOR["default"], PARALLEL_COLOR["pcore6"]]

    ns_vals = [default_summary["ns_avg"] / 1e6, pcore_summary["ns_avg"] / 1e6]
    ns_err = [default_summary["ns_std"] / 1e6, pcore_summary["ns_std"] / 1e6]

    mb_vals = [default_summary["mbps_avg"] / 1024.0, pcore_summary["mbps_avg"] / 1024.0]
    mb_err = [default_summary["mbps_std"] / 1024.0, pcore_summary["mbps_std"] / 1024.0]

    ax0 = axes[0]
    ax0.set_facecolor("#ffffff")
    bars0 = ax0.bar(labels, ns_vals, yerr=ns_err, color=colors, alpha=0.9, capsize=6)
    ax0.set_title("ParallelSearch Latency")
    ax0.set_ylabel("ms/op")
    for bar, value in zip(bars0, ns_vals):
        ax0.text(bar.get_x() + bar.get_width() / 2.0, value, f"{value:.2f}", ha="center", va="bottom", fontsize=10)

    ax1 = axes[1]
    ax1.set_facecolor("#ffffff")
    bars1 = ax1.bar(labels, mb_vals, yerr=mb_err, color=colors, alpha=0.9, capsize=6)
    ax1.set_title("ParallelSearch Throughput")
    ax1.set_ylabel("GiB/s")
    for bar, value in zip(bars1, mb_vals):
        ax1.text(bar.get_x() + bar.get_width() / 2.0, value, f"{value:.2f}", ha="center", va="bottom", fontsize=10)

    fig.suptitle("Bytestorm Parallel Profile", fontsize=15, fontweight="bold")
    fig.tight_layout()
    fig.savefig(out_file, dpi=220)
    plt.close(fig)


def write_engine_markdown(rows, path: Path):
    header = "| Size | Engine | ns/op (avg) | GB/s (avg) | B/op (avg) | allocs/op (avg) |\n| --- | --- | ---: | ---: | ---: | ---: |\n"
    lines = [header]
    for r in rows:
        lines.append(
            f"| {r['size_label']} | {ENGINE_LABEL.get(r['engine'], r['engine'])} | {r['ns_avg']:.1f} | {r['gbps']:.3f} | {r['bop_avg']:.1f} | {r['alloc_avg']:.2f} |\n"
        )
    path.write_text("".join(lines), encoding="utf-8")


def write_parallel_markdown(default_summary, pcore_summary, path: Path):
    if default_summary is None:
        path.write_text("No BenchmarkParallelSearch data found.\n", encoding="utf-8")
        return

    text = [
        "| Mode | ns/op (avg) | MB/s (avg) | B/op (avg) | allocs/op (avg) |\n",
        "| --- | ---: | ---: | ---: | ---: |\n",
        f"| default | {default_summary['ns_avg']:.1f} | {default_summary['mbps_avg']:.2f} | {default_summary['bop_avg']:.1f} | {default_summary['alloc_avg']:.2f} |\n",
    ]

    if pcore_summary is not None:
        text.append(
            f"| gomaxprocs=6 | {pcore_summary['ns_avg']:.1f} | {pcore_summary['mbps_avg']:.2f} | {pcore_summary['bop_avg']:.1f} | {pcore_summary['alloc_avg']:.2f} |\n"
        )
        delta_ns = (pcore_summary["ns_avg"] / default_summary["ns_avg"] - 1.0) * 100.0
        delta_tp = (pcore_summary["mbps_avg"] / default_summary["mbps_avg"] - 1.0) * 100.0
        text.append("\n")
        text.append(f"Latency delta (gomaxprocs=6 vs default): {delta_ns:+.2f}%\n")
        text.append(f"Throughput delta (gomaxprocs=6 vs default): {delta_tp:+.2f}%\n")

    path.write_text("".join(text), encoding="utf-8")


def resolve_input_path(path_arg: str) -> Path:
    explicit = Path(path_arg)
    if explicit.exists():
        return explicit

    fallback = Path("docs/bench_results") / explicit.name
    if fallback.exists():
        return fallback

    raise SystemExit(f"Benchmark file not found: {path_arg}")


def main() -> None:
    parser = argparse.ArgumentParser(description="Generate benchmark plots for Bytestorm")
    parser.add_argument("--engine", default="bench_engine_final.txt", help="Path to engine benchmark output")
    parser.add_argument(
        "--parallel-default",
        default="bench_parallel_final_default.txt",
        help="Path to default parallel benchmark output",
    )
    parser.add_argument(
        "--parallel-pcore6",
        default="bench_parallel_final_pcore6.txt",
        help="Path to GOMAXPROCS=6 parallel benchmark output",
    )
    parser.add_argument("--out-dir", default="docs/images", help="Output directory for charts")
    args = parser.parse_args()

    engine_path = resolve_input_path(args.engine)
    parallel_default_path = resolve_input_path(args.parallel_default)
    parallel_pcore_path = Path(args.parallel_pcore6)
    if not parallel_pcore_path.exists():
        fallback_parallel = Path("docs/bench_results") / parallel_pcore_path.name
        if fallback_parallel.exists():
            parallel_pcore_path = fallback_parallel

    out_dir = Path(args.out_dir)
    out_dir.mkdir(parents=True, exist_ok=True)

    engine_samples = parse_engine_results(engine_path)
    parallel_default_samples = parse_parallel_results(parallel_default_path)
    parallel_pcore_samples = parse_parallel_results(parallel_pcore_path) if parallel_pcore_path.exists() else {"ns": [], "mbps": [], "bop": [], "alloc": []}

    rows = aggregate_engine(engine_samples)
    parallel_default_summary = aggregate_parallel(parallel_default_samples)
    parallel_pcore_summary = aggregate_parallel(parallel_pcore_samples)

    if not rows:
        raise SystemExit("No BenchmarkEngineComparison data found in engine file")

    plot_engine_metric_bars(
        rows,
        metric_key="ns_avg",
        title="Bytestorm Engine Benchmark: ns/op",
        x_label="ns/op (avg)",
        formatter=format_ns,
        out_file=out_dir / "engine_nsop_bars.png",
    )
    plot_engine_metric_bars(
        rows,
        metric_key="bop_avg",
        title="Bytestorm Engine Benchmark: B/op",
        x_label="B/op (avg)",
        formatter=format_bytes,
        out_file=out_dir / "engine_bop_bars.png",
    )
    plot_engine_metric_bars(
        rows,
        metric_key="alloc_avg",
        title="Bytestorm Engine Benchmark: allocs/op",
        x_label="allocs/op (avg)",
        formatter=format_allocs,
        out_file=out_dir / "engine_allocs_bars.png",
    )
    plot_parallel_modes(parallel_default_summary, parallel_pcore_summary, out_dir / "parallel_modes.png")

    write_engine_markdown(rows, out_dir / "engine_summary.md")
    write_parallel_markdown(parallel_default_summary, parallel_pcore_summary, out_dir / "parallel_summary.md")

    print(f"Wrote charts to {out_dir}")
    print(f"Wrote summary to {out_dir / 'engine_summary.md'} and {out_dir / 'parallel_summary.md'}")


if __name__ == "__main__":
    main()
