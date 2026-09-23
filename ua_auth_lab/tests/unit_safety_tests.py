#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Offline regression tests for fail-closed configuration rules."""

import copy
import contextlib
import io
import sys
import tempfile
import unittest
from argparse import Namespace
from pathlib import Path
from unittest.mock import patch

import yaml

BASE = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(BASE))
sys.path.insert(0, str(BASE / "tools"))

from config_loader import load_config  # noqa: E402
import matrix_ctl  # noqa: E402


VALID = {
    "server": "127.0.0.1",
    "port": 4840,
    "cycle": 1000,
    "namespace_index": 1,
    "security": {"policies": ["NoSecurity"], "client_cert_validation": "none"},
    "user_auth": {"anonymous": True, "username": False, "x509": False},
    "nodes": [{
        "name": "n_", "type": "Int32", "count": 1,
        "change": False, "writable": False, "default": 0,
    }],
}


class ConfigSafetyTests(unittest.TestCase):
    def load(self, cfg: dict):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "config.yaml"
            path.write_text(yaml.safe_dump(cfg), encoding="utf-8")
            return load_config(path)

    def test_valid_minimal_config(self):
        self.assertEqual(self.load(copy.deepcopy(VALID))["port"], 4840)

    def test_trusted_without_store_is_rejected(self):
        cfg = copy.deepcopy(VALID)
        cfg["security"]["client_cert_validation"] = "trusted"
        with self.assertRaisesRegex(ValueError, "trust_store"):
            self.load(cfg)

    def test_no_identity_is_rejected(self):
        cfg = copy.deepcopy(VALID)
        cfg["user_auth"]["anonymous"] = False
        with self.assertRaisesRegex(ValueError, "至少"):
            self.load(cfg)

    def test_invalid_scalar_ranges_are_rejected(self):
        for key, value in (("port", 0), ("port", 65536), ("cycle", 0), ("namespace_index", 0)):
            with self.subTest(key=key, value=value):
                cfg = copy.deepcopy(VALID)
                cfg[key] = value
                with self.assertRaises(ValueError):
                    self.load(cfg)

    def test_unknown_x509_mode_is_rejected(self):
        cfg = copy.deepcopy(VALID)
        cfg["user_auth"] = {
            "anonymous": False, "username": False, "x509": True,
            "x509_validation": "pki", "x509_user_cert": "user.pem",
        }
        with self.assertRaisesRegex(ValueError, "direct"):
            self.load(cfg)

    def test_pid_record_rejects_malformed_and_boolean_pid(self):
        config_path = Path("matrix.yaml")
        fingerprint = "python.exe|123"
        with patch.object(matrix_ctl, "_process_fingerprint", return_value=fingerprint):
            good = {
                "pid": 123,
                "config": str(config_path.resolve()),
                "fingerprint": fingerprint,
            }
            self.assertTrue(matrix_ctl._record_matches(good, config_path))
            for bad in (
                {**good, "pid": True},
                {**good, "pid": -1},
                {**good, "config": None},
                {**good, "fingerprint": None},
            ):
                with self.subTest(record=bad):
                    self.assertFalse(matrix_ctl._record_matches(bad, config_path))

    def test_none_validation_requires_explicit_opt_in(self):
        manifest = {"entries": [{"port": 48800, "validation": "none"}]}
        stderr = io.StringIO()
        with patch.object(matrix_ctl, "load_manifest", return_value=manifest):
            with contextlib.redirect_stderr(stderr):
                result = matrix_ctl.cmd_start(Namespace(only=None, allow_insecure=False))
        self.assertEqual(result, 1)
        self.assertIn("--allow-insecure", stderr.getvalue())


if __name__ == "__main__":
    unittest.main()
