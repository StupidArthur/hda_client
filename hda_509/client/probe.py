#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Probe client (secured): opens a SECURED channel with the configured
application certificate and prints every endpoint the server publishes.

This is the "already knows the server security parameters" flavour. For the
standard third-party discovery flow (unsecured GetEndpoints without knowing
the server certificate in advance) use discovery_probe.py instead.

    python client/probe.py
    python client/probe.py --url opc.tcp://127.0.0.1:48621/ua_mocker/ \
        --server-cert test_material/certs/server_self_signed_cert.pem
"""

import sys
from pathlib import Path

from conn import (
    DEFAULT_APP_CERT,
    DEFAULT_APP_KEY,
    add_common_args,
    fetch_endpoints,
    run_async,
    setup_client,
)
from endpoint_dump import print_endpoints


async def run(args) -> None:
    client = await setup_client(
        args.url,
        app_cert=Path(args.app_cert),
        app_key=Path(args.app_key),
        server_cert=Path(args.server_cert),
        policy_name=args.policy,
        mode_name=args.mode,
        application_uri=args.app_uri,
    )
    endpoints = await fetch_endpoints(client)
    print_endpoints(endpoints)


def main() -> int:
    import argparse

    parser = argparse.ArgumentParser(description="OPC UA endpoint probe (secured)")
    add_common_args(parser, with_identity=False)
    args = parser.parse_args()
    return run_async(run(args))


if __name__ == "__main__":
    sys.exit(main())
