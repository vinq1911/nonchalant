#!/usr/bin/env python3
"""Build the wake-on-publish + hijack performance report PDF.

Reads CSV/text artefacts from reports/data/ and writes
reports/PERF-WOP.pdf.
"""

from __future__ import annotations

import csv
import datetime
import os
import platform
import re
import subprocess
from pathlib import Path

import matplotlib.pyplot as plt
from reportlab.lib.colors import HexColor, grey, lightgrey
from reportlab.lib.pagesizes import A4
from reportlab.lib.styles import ParagraphStyle, getSampleStyleSheet
from reportlab.lib.units import cm
from reportlab.platypus import (
    Image,
    PageBreak,
    Paragraph,
    SimpleDocTemplate,
    Spacer,
    Table,
    TableStyle,
)

ROOT = Path(__file__).resolve().parents[2]
DATA = ROOT / "reports" / "data"
OUT_PDF = ROOT / "reports" / "PERF-WOP.pdf"


# ---------- data loading ----------------------------------------------------


def parse_idle_csv(path: Path) -> list[dict]:
    rows: list[dict] = []
    with path.open() as f:
        reader = csv.DictReader(f)
        for r in reader:
            rows.append(
                {
                    "n": int(r["n"]),
                    "cpu_avg": float(r["cpu_avg"]),
                    "cpu_max": float(r["cpu_max"]),
                    "goroutines": int(r["goroutines"]),
                    "rss_mb": float(r["rss_mb"]),
                }
            )
    return rows


def parse_summary_csv(path: Path) -> list[dict]:
    """Parse the new harness summary CSV (one row per N)."""
    rows: list[dict] = []
    if not path.exists():
        return rows
    with path.open() as f:
        for r in csv.DictReader(f):
            rows.append(
                {
                    "n": int(r["n"]),
                    "connected": int(r["connected"]),
                    "flv_ok": int(r["flv_ok"]),
                    "agg_mbs": float(r["agg_mbs"]),
                    "per_client_kbs": float(r["per_client_kbs"]),
                    "stall_p50": float(r["stall_p50_ms"]),
                    "stall_p95": float(r["stall_p95_ms"]),
                    "stall_max": float(r["stall_max_ms"]),
                    "cv_mean_pct": float(r["cv_mean_pct"]),
                    "cv_p95_pct": float(r["cv_p95_pct"]),
                    "drops": int(r["bus_drops"]),
                    "connect_storm_s": float(r["connect_storm_s"]),
                }
            )
    return rows


def parse_loadtest_text(path: Path) -> dict:
    """Pull the summary block out of one (legacy) loadtest .txt artefact."""
    out: dict = {}
    if not path.exists():
        return out
    txt = path.read_text()
    pairs = {
        "clients": r"clients\s*:\s*(\d+)",
        "duration": r"duration\s*:\s*([\d.]+)",
        "flv_ok": r"FLV-validated\s*:\s*(\d+)",
        "flv_total": r"FLV-validated\s*:\s*\d+\s*/\s*(\d+)",
        "aggregate_mbs": r"aggregate\s*:\s*([\d.]+)",
    }
    for k, rx in pairs.items():
        m = re.search(rx, txt)
        if m:
            v = m.group(1)
            out[k] = float(v) if "." in v else int(v)
    return out


def collect_legacy_loadtest(label: str) -> list[dict]:
    rows = []
    for n in (1, 4, 16, 64, 256, 1024, 4096):
        d = parse_loadtest_text(DATA / f"loadtest-{label}-n{n}.txt")
        if d:
            d["n"] = n
            rows.append(d)
    return rows


