# -*- coding: utf-8 -*-
"""实测 8 位号 x 24h 秒级全量拉取耗时。"""
import asyncio
import time
from datetime import datetime, timedelta, timezone

from asyncua import Client

URL = "opc.tcp://10.30.144.70:18950/"
NODES = [f"ns=1;s=M000{i}.VALUE" for i in range(8)]
SEG = timedelta(minutes=15)
CONCURRENCY = 8


async def main():
    now = datetime.now(timezone.utc)
    start = now - timedelta(hours=24)
    client = Client(URL, timeout=120)
    await client.connect()

    jobs = []
    for nid in NODES:
        t = start
        while t < now:
            jobs.append((nid, t, min(t + SEG, now)))
            t += SEG
    print(f"请求总数: {len(jobs)} (8位号 x 24h, 每段15min), 并发={CONCURRENCY}")

    sem = asyncio.Semaphore(CONCURRENCY)

    async def worker(nid, s, e):
        async with sem:
            dvs = await client.get_node(nid).read_raw_history(s, e, 5000, return_bounds=False)
            real = len([dv for dv in dvs if dv.StatusCode is None or dv.StatusCode.value == 0])
            return len(dvs), real

    t0 = time.monotonic()
    res = await asyncio.gather(*[worker(n, s, e) for n, s, e in jobs])
    dt = time.monotonic() - t0

    total = sum(r[0] for r in res)
    real = sum(r[1] for r in res)
    print(f"总耗时: {dt:.2f}s")
    print(f"总条数: {total} (真实 {real})")
    print(f"吞吐: {real/dt:,.0f} 条/s")
    await client.disconnect()


if __name__ == "__main__":
    asyncio.run(main())
