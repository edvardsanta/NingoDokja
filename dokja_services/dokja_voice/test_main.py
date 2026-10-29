import sys
from pathlib import Path
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch

import main


class VoiceRunnerTests(unittest.TestCase):
    def test_reload_uses_a_factory_and_preserves_application_logs_in_workers(self):
        fake_server = SimpleNamespace(create_app=Mock())
        with patch.object(sys, "argv", ["main.py", "--reload"]), patch.object(
            main.uvicorn, "run"
        ) as run, patch.dict(sys.modules, {"server": fake_server}):
            main.main()
        fake_server.create_app.assert_not_called()
        self.assertEqual(run.call_args.args, ("server:create_app",))
        options = run.call_args.kwargs
        self.assertTrue(options["factory"])
        self.assertTrue(options["reload"])
        self.assertEqual(
            options["reload_dirs"], [str(Path(main.__file__).resolve().parent)]
        )
        self.assertEqual(options["log_config"]["root"]["level"], "INFO")

    def test_normal_start_does_not_enable_source_reload(self):
        app = object()
        fake_server = SimpleNamespace(create_app=Mock(return_value=app))
        with patch.object(sys, "argv", ["main.py"]), patch.object(
            main.uvicorn, "run"
        ) as run, patch.dict(sys.modules, {"server": fake_server}):
            main.main()
        fake_server.create_app.assert_called_once_with()
        self.assertEqual(run.call_args.args, (app,))
        self.assertNotIn("reload", run.call_args.kwargs)


if __name__ == "__main__":
    unittest.main()
