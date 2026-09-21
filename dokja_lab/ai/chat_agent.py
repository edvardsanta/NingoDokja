try:
    from bootstrap import ensure_repo_root
except ImportError:  # pragma: no cover
    from dokja_lab.bootstrap import ensure_repo_root  # type: ignore[no-redef]

ensure_repo_root()

from dokja_services.dokja_chat_ai import OpenAIChatService


class ChatAgent(OpenAIChatService):
    pass
