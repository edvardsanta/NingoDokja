import json
import sqlite3

import redis
import ast
from abc import ABC, abstractmethod
from typing import Any, List, Dict

from config import DB_FILE


class BaseStorage(ABC):
    @abstractmethod
    def add_meme(self, meme: Dict) -> bool:
        """Add a meme to cache. Return True if it was new, False if duplicate."""
        pass

    @abstractmethod
    def get_all_memes(self) -> List[Dict]:
        """Return all cached memes."""
        pass

    @abstractmethod
    def exists_meme(self, meme_url: str) -> Dict:
        """"Check if a meme with the given URL already exists."""
        pass

class RedisStorage(BaseStorage):
    def __init__(self, host='localhost', port=6379, db=0):
        self.r = redis.Redis(host=host, port=port, db=db)
        self.cache_key = 'memes_pool'

    def add_meme(self, meme: Dict) -> bool:
        url = meme.get("url")
        if not url or self.r.sismember(self.cache_key, url):
            return False
        self.r.sadd(self.cache_key, url)
        self.r.hset('memes_data', url, str(meme))
        return True

    def get_all_memes(self) -> List[Dict]:
        memes_bytes = self.r.hgetall('memes_data').values()
        return [ast.literal_eval(m.decode()) for m in memes_bytes]

    def exists_meme(self, meme_url: str) -> bool:
        return self.r.sismember(self.cache_key, meme_url) is not None


class SQLiteStorage(BaseStorage):
    def __init__(self, db_file=DB_FILE):
        self.conn = sqlite3.connect(db_file, check_same_thread=False)

    def add_meme(self, meme: Dict) -> bool:
        url = meme.get("url")
        if not url:
            return False
        cursor = self.conn.cursor()
        cursor.execute("SELECT 1 FROM memes WHERE url = ?", (url,))
        if cursor.fetchone():
            return False
        cursor.execute(
            "INSERT INTO memes (url, title, source, tags) VALUES (?, ?, ?, ?)",
            (url, meme.get("title", ""), meme.get("source", ""), json.dumps(meme.get("tags", [])))
        )
        self.conn.commit()
        return True

    def get_all_memes(self) -> List[Dict]:
        cursor = self.conn.cursor()
        cursor.execute("SELECT url, title, source, tags FROM memes")
        rows = cursor.fetchall()
        memes = []
        for url, title, source, tags in rows:
            memes.append({
                "url": url,
                "title": title,
                "source": source,
                "tags": json.loads(tags) if tags else []
            })
        return memes

    def exists_meme(self, url: str) -> bool:
        cursor = self.conn.cursor()
        cursor.execute("SELECT 1 FROM memes WHERE url = ?", (url,))
        return cursor.fetchone() is not None

