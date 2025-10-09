import sqlite3
import os
from dataclasses import fields
from models.Meme import Meme
from config import DB_FILE


def create_table_from_model(model_cls, table_name):
    col_defs = []
    for i, f in enumerate(fields(model_cls)):
        typ = f.type
        # Handle Optional types
        try:
            from typing import get_origin, get_args, Union

            if hasattr(typ, "__origin__") and typ.__origin__ is Union:
                args = [a for a in typ.__args__ if a is not type(None)]
                if args:
                    typ = args[0]
        except Exception:
            pass
        if typ in (int, bool):
            col_type = "INTEGER"
        elif typ == float:
            col_type = "REAL"
        else:
            col_type = "TEXT"
        col_def = f"{f.name} {col_type}"
        if i == 0:
            col_def += " PRIMARY KEY"
        col_defs.append(col_def)
    columns_sql = ",\n        ".join(col_defs)
    return f"CREATE TABLE IF NOT EXISTS {table_name} (\n        {columns_sql}\n    )"


def create_memes():
    db_exists = os.path.exists(DB_FILE)
    conn = sqlite3.connect(DB_FILE)
    cursor = conn.cursor()

    if not db_exists:
        print(f"Creating new SQLite database: {DB_FILE}")

    sql = create_table_from_model(Meme, "meme")
    cursor.execute(sql)

    conn.commit()
    conn.close()
    print("Migration complete. Table 'meme' is ready.")


if __name__ == "__main__":
    create_memes()
