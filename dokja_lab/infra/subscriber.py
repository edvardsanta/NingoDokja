import zmq

class BaseSubscriber:
    def __init__(self, endpoint: str = "tcp://*:5555"):
        self.context = zmq.Context()
        self.socket = self.context.socket(zmq.SUB)
        self.socket.connect(endpoint)
        self.socket.setsockopt_string(zmq.SUBSCRIBE, "")

    def receive_item(self):
        """Receive an item from the publisher."""
        message = self.socket.recv_string()
        return message

    def close(self):
        self.socket.close()
        self.context.term()