from __future__ import annotations

import asyncio
from contextlib import suppress
from dataclasses import replace
from datetime import datetime, timezone

from asyncua import Server, ua

from config import Settings
from history import AggregateHistoryManager, VirtualHistoryStorage
from model import NodeSpec, build_specs, load_replay_csv, realtime_is_bad, value_at


def normalize_write_timestamp(value: ua.DataValue) -> ua.DataValue:
    """Fill a missing source timestamp with the server receive time in UTC."""
    if value.SourceTimestamp is not None:
        return value
    timestamp = value.ServerTimestamp or datetime.now(timezone.utc)
    return replace(value, SourceTimestamp=timestamp, SourcePicoseconds=None)


def realtime_value(spec: NodeSpec, now: datetime, settings: Settings) -> ua.DataValue:
    status = ua.StatusCodes.Good
    if spec.mode == "bad_realtime" and realtime_is_bad(
        now.timestamp(), settings.good_duration, settings.bad_duration
    ):
        status = ua.StatusCodes.BadNoCommunication
    return ua.DataValue(
        ua.Variant(value_at(spec, now.timestamp()), spec.data_type),
        StatusCode=ua.StatusCode(status),
        SourceTimestamp=now,
    )


async def update_realtime_nodes(nodes: list[tuple[object, NodeSpec]], settings: Settings) -> None:
    """Write changing values so monitored-item subscriptions receive data changes."""
    loop = asyncio.get_running_loop()
    next_update = loop.time()
    while True:
        now = datetime.now(timezone.utc)
        await asyncio.gather(*(node.write_value(realtime_value(spec, now, settings)) for node, spec in nodes))
        next_update += settings.interval
        await asyncio.sleep(max(0.0, next_update - loop.time()))


async def run(settings: Settings) -> None:
    specs = build_specs(
        settings.type_groups,
        settings.dynamic_count,
        settings.static_count,
        settings.bad_count,
    )

    replay_tags = None
    if settings.replay_csv:
        replay_tags = load_replay_csv(settings.replay_csv)
        for tag in replay_tags:
            specs.append(NodeSpec(tag.node_id, ua.VariantType.Double, 0.0, False, "replay"))
        print(f"Replay: {len(replay_tags)} 个位号 from {settings.replay_csv}")

    server = Server()
    await server.init()
    server.set_endpoint(f"opc.tcp://{settings.host}:{settings.port}/hda-mocker/")
    namespace = await server.register_namespace(settings.namespace_uri)

    storage = VirtualHistoryStorage(specs, settings.history_length, settings.query_duration, settings.page_size, settings.interval, settings.read_timeout, replay_tags)
    server.iserver.history_manager = AggregateHistoryManager(server.iserver, storage)

    root = await server.nodes.objects.add_object(namespace, "HDA_Mocker")
    folders = {
        "type": await root.add_object(namespace, "TypeNodes"),
        "dynamic": await root.add_object(namespace, "DynamicNodes"),
        "static": await root.add_object(namespace, "StaticNodes"),
        "bad_realtime": await root.add_object(namespace, "BadRealtimeNodes"),
    }
    if replay_tags:
        folders["replay"] = await root.add_object(namespace, "QANodes")
    replay_nodes = []
    realtime_nodes = []

    for spec in specs:
        if spec.mode == "replay":
            group = "replay"
        elif spec.node_id.startswith("inter_"):
            group = "type"
        elif spec.node_id.startswith("dynamic_"):
            group = "dynamic"
        elif spec.node_id.startswith("static_"):
            group = "static"
        else:
            group = "bad_realtime"
        node = await folders[group].add_variable(
            ua.NodeId(spec.node_id, namespace),
            spec.node_id,
            ua.Variant(spec.initial_value, spec.data_type),
        )
        if spec.writable:
            await node.set_writable()

            def write_value(node_data, attribute, value):
                node_data.attributes[attribute].value = normalize_write_timestamp(value)

            server.set_attribute_value_setter(node.nodeid, write_value)
        await server.historize_node_data_change(node)
        if spec.mode in {"sawtooth", "bad_realtime"}:
            realtime_nodes.append((node, spec))
        if spec.mode == "replay":
            replay_nodes.append(node)

    print(f"HDA Mocker: opc.tcp://{settings.host}:{settings.port}/hda-mocker/")
    print(f"Namespace: ns={namespace} ({settings.namespace_uri})")
    print(f"Nodes: {len(specs)}; history={settings.history_length}s; page={settings.page_size}")
    async with server:
        # Replay 节点实时读 → 数据窗内最近点(末点)
        for node in replay_nodes:
            points = storage.replay.get(str(node.nodeid.Identifier))
            if not points:
                continue
            last_ts, last_value, last_status = points[-1]

            def _replay_current(_nid, _attr, _pts=points, _lts=last_ts, _lv=last_value, _ls=last_status):
                ts = datetime.fromtimestamp(_lts, timezone.utc)
                if _lv is None:
                    return ua.DataValue(ua.Variant(None), StatusCode=ua.StatusCode(_ls), SourceTimestamp=ts)
                return ua.DataValue(ua.Variant(_lv, ua.VariantType.Double), StatusCode=ua.StatusCode(_ls), SourceTimestamp=ts)

            server.set_attribute_value_callback(node.nodeid, _replay_current)

        updater = asyncio.create_task(update_realtime_nodes(realtime_nodes, settings))
        try:
            await asyncio.Event().wait()
        finally:
            updater.cancel()
            with suppress(asyncio.CancelledError):
                await updater
