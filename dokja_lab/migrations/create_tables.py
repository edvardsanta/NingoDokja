import sqlite3
import os
from config import DB_FILE


def create_memes():
    db_exists = os.path.exists(DB_FILE)
    conn = sqlite3.connect(DB_FILE)
    cursor = conn.cursor()

    if not db_exists:
        print(f"Creating new SQLite database: {DB_FILE}")

    cursor.execute("""
    CREATE TABLE IF NOT EXISTS memes (
        url TEXT PRIMARY KEY,
        title TEXT NOT NULL,
        source TEXT NOT NULL,
        tags TEXT
    )
    """)


    conn.commit()
    conn.close()
    print("Migration complete. Table 'memes' is ready.")

def create_stocks():
    db_exists = os.path.exists(DB_FILE)
    conn = sqlite3.connect(DB_FILE)
    cursor = conn.cursor()

    if not db_exists:
        print(f"Creating new SQLite database: {DB_FILE}")

    cursor.execute("""
    CREATE TABLE IF NOT EXISTS stocks (
        symbol TEXT PRIMARY KEY,
        price REAL NOT NULL,
        change REAL,
        percent REAL,
        last_update TEXT,
        source TEXT
    )
    """)

    conn.commit()
    conn.close()
    print("Migration complete. Table 'stocks' is ready.")

if __name__ == "__main__":
    create_memes()
