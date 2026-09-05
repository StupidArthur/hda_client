# -*- coding: utf-8 -*-
"""
HDA (Historical Data Access) 客户端。

连接支持 HDA 的 OPC UA 服务器，读取：
  - 原始历史数据   (--raw)
  - 聚合历史数据   (--processed, 如 Average/Min/Max/TimeAverage)
  - 事件历史       (--events)
并支持列出服务器上开启历史的节点 (--list-history)。

支持 X.509 证书认证连接（同 mocker 的 Basic256Sha256_SignAndEncrypt）。
用法示例：
  python hda_client.py --url opc.tcp://127.0.0.1:48621/ua_mocker/ --list-history
  python hda_client.py --url opc.tcp://127.0.0.1:48621/ua_mocker/ --raw --node ns=1;s=hda_change_1
  python hda_client.py --url opc.tcp://127.0.0.1:48621/ua_mocker/ --processed --node ns=1;s=hda_change_1 --agg Average --interval 10
  python hda_client.py --url opc.tcp://127.0.0.1:48621/ua_mocker/ --events --node ns=1;i=85
"""
import argparse
import asyncio
from datetime import datetime, timedelta, timezone

from asyncua import Client, Node, ua
from asyncua.crypto.security_policies import SecurityPolicyBasic256Sha256

DEFAULT_CERT = "certs/client_cert.pem"
DEFAULT_KEY = "certs/client_key.pem"
DEFAULT_SERVER_CERT = "certs/server_cert.pem"

AGG_IDS = {
    "Average": ua.ObjectIds.AggregateFunction_Average,
    "Minimum": ua.ObjectIds.AggregateFunction_Minimum,
    "Maximum": ua.ObjectIds.AggregateFunction_Maximum,
    "Count": ua.ObjectIds.AggregateFunction_Count,
    "TimeAverage": ua.ObjectIds.AggregateFunction_TimeAverage,
}


def _parse_ts(text: str | None) -> datetime | None:
    if not text:
        return None
    return datetime.fromisoformat(text.replace("Z", "+00:00")).astimezone(timezone.utc)


def _fmt_dv(dv: ua.DataValue) -> str:
    ts = (dv.SourceTimestamp or dv.ServerTimestamp or "").strftime("%H:%M:%S.%f")[:-3]
    val = dv.Value.Value if dv.Value else None
    status = dv.StatusCode.name if dv.StatusCode else ""
    return f"{ts}  {val!r}  [{status}]"


async def connect(url: str, use_security: bool) -> Client:
    client = Client(url)
    if use_security:
        await client.set_security(
            SecurityPolicyBasic256Sha256,
            DEFAULT_CERT,
            DEFAULT_KEY,
            server_certificate=DEFAULT_SERVER_CERT,
            mode=ua.MessageSecurityMode.SignAndEncrypt,
        )
    await client.connect()
    print(f"已连接: {url}")
    return client


async def list_history(client: Client) -> None:
    """遍历 Objects 子树(递归)找 Historizing=True 的变量节点。"""
    print("开启历史(HDA)的节点:")
    found = 0

    async def walk(node: Node) -> None:
        nonlocal found
        try:
            children = await node.get_children()
        except Exception:
            return
        for child in children:
            try:
                if (await child.read_node_class()) == ua.NodeClass.Variable:
                    try:
                        hv = await child.read_attribute(ua.AttributeIds.Historizing)
                        if hv.Value.Value is True:
                            found += 1
                            print(f"  {child.nodeid}  {child}")
                    except Exception:
                        pass
                else:
                    await walk(child)
            except Exception:
                continue

    await walk(client.nodes.objects)
    if found == 0:
        print("  (未找到 historizing 节点)")


async def read_raw(client: Client, node_str: str, start: datetime | None, end: datetime | None, numvalues: int) -> None:
    node = client.get_node(node_str)
    print(f"原始历史 {node_str}  start={start} end={end} numvalues={numvalues}")
    dvs = await node.read_raw_history(start, end, numvalues)
    print(f"共 {len(dvs)} 条:")
    for dv in dvs:
        print(" ", _fmt_dv(dv))


async def read_processed(client: Client, node_str: str, start: datetime | None, end: datetime | None, agg: str, interval_s: float) -> None:
    node: Node = client.get_node(node_str)
    details = ua.ReadProcessedDetails()
    details.StartTime = start or datetime(1970, 1, 1, tzinfo=timezone.utc)
    details.EndTime = end or datetime.now(timezone.utc)
    details.ProcessingInterval = interval_s * 1000.0
    details.AggregateType.append(ua.NodeId(AGG_IDS[agg]))
    details.AggregateConfiguration_ = ua.AggregateConfiguration(
        UseServerCapabilitiesDefaults=True,
        TreatUncertainAsBad=False,
        PercentDataBad=0,
        PercentDataGood=100,
        UseSlopedExtrapolation=False,
    )
    try:
        result = await node.history_read(details)
        result.StatusCode.check()
    except Exception as e:
        if "BadNotImplemented" in str(e):
            print(f"[提示] 服务器不支持聚合历史(ReadProcessedDetails): {e}")
            print("      本 mocker(asyncua 内置存储)仅支持原始历史(--raw);生产 HDA 服务器通常支持聚合。")
        else:
            raise
        return
    data = result.HistoryData.DataValues
    print(f"聚合 {agg}({interval_s}s) {node_str} -> {len(data)} 条:")
    for dv in data:
        print(" ", _fmt_dv(dv))


async def read_events(client: Client, node_str: str, start: datetime | None, end: datetime | None, numvalues: int) -> None:
    node = client.get_node(node_str)
    evs = await node.read_event_history(start, end, numvalues)
    print(f"事件历史 {node_str} -> {len(evs)} 条:")
    for ev in evs:
        print(" ", ev)


async def main() -> None:
    parser = argparse.ArgumentParser(description="HDA 客户端")
    parser.add_argument("--url", required=True)
    parser.add_argument("--no-security", action="store_true", help="使用无安全连接(默认 X.509)")
    parser.add_argument("--node", default=None, help="目标节点, 如 ns=1;s=hda_change_1")
    parser.add_argument("--list-history", action="store_true")
    parser.add_argument("--raw", action="store_true")
    parser.add_argument("--processed", action="store_true")
    parser.add_argument("--events", action="store_true")
    parser.add_argument("--start", default=None)
    parser.add_argument("--end", default=None)
    parser.add_argument("--numvalues", type=int, default=0)
    parser.add_argument("--agg", default="Average", choices=sorted(AGG_IDS))
    parser.add_argument("--interval", type=float, default=10.0, help="聚合处理间隔(秒)")
    args = parser.parse_args()

    start = _parse_ts(args.start)
    end = _parse_ts(args.end)

    client = await connect(args.url, not args.no_security)
    try:
        if args.list_history:
            await list_history(client)
        if args.raw:
            if not args.node:
                parser.error("--raw 需要 --node")
            await read_raw(client, args.node, start, end, args.numvalues)
        if args.processed:
            if not args.node:
                parser.error("--processed 需要 --node")
            await read_processed(client, args.node, start, end, args.agg, args.interval)
        if args.events:
            if not args.node:
                parser.error("--events 需要 --node")
            await read_events(client, args.node, start, end, args.numvalues)
        if not (args.list_history or args.raw or args.processed or args.events):
            parser.error("至少指定一个操作: --list-history / --raw / --processed / --events")
    finally:
        await client.disconnect()


if __name__ == "__main__":
    asyncio.run(main())
