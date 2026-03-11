import logging

from server import MemeServiceServer


def main():
    logging.basicConfig(
        level=logging.DEBUG,
        format="%(asctime)s %(levelname)s %(name)s %(message)s",
    )
    MemeServiceServer().start()


if __name__ == "__main__":
    main()
