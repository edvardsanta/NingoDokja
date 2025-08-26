import json

from openai import OpenAI


class ChatAgent:
    def __init__(self, api_key: str, base_url: str, model: str = "n/a"):
        self.client = OpenAI(api_key=api_key, base_url=base_url)
        self.model = model

    def send_message(
        self, message: str, temperature: float = 0
    ) -> dict[str, list[str | None]]:
        """
        Send a message to the AI and return the response.
        """
        response = self.client.chat.completions.create(
            model=self.model,
            messages=[{"role": "user", "content": message}],
            temperature=temperature,
            extra_body={"include_retrieval_info": True},
        )

        # Extract message content
        contents = [choice.message.content for choice in response.choices]

        # Extract full retrieval object
        response_dict = response.to_dict()
        retrieval_info = response_dict.get("retrieval", {})

        return {"contents": contents, "retrieval": retrieval_info}

    def print_response(self, message: str, temperature: float = 0):
        """
        Helper method to send message and print response nicely.
        """
        result = self.send_message(message, temperature)
        print("AI Response:")
        for content in result["contents"]:
            print(content)

        print("\nFull retrieval object:")
        print(json.dumps(result["retrieval"], indent=2))


# Example usage
if __name__ == "__main__":
    agent_endpoint = "YOUR_AGENT_ENDPOINT"
    agent_access_key = "YOUR_AGENT_ACCESS_KEY"

    ai = ChatAgent(api_key=agent_access_key, base_url=agent_endpoint, model="n/a")
    ai.print_response("Hello, Ningo AI!")
