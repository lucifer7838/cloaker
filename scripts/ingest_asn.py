#!/usr/bin/env python3
"""Bulk import MaxMind GeoLite2-ASN-Blocks-IPv4.csv into Redis sorted sets.

Follows the SKILL_redis_cidr_ipmatch.md section 4 pattern exactly:
- Redis sorted set key: "asn:ipv4:ranges"
- Score = end IP integer (last IP in the CIDR block)
- Member = "start_int|as_number|as_name"

This enables O(log n) IP-to-ASN lookups using ZRANGEBYSCORE.

Requirements:
    pip install redis==5.2.1 tqdm==4.67.1

Usage:
    python3 ingest_asn.py --file GeoLite2-ASN-Blocks-IPv4.csv \
                          --host 127.0.0.1 --port 6379 --db 0

    # With authentication and flush:
    python3 ingest_asn.py --file GeoLite2-ASN-Blocks-IPv4.csv \
                          --host redis.local --password secret --flush-key
"""

import argparse
import csv
import ipaddress
import sys
import time

import redis
from tqdm import tqdm


REDIS_KEY = "asn:ipv4:ranges"


def ipv4_to_int(ip_str: str) -> int:
    """Convert IPv4 string to unsigned 32-bit integer."""
    return int(ipaddress.IPv4Address(ip_str))


def cidr_to_range(cidr: str) -> tuple:
    """Return (start_int, end_int) for a CIDR block."""
    network = ipaddress.IPv4Network(cidr, strict=False)
    start = int(network.network_address)
    end = int(network.broadcast_address)
    return start, end


def count_lines(filepath: str) -> int:
    """Count lines in CSV for progress bar."""
    with open(filepath, "r") as f:
        return sum(1 for _ in f) - 1  # Subtract header


def import_csv(filepath: str, redis_client: redis.Redis, batch_size: int = 5000) -> tuple:
    """Import GeoLite2-ASN-Blocks-IPv4.csv into Redis.

    CSV format:
        network,autonomous_system_number,autonomous_system_organization
        1.0.0.0/24,13335,CLOUDFLARENET
    """
    total_lines = count_lines(filepath)
    imported = 0
    skipped = 0
    pipe = redis_client.pipeline(transaction=False)

    with open(filepath, "r", newline="") as csvfile:
        reader = csv.DictReader(csvfile)

        for row in tqdm(reader, total=total_lines, desc="Importing ASN ranges"):
            cidr = row.get("network", "").strip()
            as_number = row.get("autonomous_system_number", "").strip()
            as_name = row.get("autonomous_system_organization", "").strip()

            if not cidr or not as_number:
                skipped += 1
                continue

            try:
                start, end = cidr_to_range(cidr)
            except (ValueError, ipaddress.AddressValueError):
                skipped += 1
                continue

            # Member: "start_int|as_number|as_name"
            member = f"{start}|{as_number}|{as_name}"

            # Score is the end of the range
            pipe.zadd(REDIS_KEY, {member: float(end)})
            imported += 1

            if imported % batch_size == 0:
                pipe.execute()
                pipe = redis_client.pipeline(transaction=False)

    # Flush remaining
    pipe.execute()

    return imported, skipped


def main():
    parser = argparse.ArgumentParser(description="Import MaxMind ASN data into Redis")
    parser.add_argument("--file", required=True, help="Path to GeoLite2-ASN-Blocks-IPv4.csv")
    parser.add_argument("--host", default="127.0.0.1", help="Redis host")
    parser.add_argument("--port", type=int, default=6379, help="Redis port")
    parser.add_argument("--db", type=int, default=0, help="Redis database number")
    parser.add_argument("--password", default="", help="Redis password")
    parser.add_argument("--batch-size", type=int, default=5000, help="Pipeline batch size")
    parser.add_argument("--flush-key", action="store_true", help="Delete existing key before import")
    args = parser.parse_args()

    r = redis.Redis(
        host=args.host,
        port=args.port,
        db=args.db,
        password=args.password or None,
        decode_responses=True,
    )

    # Verify connection
    try:
        r.ping()
    except redis.ConnectionError as e:
        print(f"ERROR: Cannot connect to Redis at {args.host}:{args.port}: {e}")
        sys.exit(1)

    if args.flush_key:
        r.delete(REDIS_KEY)
        print(f"Deleted existing key: {REDIS_KEY}")

    print(f"Importing {args.file} into Redis {args.host}:{args.port}/{args.db}")
    print(f"Key: {REDIS_KEY}, Batch size: {args.batch_size}")

    start_time = time.time()
    imported, skipped = import_csv(args.file, r, args.batch_size)
    elapsed = time.time() - start_time

    print(f"\nComplete!")
    print(f"  Imported: {imported:,} ranges")
    print(f"  Skipped:  {skipped:,} invalid entries")
    print(f"  Time:     {elapsed:.2f}s")
    if elapsed > 0:
        print(f"  Rate:     {imported/elapsed:,.0f} ranges/sec")
    print(f"  Key size: {r.zcard(REDIS_KEY):,} members")
    mem = r.memory_usage(REDIS_KEY)
    if mem:
        print(f"  Memory:   {mem:,} bytes")


if __name__ == "__main__":
    main()
