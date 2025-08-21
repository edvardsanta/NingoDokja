class BaseTransport:
    def receive(self):
        raise NotImplementedError

    def send(self, msg):
        raise NotImplementedError

    def close(self):
        raise NotImplementedError
