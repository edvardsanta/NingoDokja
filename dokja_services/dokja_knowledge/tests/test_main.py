import signal
from unittest import mock

import pytest

import main


def test_a_stop_signal_ends_the_service_cleanly():
    # Regression: as PID 1 the service ignored SIGTERM, so stopping it hung until the
    # engine killed it (exit 137) and the desktop's restart failed.
    with mock.patch.object(main, "KnowledgeServiceServer"):
        main.main()
    assert signal.getsignal(signal.SIGTERM) is main.stop
    with pytest.raises(SystemExit) as ended:
        main.stop(signal.SIGTERM, None)
    assert ended.value.code == 0