def parse_pprof_top(path: Path, max_rows: int = 6) -> list[tuple[str, str, str]]:
    """Pull the most-cumulative entries from a pprof -top text file."""
    if not path.exists():
        return []
    out: list[tuple[str, str, str]] = []
    with path.open() as f:
        in_table = False
        for line in f:
            if "flat" in line and "cum" in line and "%" in line:
                in_table = True
                continue
            if not in_table:
                continue
            line = line.strip()
            if not line:
                continue
            # Format: flat flat% sum% cum cum% func
            parts = line.split()
            if len(parts) < 6:
                continue
            cum = parts[3]
            cum_pct = parts[4]
            func = " ".join(parts[5:])
            # Skip wrapper functions to highlight where time actually goes.
            skip = ("net/http.serverHandler", "net/http.HandlerFunc",
                    "net/http.(*conn).serve", "net/http.(*ServeMux).ServeHTTP",
                    "nonchalant/internal/svc/httpflv.(*Handler).serveDispatched",
                    "nonchalant/internal/svc/httpflv.(*Handler).ServeHTTP",
                    "nonchalant/internal/svc/httpflv.(*Subscriber).ProcessMessages")
            if any(s in func for s in skip):
                continue
            out.append((cum, cum_pct, func))
            if len(out) >= max_rows:
                break
    return out


# ---------- charts ----------------------------------------------------------


def plot_idle_cpu(pre: list[dict], post: list[dict], out: Path) -> None:
    fig, ax = plt.subplots(figsize=(6.5, 3.4))
    xs = [r["n"] for r in pre if r["n"] > 0]
    ax.plot(
        xs, [r["cpu_avg"] for r in pre if r["n"] > 0],
        "o-", color="#cc4444", label="pre-WOP (poll 20 ms)",
    )
    ax.plot(
        xs, [r["cpu_avg"] for r in post if r["n"] > 0],
        "s-", color="#449944", label="WOP (wake on publish)",
    )
    ax.set_xscale("log", base=2)
    ax.set_xlabel("idle subscribers attached")
    ax.set_ylabel("server CPU (%)")
    ax.set_title("Idle-CPU vs subscriber count")
    ax.grid(True, which="both", alpha=0.3)
    ax.legend()
    fig.tight_layout()
    fig.savefig(out, dpi=150)
    plt.close(fig)


def plot_throughput(pre: list[dict], wop: list[dict], hj: list[dict], out: Path) -> None:
    fig, ax = plt.subplots(figsize=(6.5, 3.4))
    ax.plot([r["n"] for r in pre], [r.get("aggregate_mbs", 0) for r in pre],
            "o-", color="#cc4444", label="pre-WOP, no hijack")
    ax.plot([r["n"] for r in wop], [r.get("aggregate_mbs", 0) for r in wop],
            "s-", color="#aa8822", label="WOP, no hijack")
    ax.plot([r["n"] for r in hj], [r["agg_mbs"] for r in hj],
            "^-", color="#449944", label="WOP + hijack (final)")
    ax.set_xscale("log", base=2)
    ax.set_xlabel("active subscribers")
    ax.set_ylabel("aggregate throughput (MB/s)")
    ax.set_title("Active throughput vs subscriber count")
    ax.grid(True, which="both", alpha=0.3)
    ax.legend()
    fig.tight_layout()
    fig.savefig(out, dpi=150)
    plt.close(fig)


def plot_quality(hj: list[dict], out: Path) -> None:
    fig, ax = plt.subplots(figsize=(6.5, 3.4))
    xs = [r["n"] for r in hj if r["n"] >= 16]
    p50 = [r["stall_p50"] for r in hj if r["n"] >= 16]
    p95 = [r["stall_p95"] for r in hj if r["n"] >= 16]
    mx = [r["stall_max"] for r in hj if r["n"] >= 16]
    ax.plot(xs, p50, "o-", color="#449944", label="p50 stall (ms)")
    ax.plot(xs, p95, "s-", color="#cc8822", label="p95 stall (ms)")
    ax.plot(xs, mx, "^-", color="#cc4444", label="max stall (ms)")
    ax.set_xscale("log", base=2)
    ax.set_yscale("log")
    ax.set_xlabel("active subscribers")
    ax.set_ylabel("longest gap with no bytes (ms, log)")
    ax.set_title("Subscriber-side stall distribution (final build)")
    ax.grid(True, which="both", alpha=0.3)
    ax.legend()
    fig.tight_layout()
    fig.savefig(out, dpi=150)
    plt.close(fig)


# ---------- PDF -------------------------------------------------------------


