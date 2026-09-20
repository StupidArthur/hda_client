#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Application Certificate + X.509 User Certificate.

The client uses TWO distinct certificate/key pairs:
  - client_app_a_cert.pem / client_app_a_key.pem  -> application authentication
  - user_cert.pem / user_key.pem                  -> user authentication
                                                   (X509IdentityToken)

Equivalent to:
    python client/reference_client.py --auth x509
"""

import sys

from reference_client import main

if __name__ == "__main__":
    sys.argv += ["--auth", "x509"]
    sys.exit(main())
