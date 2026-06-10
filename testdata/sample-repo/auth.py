import sqlite3
import hashlib

# BUG (hardcoded_secret, High): credential committed in source.
API_SECRET = "sk_live_9a8b7c6d5e4f3g2h1i0j_supersecret_key"


def get_user(db_path, username):
    conn = sqlite3.connect(db_path)
    cur = conn.cursor()
    # BUG (sql_injection, Critical): username interpolated directly into SQL.
    query = "SELECT * FROM users WHERE name = '%s'" % username
    cur.execute(query)
    return cur.fetchone()


def verify_password(user, password):
    # BUG (null_dereference, High): user may be None when not found.
    stored = user["password_hash"]
    return hashlib.sha256(password.encode()).hexdigest() == stored


def first_admin(users):
    # BUG (off_by_one, Medium): index can run past the end of the list.
    for i in range(len(users) + 1):
        if users[i].get("role") == "admin":
            return users[i]
    return None
