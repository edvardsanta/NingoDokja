import signal
import sys
import types
from unittest import mock

import pytest

# main imports the server, which needs the whole meme stack; the stop signal does not.
sys.modules.setdefault("server", types.SimpleNamespace(MemeServiceServer=mock.Mock()))

import main  # noqa: E402


def test_a_stop_signal_ends_the_service_cleanly():
    # Regression: as PID 1 the service ignored SIGTERM, so stopping it hung until the
    # engine killed it (exit 137) and the desktop's restart failed.
    with mock.patch.object(main, "MemeServiceServer"):
        main.main()
    assert signal.getsignal(signal.SIGTERM) is main.stop
    with pytest.raises(SystemExit) as ended:
        main.stop(signal.SIGTERM, None)
    assert ended.value.code == 0
