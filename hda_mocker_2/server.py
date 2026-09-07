from __future__ import annotations

import asyncio
from datetime import datetime, timezone

from asyncua import Server, ua

from config import Settings
from history import AggregateHistoryManager, VirtualHistoryStorage
from model import NodeSpec, build_specs, realtime_is_bad, sawtooth


async def run(settings: Settings) -> None:
    specs = build_specs(
        settings.type_groups,
        settings.dynamic_count,
        settings.static_count,
        settings.bad_count,
    )

    server = Server()
    await server.init()
    server.set_endpoint(f"opc.tcp://{settings.host}:{settings.port}/hda-mocker/")
    namespace = await server.register_namespace(settings.namespace_uri)

    storage = VirtualHistoryStorage(specs, settings.history_length, settings.query_duration, settings.page_size)
    server.iserver.history_manager = AggregateHistoryManager(server.iserver, storage)

    root = await server.nodes.objects.add_object(namespace, "HDA_Mocker")
    folders = {
        "type": await root.add_object(namespace, "TypeNodes"),
        "dynamic": await root.add_object(namespace, "DynamicNodes"),
        "static": await root.add_object(namespace, "StaticNodes"),
        "bad_realtime": await root.add_object(namespace, "BadRealtimeNodes"),
    }

    nodes: dict[str, tuple[NodeSpec, object]] = {}
    for spec in specs:
        if spec.node_id.startswith("inter_"):
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
        await server.historize_node_data_change(node)
        nodes[spec.node_id] = (spec, node)

    async def update_realtime() -> None:
        while True:
            now = datetime.now(timezone.utc)
            timestamp = now.timestamp()
            bad = realtime_is_bad(timestamp, settings.good_duration, settings.bad_duration)
            for spec, node in nodes.values():
                if spec.mode not in {"sawtooth", "bad_realtime"}:
                    continue
                status = ua.StatusCodes.BadNoCommunication if spec.mode == "bad_realtime" and bad else ua.StatusCodes.Good
                dv = ua.DataValue(
                    ua.Variant(sawtooth(timestamp), ua.VariantType.Double),
                    StatusCode=ua.StatusCode(status),
                    SourceTimestamp=now,
                )
                await server.write_attribute_value(node.nodeid, dv)
            await asyncio.sleep(settings.interval)

    print(f"HDA Mocker: opc.tcp://{settings.host}:{settings.port}/hda-mocker/")
    print(f"Namespace: ns={namespace} ({settings.namespace_uri})")
    print(f"Nodes: {len(specs)}; history={settings.history_length}s; page={settings.page_size}")
    async with server:
        task = asyncio.create_task(update_realtime())
        try:
            await asyncio.Event().wait()
        finally:
            task.cancel()
            await asyncio.gather(task, return_exceptions=True)
