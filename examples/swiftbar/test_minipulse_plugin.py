#!/usr/bin/env python3
"""Stdlib unittest for the SwiftBar plugin's pure helpers.

Run: python3 -m unittest test_minipulse_plugin (from this folder).
The plugin filename carries dots (SwiftBar's refresh convention),
so it is loaded by path.
"""
import importlib.util
import pathlib
import unittest

_spec = importlib.util.spec_from_file_location(
    "minipulse_plugin", pathlib.Path(__file__).with_name("minipulse.5s.py"))
plugin = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(plugin)


class TestSanitize(unittest.TestCase):
    def test_clean_blocks_swiftbar_param_injection(self):
        # A hostile/buggy server field must not become a SwiftBar
        # parameter (shell= would run on click).
        self.assertNotIn("|", plugin.clean("USD | shell=/tmp/pwn"))
        self.assertNotIn("\n", plugin.clean("a\nb | shell=x"))

    def test_valid_base(self):
        self.assertEqual(plugin.valid_base("http://127.0.0.1:8848/"),
                         "http://127.0.0.1:8848")
        self.assertIsNone(plugin.valid_base("127.0.0.1:8848"))  # no scheme
        self.assertIsNone(plugin.valid_base("http://h/x | shell=y"))
        self.assertIsNone(plugin.valid_base("http://user:pw@h:8848"))
        self.assertIsNone(plugin.valid_base("http://h:8848/?token=s"))

    def test_num_rejects_non_numbers(self):
        # Wrong-typed server payloads must not raise in formatting.
        self.assertIsNone(plugin.num("busy"))
        self.assertIsNone(plugin.num(True))
        self.assertEqual(plugin.num(3), 3.0)

    def test_from_mini_wrong_types_render_without_crash(self):
        import io, contextlib
        v = plugin.from_mini({"cpu_pct": "busy", "sys_w": "x",
                              "power_available": True, "top": {"name": "n|shell=p", "cpu_pct": "q"}})
        out = io.StringIO()
        with contextlib.redirect_stdout(out):
            plugin.render(v)
        text = out.getvalue()
        self.assertIn("—W", text)          # no fake numeric watts
        self.assertNotIn("n|shell", text)  # top name sanitized

    def test_wrong_container_types_do_not_raise(self):
        # {"host": "x"} / {"top": "node"} used to AttributeError.
        v = plugin.from_overview({"host": "x", "sample": "y"})
        self.assertIsNone(v["host"])
        v = plugin.from_mini({"top": "node"})
        self.assertEqual(v["top"], [])
        v = plugin.from_mini("not-a-dict")
        self.assertFalse(v["power_available"])

    def test_fields_customizable_canonical_order(self):
        self.assertEqual(plugin.parse_fields("mem,cpu"), ["cpu", "mem"])
        self.assertEqual(plugin.parse_fields("bogus,, watts "), ["watts"])
        self.assertEqual(plugin.parse_fields(""), ["watts", "cpu", "mem"])
        v = plugin.from_mini({"sys_w": 18.4, "soc_w": 9, "cpu_pct": 23, "mem_pct": 61,
                              "cpu_temp_c": 64, "gpu_temp_c": 58, "fan_rpm": 1200,
                              "power_available": True, "wh_total": 10})
        old = plugin.FIELDS
        try:
            plugin.FIELDS = ["watts", "cpu", "mem"]
            self.assertEqual(plugin.header_text(v), "18W · CPU 23% · MEM 61%")
            plugin.FIELDS = ["cpu", "mem"]
            self.assertEqual(plugin.header_text(v), "CPU 23% · MEM 61%")
        finally:
            plugin.FIELDS = old

    def test_redirects_refused_so_token_cannot_leak(self):
        handler = plugin._NoRedirect()
        self.assertIsNone(handler.redirect_request(None, None, 302, "", {}, "http://evil/"))


if __name__ == "__main__":
    unittest.main()
