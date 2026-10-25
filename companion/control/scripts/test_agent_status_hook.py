#!/usr/bin/env python3
import importlib.util
import unittest
from pathlib import Path


MODULE_PATH = Path(__file__).with_name("agent-status-hook.py")
SPEC = importlib.util.spec_from_file_location("agent_status_hook", MODULE_PATH)
MODULE = importlib.util.module_from_spec(SPEC)
assert SPEC and SPEC.loader
SPEC.loader.exec_module(MODULE)


class AgentStatusHookTests(unittest.TestCase):
    def test_post_tool_use_success_stays_working(self):
        self.assertEqual(
            MODULE.state_for("PostToolUse", {"tool_response": {"output": "error handling docs", "exit_code": 0}}),
            "working",
        )

    def test_post_tool_use_structured_failure_is_error(self):
        self.assertEqual(
            MODULE.state_for("PostToolUse", {"tool_response": {"ok": False, "message": "failed to connect"}}),
            "error",
        )
        self.assertEqual(
            MODULE.state_for("PostToolUse", {"tool_response": {"exit_code": 1}}),
            "error",
        )

    def test_post_tool_use_plain_text_error_is_not_inferred(self):
        self.assertEqual(
            MODULE.state_for("PostToolUse", {"tool_response": "The command printed an error message in its example output."}),
            "working",
        )

    def test_common_nested_error_fields_are_failures(self):
        self.assertEqual(
            MODULE.state_for("PostToolUse", {"tool_result": {"error": "permission denied"}}),
            "error",
        )

    def test_false_error_field_is_not_a_failure(self):
        self.assertEqual(
            MODULE.state_for("PostToolUse", {"tool_result": {"error": False, "exit_code": 0}}),
            "working",
        )
        self.assertEqual(
            MODULE.state_for("AfterToolUse", {"result": {"is_error": True}}),
            "error",
        )


if __name__ == "__main__":
    unittest.main()
