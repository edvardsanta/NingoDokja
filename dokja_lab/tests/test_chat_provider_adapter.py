import unittest

from dokja_lab.ai.chat_agent import ChatAgent


class ChatProviderFixture:
    def send_message(self, message, temperature=0):
        return {"message": message, "temperature": temperature}

    def send_messages(self, messages, temperature=0):
        return {"messages": messages, "temperature": temperature}


class ChatAgentAdapterTest(unittest.TestCase):
    def test_forwards_single_message_to_injected_provider(self):
        agent = ChatAgent(ChatProviderFixture())
        self.assertEqual(agent.send_message("hello", temperature=0.2), {"message": "hello", "temperature": 0.2})

    def test_forwards_history_to_injected_provider(self):
        messages = [{"role": "user", "content": "hello"}]
        agent = ChatAgent(ChatProviderFixture())
        self.assertEqual(agent.send_messages(messages), {"messages": messages, "temperature": 0})
