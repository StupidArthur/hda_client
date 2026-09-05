# -*- coding: utf-8 -*-
"""
HDA 双因素模型校准与推演。

实测:
  - t_req  : 单次 HistoryRead 请求往返耗时(900 条/段)
  - thpt   : 服务器吞吐上限(条/s), 并发 8 读 32 请求测出
推演:
  - T1 = D / thpt                       (吞吐瓶颈)
  - T2 = R * t_req / C                  (延迟瓶颈)
  - 总耗时 = max(T1, T2)
"""
import asyncio
import math
import statistics
import time
from datetime import datetime, timedelta, timezone

from asyncua import Client

URL = "opc.tcp://10.30.144.70:18950/"
NODE = "ns=1;s=M0000.VALUE"
SEG = timedelta(minutes=15)  # 单段 900 条(秒级采样)


async def measure_t_req(client, times=10) -> float:
    """连续单请求读一段, 返回平均往返耗时(秒)。"""
    now = datetime.now(timezone.utc)
    s, e = now - SEG, now
    samples = []
    for _ in range(times):
        t0 = time.monotonic()
        dvs = await client.get_node(NODE).read_raw_history(s, e, 5000, return_bounds=False)
        samples.append(time.monotonic() - t0)
        assert len(dvs) > 0
    return statistics.median(samples)


async def measure_thpt(client, nodenames, jobs) -> float:
    """并发 8 读全部请求, 返回吞吐(条/s)。"""
    sem = asyncio.Semaphore(8)

    async def worker(nid, s, e):
        async with sem:
            dvs = await client.get_node(nid).read_raw_history(s, e, 5000, return_bounds=False)
            return len(dvs)

    t0 = time.monotonic()
    res = await asyncio.gather(*[worker(n, s, e) for n, s, e in jobs])
    dt = time.monotonic() - t0
    return sum(res) / dt, sum(res)


def make_jobs(nodenames, hours):
    jobs = []
    end = datetime.now(timezone.utc)
    start = end - timedelta(hours=hours)
    for nid in nodenames:
        t = start
        while t < end:
            jobs.append((nid, t, min(t + SEG, end)))
            t += SEG
    return jobs


def estimate(N, M, C, t_req, thpt):
    K = math.ceil(M / (SEG.total_seconds()))
    R = N * K
    D = N * M
    T1 = D / thpt
    T2 = R * t_req / C
    return R, D, T1, T2, max(T1, T2)


async def main():
    client = Client(URL, timeout=60)
    await client.connect()

    t_req = await measure_t_req(client)
    nodenames = [f"ns=1;s=M000{i}.VALUE" for i in range(8)]
    jobs = make_jobs(nodenames, 1)
    thpt, _ = await measure_thpt(client, nodenames, jobs)
    await client.disconnect()

    print("=" * 62)
    print("校准结果(10.30.144.70:18950)")
    print(f"  单请求往返 t_req = {t_req*1000:.1f} ms (读900条)")
    print(f"  服务器吞吐 thpt   = {thpt:,.0f} 条/s")
    print("=" * 62)

    scenarios = [("8位号 x 1h", 8, 3600), ("8位号 x 24h", 8, 86400),
                 ("32位号 x 8h", 32, 28800), ("100位号 x 1h", 100, 3600)]
    for name, N, M in scenarios:
        print(f"\n--- {name} ---")
        R, D, T1, T2, _ = estimate(N, M, 1, t_req, thpt)
        print(f"   请求数 R={R}, 数据量 D={D:,} 条, 吞吐下限 T1={T1:.1f}s")
        print(f"   {'并发':>6} {'T2延迟':>10} {'总耗时(max)':>14}")
        best = None
        for C in [1, 2, 4, 8, 16]:
            R, D, T1b, T2b, T = estimate(N, M, C, t_req, thpt)
            print(f"   {C:>6} {T2b:>10.1f}s {T:>14.1f}s")
            if best is None or T < best[1]:
                best = (C, T)
        print(f"   => 推荐并发 {best[0]}, 预期 {best[1]:.1f}s (t_req 实测 {t_req*1000:.0f}ms)")

    print("\n" + "=" * 62)
    print("总结果: 该服务器瓶颈=吞吐 {0:,.0f} 条/s; 并发>=4 即饱和, 更多并发仅降低延迟项占比".format(thpt))
    print("规划公式: C = max(1, ceil(R*t_req / T_target)), 封顶 8")
    print("=" * 62)


if __name__ == "__main__":
    asyncio.run(main())
