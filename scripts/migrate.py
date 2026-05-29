#!/usr/bin/env python3
"""Migrate YellowTDS SQLite data to GhostRoute PostgreSQL and ClickHouse.

Reads campaigns from a YellowTDS database.sqlite file and inserts them into
the GhostRoute PostgreSQL campaigns table. Optionally migrates click data
into the ClickHouse visits table.

Requirements:
    pip install psycopg2-binary==2.9.9 clickhouse-connect==0.7.19 tqdm==4.67.1

Usage:
    python3 migrate.py --sqlite-path /path/to/database.sqlite \
                       --pg-dsn "postgresql://user:pass@localhost/ghostroute"

    # With ClickHouse click migration:
    python3 migrate.py --sqlite-path /path/to/database.sqlite \
                       --pg-dsn "postgresql://user:pass@localhost/ghostroute" \
                       --ch-host localhost --ch-port 8123 --ch-user default
"""

import argparse
import json
import os
import sqlite3
import sys
import time
from datetime import datetime, timezone

try:
    import psycopg2
    import psycopg2.extras
except ImportError:
    print("ERROR: psycopg2 is required. Install with: pip install psycopg2-binary==2.9.9")
    sys.exit(1)

try:
    import clickhouse_connect
except ImportError:
    clickhouse_connect = None

try:
    from tqdm import tqdm
except ImportError:
    # Fallback if tqdm is not installed
    def tqdm(iterable, **kwargs):
        return iterable


def parse_args():
    parser = argparse.ArgumentParser(
        description="Migrate YellowTDS SQLite data to GhostRoute PostgreSQL/ClickHouse"
    )
    parser.add_argument(
        "--sqlite-path",
        required=True,
        help="Path to the YellowTDS database.sqlite file",
    )
    parser.add_argument(
        "--pg-dsn",
        default=os.environ.get("POSTGRES_DSN", ""),
        help="PostgreSQL DSN (default: from POSTGRES_DSN env var)",
    )
    parser.add_argument(
        "--ch-host",
        default=os.environ.get("CLICKHOUSE_HOST", ""),
        help="ClickHouse host (enables click migration if set)",
    )
    parser.add_argument(
        "--ch-port",
        type=int,
        default=int(os.environ.get("CLICKHOUSE_PORT", "8123")),
        help="ClickHouse HTTP port (default: 8123)",
    )
    parser.add_argument(
        "--ch-user",
        default=os.environ.get("CLICKHOUSE_USER", "default"),
        help="ClickHouse username (default: default)",
    )
    parser.add_argument(
        "--ch-password",
        default=os.environ.get("CLICKHOUSE_PASSWORD", ""),
        help="ClickHouse password",
    )
    parser.add_argument(
        "--ch-database",
        default=os.environ.get("CLICKHOUSE_DB", "ghostroute"),
        help="ClickHouse database (default: ghostroute)",
    )
    return parser.parse_args()


def open_sqlite(path):
    """Open SQLite database in read-only mode."""
    if not os.path.exists(path):
        print(f"ERROR: SQLite database not found: {path}")
        sys.exit(1)

    conn = sqlite3.connect(f"file:{path}?mode=ro", uri=True)
    conn.row_factory = sqlite3.Row
    return conn


def safe_json_parse(value, default):
    """Parse a JSON string safely, returning default on failure."""
    if not value:
        return default
    try:
        return json.loads(value)
    except (json.JSONDecodeError, TypeError):
        return default


def migrate_campaigns(sqlite_conn, pg_conn):
    """Migrate campaigns from SQLite to PostgreSQL."""
    cursor = sqlite_conn.cursor()
    cursor.execute("SELECT * FROM campaigns")
    rows = cursor.fetchall()

    if not rows:
        print("No campaigns found in SQLite database.")
        return 0

    print(f"Migrating {len(rows)} campaigns to PostgreSQL...")

    pg_cursor = pg_conn.cursor()
    inserted = 0

    for row in tqdm(rows, desc="Campaigns"):
        name = row["name"] or "Unnamed Campaign"
        route = row["route"] or f"/campaign-{row['id']}"
        active = bool(row["active"]) if row["active"] is not None else True

        # Map SQLite JSON text columns to JSONB
        flows = safe_json_parse(row["flows_json"] if "flows_json" in row.keys() else None, [])
        filters = safe_json_parse(row["filters_json"] if "filters_json" in row.keys() else None, {})
        settings = safe_json_parse(row["settings_json"] if "settings_json" in row.keys() else None, {})

        # Convert Unix timestamps to datetime
        created_at = None
        if row["created_at"]:
            created_at = datetime.fromtimestamp(row["created_at"], tz=timezone.utc)

        updated_at = None
        if row["updated_at"]:
            updated_at = datetime.fromtimestamp(row["updated_at"], tz=timezone.utc)

        try:
            pg_cursor.execute(
                """
                INSERT INTO campaigns (name, route, active, flows, filters, settings, created_at, updated_at)
                VALUES (%s, %s, %s, %s, %s, %s, COALESCE(%s, NOW()), COALESCE(%s, NOW()))
                ON CONFLICT (route) DO UPDATE SET
                    name = EXCLUDED.name,
                    active = EXCLUDED.active,
                    flows = EXCLUDED.flows,
                    filters = EXCLUDED.filters,
                    settings = EXCLUDED.settings,
                    updated_at = COALESCE(EXCLUDED.updated_at, NOW())
                """,
                (
                    name,
                    route,
                    active,
                    json.dumps(flows),
                    json.dumps(filters),
                    json.dumps(settings),
                    created_at,
                    updated_at,
                ),
            )
            inserted += 1
        except Exception as e:
            print(f"  WARNING: Failed to insert campaign '{name}' (route={route}): {e}")
            pg_conn.rollback()
            continue

    pg_conn.commit()
    print(f"  Successfully migrated {inserted}/{len(rows)} campaigns.")
    return inserted


