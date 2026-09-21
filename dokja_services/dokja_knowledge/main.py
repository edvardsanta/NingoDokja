import logging

from server import KnowledgeServiceServer


def main():
    logging.basicConfig(
        level=logging.INFO,
        format="%(asctime)s %(levelname)s %(name)s %(message)s",
    )
    KnowledgeServiceServer().start()


if __name__ == "__main__":
    main()