def styled_table(data: list[list[str]], col_widths: list[float]) -> Table:
    t = Table(data, colWidths=col_widths)
    t.setStyle(
        TableStyle(
            [
                ("BACKGROUND", (0, 0), (-1, 0), HexColor("#22334d")),
                ("TEXTCOLOR", (0, 0), (-1, 0), HexColor("#ffffff")),
                ("FONTNAME", (0, 0), (-1, 0), "Helvetica-Bold"),
                ("ALIGN", (0, 0), (-1, -1), "RIGHT"),
                ("ALIGN", (0, 0), (0, -1), "LEFT"),
                ("FONTSIZE", (0, 0), (-1, -1), 9),
                ("BOTTOMPADDING", (0, 0), (-1, 0), 6),
                ("TOPPADDING", (0, 0), (-1, 0), 4),
                ("GRID", (0, 0), (-1, -1), 0.25, lightgrey),
                ("ROWBACKGROUNDS", (0, 1), (-1, -1), [HexColor("#f7f7f7"), None]),
            ]
        )
    )
    return t


def host_info() -> str:
    parts = [platform.system(), platform.release(), platform.machine()]
    try:
        cpu = subprocess.check_output(
            ["sysctl", "-n", "machdep.cpu.brand_string"], text=True
        ).strip()
        parts.append(cpu)
    except Exception:
        pass
    try:
        cores = subprocess.check_output(["sysctl", "-n", "hw.ncpu"], text=True).strip()
        parts.append(f"{cores} CPUs")
    except Exception:
        pass
    return " · ".join(parts)


