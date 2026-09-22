#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Application Certificate + UserName User (test/test).

Equivalent to:
    python client/reference_client.py --auth username
"""

import sys

from reference_client import main

if __name__ == "__main__":
    sys.argv += ["--auth", "username"]
    sys.exit(main())
