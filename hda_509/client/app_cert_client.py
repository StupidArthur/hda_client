#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Application Certificate + Anonymous User.

Equivalent to:
    python client/reference_client.py --auth anon
"""

import sys

from reference_client import main

if __name__ == "__main__":
    sys.argv += ["--auth", "anon"]
    sys.exit(main())