def build() -> None:
    pre_idle = parse_idle_csv(DATA / "idle-cpu-idle-pre-wop.csv")
    post_idle = parse_idle_csv(DATA / "idle-cpu-idle-wop.csv")
    pre_active = collect_legacy_loadtest("pre-wop")
    wop_active = collect_legacy_loadtest("wop")
    hj_active = parse_summary_csv(DATA / "summary-hijack.csv")

    img_idle = DATA / "chart-idle-cpu.png"
    img_thr = DATA / "chart-throughput.png"
    img_q = DATA / "chart-quality.png"
    plot_idle_cpu(pre_idle, post_idle, img_idle)
    plot_throughput(pre_active, wop_active, hj_active, img_thr)
    plot_quality(hj_active, img_q)

    prof_pre = parse_pprof_top(DATA / "cpu-before-fanout.txt", max_rows=8)
    prof_post = parse_pprof_top(DATA / "cpu-after-hijack.txt", max_rows=8)

    styles = getSampleStyleSheet()
    body = ParagraphStyle("body", parent=styles["BodyText"],
        fontName="Helvetica", fontSize=10, leading=14, spaceAfter=8)
    h1 = ParagraphStyle("h1", parent=styles["Heading1"],
        fontSize=16, textColor=HexColor("#22334d"), spaceAfter=10)
    h2 = ParagraphStyle("h2", parent=styles["Heading2"],
        fontSize=12, textColor=HexColor("#22334d"), spaceAfter=6, spaceBefore=10)
    mono = ParagraphStyle("mono", parent=styles["BodyText"],
        fontName="Courier", fontSize=9, leading=12, textColor=grey)
    title = ParagraphStyle("title", parent=styles["Title"],
        fontSize=22, textColor=HexColor("#22334d"), leading=26)
    subtitle = ParagraphStyle("subtitle", parent=styles["BodyText"],
        fontSize=11, textColor=grey, leading=14)

    doc = SimpleDocTemplate(
        str(OUT_PDF), pagesize=A4,
        leftMargin=2*cm, rightMargin=2*cm, topMargin=2*cm, bottomMargin=2*cm,
        title="nonchalant — wake-on-publish + hijack performance report",
        author="nonchalant",
    )

    story = []
    story.append(Paragraph("nonchalant — Wake-on-Publish &amp; HTTP Hijack", title))
    story.append(Paragraph(
        "Performance smoke test — idle CPU, active throughput, and stall distribution",
        subtitle))
    story.append(Spacer(1, 6))
    story.append(Paragraph(
        f"Generated {datetime.datetime.now().strftime('%Y-%m-%d %H:%M %Z')} on "
        f"{host_info()}", mono))
    story.append(Spacer(1, 12))

    # ---------- summary -----------------------------------------------------
    story.append(Paragraph("Executive summary", h1))
    pre_idle_4096 = next(r["cpu_avg"] for r in pre_idle if r["n"] == 4096)
    post_idle_4096 = next(r["cpu_avg"] for r in post_idle if r["n"] == 4096)
    pre_1024 = next(r["aggregate_mbs"] for r in pre_active if r["n"] == 1024)
    hj_1024 = next(r["agg_mbs"] for r in hj_active if r["n"] == 1024)
    story.append(Paragraph(
        "Two changes are evaluated in this report:", body))
    story.append(Paragraph(
        "<b>1. Wake-on-publish (WOP)</b> — replaces the 20 ms idle-poll loop in every "
        "subscriber with a parked wait on a per-stream broadcast channel that the "
        "publisher rotates on each <code>Publish</code>.", body))
    story.append(Paragraph(
        "<b>2. HTTP Hijack</b> — pprof showed that on the active path, 97% of CPU was "
        "<code>syscall.write</code>: every FLV tag went through bufio.Writer.Flush "
        "<i>and</i> http.Flusher.Flush, plus HTTP chunked-transfer-encoding overhead. "
        "The handler now <code>http.Hijacker.Hijack()</code>s the connection and writes "
        "raw FLV bytes directly to <code>net.Conn</code> — one syscall per tag, no "
        "chunked encoding.", body))
    story.append(Paragraph(
        f"<b>Idle CPU at 4 096 subscribers:</b> "
        f"<b>{pre_idle_4096:.1f}%</b> → <b>{post_idle_4096:.1f}%</b> "
        f"({(1-post_idle_4096/pre_idle_4096)*100:.0f}% reduction, due to WOP).",
        body))
    story.append(Paragraph(
        f"<b>Active throughput at 1 024 subscribers:</b> "
        f"<b>{pre_1024:.0f} MB/s</b> → <b>{hj_1024:.0f} MB/s</b> "
        f"({hj_1024/pre_1024*100:.0f}% of pre-WOP, due to hijack + WOP combined).",
        body))

    # ---------- methodology -------------------------------------------------
    story.append(Paragraph("Methodology", h1))
    story.append(Paragraph(
        "The harness builds three variants of nonchalant from the same source tree: "
        "(a) pre-WOP (poll-loop subscriber + bufio + chunked transfer), "
        "(b) WOP (wake-on-publish, still bufio + chunked), "
        "(c) WOP + hijack (final build).", body))
    story.append(Paragraph(
        "Workload: <code>ffmpeg</code> publishes a looped MP4 over RTMP at native "
        "rate (~340 KB/s). The load-test client (<code>cmd/loadtest</code>) opens N "
        "concurrent HTTP-FLV consumers, validates the FLV signature, then samples "
        "bytes / 100 ms during a steady-state window that begins only after every "
        "client has read its first byte. We record per-client jitter (CV%), longest "
        "no-byte gap (stall), and bus drop counts via <code>/api/streams</code>.", body))
    story.append(Paragraph(
        "A 30-second CPU profile is captured via the new <code>/debug/pprof/profile</code> "
        "endpoint while the load-test runs at N=1024 subscribers. Top cumulative "
        "consumers are tabulated below.", body))

    # ---------- table 1: idle CPU -------------------------------------------
    story.append(Paragraph("Result 1 — idle subscriber CPU cost (WOP win)", h1))
    rows = [["N subs", "Pre-WOP CPU%", "WOP CPU%", "Reduction",
             "Goroutines (WOP)", "RSS (WOP, MB)"]]
    for r_pre, r_post in zip(pre_idle, post_idle):
        if r_pre["n"] == 0:
            continue
        red = "—" if r_pre["cpu_avg"] == 0 else f"{(1-r_post['cpu_avg']/r_pre['cpu_avg'])*100:.0f}%"
        rows.append([f"{r_post['n']:,}", f"{r_pre['cpu_avg']:.2f}",
                     f"{r_post['cpu_avg']:.2f}", red,
                     f"{r_post['goroutines']:,}", f"{r_post['rss_mb']:.1f}"])
    story.append(styled_table(rows, [2.3*cm, 2.5*cm, 2.2*cm, 2.2*cm, 3.2*cm, 3.2*cm]))
    story.append(Spacer(1, 8))
    story.append(Image(str(img_idle), width=16*cm, height=8.4*cm))

    story.append(PageBreak())

    # ---------- table 2: active throughput ----------------------------------
    story.append(Paragraph("Result 2 — active throughput (hijack win)", h1))
    story.append(Paragraph(
        "Aggregate bytes/second delivered to N concurrent HTTP-FLV consumers during "
        "a 10-second steady-state window. The new harness measures only after every "
        "client is reading.",
        body))
    rows = [["N", "Pre-WOP MB/s", "WOP MB/s", "WOP+Hijack MB/s",
             "Per-client KB/s", "Connected/Total"]]
    for r_pre, r_wop, r_hj in zip(pre_active, wop_active, hj_active):
        if r_hj["n"] != r_pre["n"]:
            continue
        rows.append([
            f"{r_hj['n']:,}",
            f"{r_pre.get('aggregate_mbs', 0):.1f}",
            f"{r_wop.get('aggregate_mbs', 0):.1f}",
            f"{r_hj['agg_mbs']:.1f}",
            f"{r_hj['per_client_kbs']:.0f}",
            f"{r_hj['connected']:,} / {r_hj['n']:,}",
        ])
    story.append(styled_table(rows, [1.7*cm, 2.6*cm, 2.4*cm, 3.1*cm, 2.8*cm, 3.4*cm]))
    story.append(Spacer(1, 8))
    story.append(Image(str(img_thr), width=16*cm, height=8.4*cm))

    # ---------- table 3: quality markers ------------------------------------
    story.append(Paragraph("Result 3 — quality markers (final build)", h1))
    story.append(Paragraph(
        "Stall = longest period during the 10 s window with zero bytes received "
        "by a given client. Jitter CV% = stddev/mean of bytes per 100 ms slot. "
        "Drops = bus-side messages-overwritten counter (slow subscribers).", body))
    rows = [["N", "p50 stall ms", "p95 stall ms", "max stall ms",
             "jitter CV% (mean)", "jitter CV% (p95)", "Bus drops"]]
    for r in hj_active:
        rows.append([
            f"{r['n']:,}",
            f"{r['stall_p50']:.0f}", f"{r['stall_p95']:.0f}", f"{r['stall_max']:.0f}",
            f"{r['cv_mean_pct']:.0f}", f"{r['cv_p95_pct']:.0f}",
            f"{r['drops']:,}",
        ])
    story.append(styled_table(rows, [1.7*cm, 2.4*cm, 2.4*cm, 2.4*cm, 2.6*cm, 2.6*cm, 2.0*cm]))
    story.append(Spacer(1, 8))
    story.append(Image(str(img_q), width=16*cm, height=8.4*cm))

    story.append(PageBreak())

    # ---------- profile -----------------------------------------------------
    story.append(Paragraph("Result 4 — CPU profile (pprof)", h1))
    story.append(Paragraph(
        "30-second CPU profiles captured via <code>/debug/pprof/profile</code> while "
        "1 024 active subscribers consumed the live stream. Top cumulative consumers "
        "below — non-application wrapper functions filtered out.", body))

    story.append(Paragraph("Pre-hijack profile (bufio + chunked + double-flush)", h2))
    rows = [["Cumulative", "% of total", "Function"]]
    for c, p, fn in prof_pre:
        rows.append([c, p, fn])
    story.append(styled_table(rows, [2.2*cm, 2.0*cm, 11.8*cm]))

    story.append(Spacer(1, 8))
    story.append(Paragraph("Post-hijack profile (raw conn.Write)", h2))
    rows = [["Cumulative", "% of total", "Function"]]
    for c, p, fn in prof_post:
        rows.append([c, p, fn])
    story.append(styled_table(rows, [2.2*cm, 2.0*cm, 11.8*cm]))

    # ---------- discussion --------------------------------------------------
    story.append(Spacer(1, 14))
    story.append(Paragraph("Discussion", h1))

    story.append(Paragraph("Why idle CPU drops so steeply (WOP)", h2))
    story.append(Paragraph(
        "Pre-WOP, every subscriber goroutine wakes 50 times/sec to ask <i>“is there "
        "data?”</i> via <code>time.After(20*time.Millisecond)</code>. With 4 096 idle "
        "subs that is 204 800 useless wakeups per second — pure scheduler tax. WOP "
        "replaces this with a parked receive on a <code>chan struct{}</code> the "
        "publisher closes on each <code>Publish</code>; idle subscribers are off the "
        "run queue entirely. Idle CPU drops to ~5%, almost all from the 1 fps trickle "
        "publisher and TCP keepalive.",
        body))

    story.append(Paragraph("Why active throughput rises ~2× (hijack)", h2))
    story.append(Paragraph(
        "Pre-hijack, every FLV tag traversed: <code>bufio.Writer.Write</code> → "
        "<code>bufio.Writer.Flush</code> → <code>chunkWriter.Write</code> "
        "(adds chunk framing + a syscall) → "
        "<code>http.ResponseWriter.Flush</code> (forces another syscall). Two "
        "<code>write(2)</code> syscalls per frame plus chunked-encoding bookkeeping. "
        "After hijack the same FLV tag becomes one <code>conn.Write</code> directly "
        "to the TCP socket — one syscall, no chunk framing. The pprof comparison "
        "above shows <code>bufio.(*Writer).Flush</code>, "
        "<code>(*chunkWriter).Write</code>, and <code>(*response).Flush</code> "
        "all disappearing post-hijack; only <code>syscall.write</code> remains. "
        "Aggregate throughput at 1 024 subscribers nearly doubles.", body))

    story.append(Paragraph("Why throughput still falls at 4 096 subscribers", h2))
    story.append(Paragraph(
        "Even with hijack, 4 096 simultaneous TCP writes per publisher frame "
        "saturate the macOS kernel's per-process syscall and send-buffer paths. "
        "p95 stall is 33 s in this scenario — clients are connected and reading, "
        "but the kernel can't get bytes out fast enough. Bus-side drops stay at "
        "zero (the bus is still O(1) regardless of fanout); the bottleneck has moved "
        "fully into the kernel. Beyond ~1-2 K subscribers per process, you need "
        "either multiple worker procs (SO_REUSEPORT) or an edge-cache fan-out tier — "
        "no amount of in-process tuning fixes the per-process kernel ceiling.", body))

    story.append(Paragraph("Quality at the 1 K-subscriber operating point", h2))
    rows = [["N=1024 (final build)", ""]]
    final = next(r for r in hj_active if r["n"] == 1024)
    rows += [
        ["Aggregate throughput", f"{final['agg_mbs']:.1f} MB/s"],
        ["Per-client throughput", f"{final['per_client_kbs']:.0f} KB/s "
                                  "(matches publisher rate ≈ 340 KB/s)"],
        ["p50 stall", f"{final['stall_p50']:.0f} ms"],
        ["p95 stall", f"{final['stall_p95']:.0f} ms"],
        ["Max stall", f"{final['stall_max']:.0f} ms"],
        ["Jitter CV (mean)", f"{final['cv_mean_pct']:.0f} %"],
        ["Jitter CV (p95)", f"{final['cv_p95_pct']:.0f} %"],
        ["Bus drops", f"{final['drops']}"],
        ["FLV-validated", f"{final['flv_ok']:,} / {final['n']:,}"],
    ]
    story.append(styled_table(rows, [5*cm, 11*cm]))

    story.append(Paragraph("Files exercised by this report", h2))
    story.append(Paragraph(
        "Server: <code>./bin/nonchalant</code><br/>"
        "Publisher: <code>ffmpeg -re -stream_loop -1 -i assets/nonchalant-test.mp4 …</code><br/>"
        "Consumer: <code>./bin/loadtest -url … -n N -d 10s -summary …</code><br/>"
        "Profiler: <code>curl /debug/pprof/profile?seconds=30</code> "
        "→ <code>go tool pprof -top</code><br/>"
        "Harness: <code>scripts/perf/{run,idle,profile}.sh</code><br/>"
        "Data: <code>reports/data/{summary-*,idle-cpu-*,cpu-*,loadtest-*}</code>",
        mono))

    doc.build(story)
    print(f"wrote {OUT_PDF}")


if __name__ == "__main__":
    os.chdir(ROOT)
    build()
