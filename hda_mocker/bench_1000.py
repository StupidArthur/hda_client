# -*- coding: utf-8 -*-
"""实测: 1000 位号(M0001~M1000).VALUE 各取 20 分钟, 总耗时。多连接并行。"""
import asyncio
import time
from datetime import datetime, timedelta, timezone

from asyncua import Client

URL = "opc.tcp://10.30.144.70:18950/"
MINS = 20
N_CONN = 8  # 连接数(有效并行度)


def gen_nodes():
    return [f"ns=1;s=M{i:04d}.VALUE" for i in range(1, 1001)]


async def worker_conn(conn_id, nodes, start, end):
    client = Client(URL, timeout=120)
    await client.connect()
    total = 0
    real = 0
    try:
        for nid in nodes:
            try:
                dvs = await client.get_node(nid).read_raw_history(start, end, 5000, return_bounds=False)
                total += len(dvs)
                real += len([dv for dv in dvs if dv.StatusCode is None or dv.StatusCode.value == 0])
            except Exception as e:
                print(f"  [conn{conn_id}] {nid} ERR: {type(e).__name__}")
        return total, real
    finally:
        await client.disconnect()


async def main():
    nodes = gen_nodes()
    end = datetime.now(timezone.utc)
    start = end - timedelta(minutes=MINS)
    print(f"位号数={len(nodes)}, 每节点 {MINS}min(秒级≈{MINS*60}点), 连接数={N_CONN}")

    chunks = [nodes[i::N_CONN] for i in range(N_CONN)]
    t0 = time.monotonic()
    results = await asyncio.gather(*[worker_conn(i, ch, start, end) for i, ch in enumerate(chunks)])
    dt = time.monotonic() - t0
    total = sum(r[0] for r in results)
    real = sum(r[1] for r in results)
    print(f"总耗时: {dt:.2f}s")
    print(f"总条数: {total} (真实 {real})")
    print(f"吞吐: {real/dt:,.0f} 条/s")
    print(f"平均每请求: {dt/len(nodes)*1000:.0f} ms")


if __name__ == "__main__":
    asyncio.run(main())