def migrate_clicks_to_clickhouse(sqlite_conn, ch_client, batch_size=10000):
    """Migrate clicks from SQLite to ClickHouse visits table."""
    cursor = sqlite_conn.cursor()
    cursor.execute("SELECT COUNT(*) FROM clicks")
    total = cursor.fetchone()[0]

    if total == 0:
        print("No clicks found in SQLite database.")
        return 0

    print(f"Migrating {total:,} clicks to ClickHouse...")

    cursor.execute("SELECT * FROM clicks ORDER BY id")

    inserted = 0
    batch = []
    column_names = [
        "campaign_id",
        "event_time",
        "visitor_ip",
        "user_agent",
        "country",
        "device_type",
        "os",
        "browser",
        "referer",
        "landing_url",
        "is_bot",
    ]

    device_type_map = {
        "desktop": "desktop",
        "mobile": "mobile",
        "tablet": "tablet",
        "bot": "bot",
    }

    for row in tqdm(cursor, total=total, desc="Clicks"):
        # Convert Unix timestamp to datetime
        event_time = datetime.fromtimestamp(
            row["timestamp"] if row["timestamp"] else 0, tz=timezone.utc
        )

        # Map device type to enum value
        device = (row["device"] or "").lower()
        device_type = device_type_map.get(device, "unknown")

        # Map visitor IP (add IPv6 prefix for IPv4)
        visitor_ip = row["ip"] or "0.0.0.0"

        batch.append([
            row["campaign_id"] or 0,
            event_time,
            visitor_ip,
            row["ua"] or "",
            row["country"] or "",
            device_type,
            row["os"] or "",
            row["browser"] or "",
            row["referer"] or "",
            row["landing_url"] or "",
            1 if row["is_bot"] else 0,
        ])

        if len(batch) >= batch_size:
            ch_client.insert(
                "visits",
                batch,
                column_names=column_names,
                database="ghostroute",
            )
            inserted += len(batch)
            batch = []

    # Flush remaining batch
    if batch:
        ch_client.insert(
            "visits",
            batch,
            column_names=column_names,
            database="ghostroute",
        )
        inserted += len(batch)

    print(f"  Successfully migrated {inserted:,} clicks to ClickHouse.")
    return inserted


def main():
    args = parse_args()

    if not args.pg_dsn:
        print("ERROR: --pg-dsn is required (or set POSTGRES_DSN environment variable)")
        sys.exit(1)

    # Open SQLite
    print(f"Opening SQLite database: {args.sqlite_path}")
    sqlite_conn = open_sqlite(args.sqlite_path)

    # Connect to PostgreSQL
    print(f"Connecting to PostgreSQL...")
    try:
        pg_conn = psycopg2.connect(args.pg_dsn)
        pg_conn.autocommit = False
    except Exception as e:
        print(f"ERROR: Cannot connect to PostgreSQL: {e}")
        sys.exit(1)

    start_time = time.time()

    # Migrate campaigns
    try:
        campaign_count = migrate_campaigns(sqlite_conn, pg_conn)
    except Exception as e:
        print(f"ERROR: Campaign migration failed: {e}")
        pg_conn.rollback()
        sys.exit(1)

    # Optionally migrate clicks to ClickHouse
    click_count = 0
    if args.ch_host:
        if clickhouse_connect is None:
            print("WARNING: clickhouse-connect not installed. Skipping click migration.")
            print("  Install with: pip install clickhouse-connect==0.7.19")
        else:
            print(f"Connecting to ClickHouse at {args.ch_host}:{args.ch_port}...")
            try:
                ch_client = clickhouse_connect.get_client(
                    host=args.ch_host,
                    port=args.ch_port,
                    username=args.ch_user,
                    password=args.ch_password,
                    database=args.ch_database,
                )
                click_count = migrate_clicks_to_clickhouse(sqlite_conn, ch_client)
                ch_client.close()
            except Exception as e:
                print(f"ERROR: ClickHouse click migration failed: {e}")
                sys.exit(1)

    elapsed = time.time() - start_time

    # Summary
    print(f"\nMigration complete!")
    print(f"  Campaigns migrated: {campaign_count}")
    print(f"  Clicks migrated:    {click_count:,}")
    print(f"  Total time:         {elapsed:.2f}s")

    # Cleanup
    sqlite_conn.close()
    pg_conn.close()


if __name__ == "__main__":
    main()
