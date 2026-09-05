# -*- coding: utf-8 -*-
"""HDA 并发基准：8 位号 x 1 小时(32 请求), 扫描并发度找最佳性能点。"""
import asyncio
import time
from datetime import datetime, timedelta, timezone

from asyncua import Client

URL = "opc.tcp://10.30.144.70:18950/"
NODES = [f"ns=1;s=M000{i}.VALUE" for i in range(8)]
HOURS = 1
SEG_MIN = 15
CONCURRENCIES = [1, 2, 4, 8, 16, 32, 64]


def make_jobs(start, end):
    seg = timedelta(minutes=SEG_MIN)
    jobs = []
    for nid in NODES:
        t = start
        while t < end:
            jobs.append((nid, t, min(t + seg, end)))
            t += seg
    return jobs


async def worker(client, nid, s, e, sem):
    async with sem:
        dvs = await client.get_node(nid).read_raw_history(s, e, 5000, return_bounds=False)
        return len(dvs)


async def run_one(client, jobs, concurrency):
    sem = asyncio.Semaphore(concurrency)
    t0 = time.monotonic()
    res = await asyncio.gather(*[worker(client, n, s, e, sem) for n, s, e in jobs])
    return time.monotonic() - t0, sum(res)


async def main():
    now = datetime.now(timezone.utc)
    start, end = now - timedelta(hours=HOURS), now
    client = Client(URL, timeout=60)
    await client.connect()
    jobs = make_jobs(start, end)
    print(f"请求总数: {len(jobs)}  (8位号 x {HOURS}h, 每段{SEG_MIN}min)")
    print(f"{'并发':>6} {'耗时(s)':>10} {'条数':>8} {'吞吐(条/s)':>12} {'加速比':>8}")
    base = None
    for cc in CONCURRENCIES:
        dt, total = await run_one(client, jobs, cc)
        if base is None:
            base = dt
        print(f"{cc:>6} {dt:>10.3f} {total:>8} {total/dt:>12.0f} {base/dt:>8.2f}")
        await asyncio.sleep(0.5)
    await client.disconnect()


if __name__ == "__main__":
    asyncio.run(main())
