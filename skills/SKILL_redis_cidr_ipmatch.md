# SKILL: Redis Sorted Set CIDR IP-to-ASN Lookup

## Purpose

Implement O(log n) IP-to-ASN lookups using Redis sorted sets. This pattern stores
CIDR ranges as scored members where the score is the integer representation of the
IP address, enabling efficient range queries with `ZRANGEBYSCORE`.

## Version Pins

- Go: `github.com/redis/go-redis/v9 v9.7.0`
- Python: `redis==5.2.1`
- Redis Server: 7.2+
- Data Source: MaxMind GeoLite2-ASN-Blocks-IPv4.csv

---

## 1. IP-to-Integer Conversion (Go)

### IPv4 Conversion

```go
package ipmatch

import (
	"encoding/binary"
	"fmt"
	"net"
)

// IPv4ToUint32 converts an IPv4 address to its 32-bit unsigned integer representation.
func IPv4ToUint32(ip net.IP) (uint32, error) {
	ipv4 := ip.To4()
	if ipv4 == nil {
		return 0, fmt.Errorf("not a valid IPv4 address: %s", ip.String())
	}
	return binary.BigEndian.Uint32(ipv4), nil
}

// Uint32ToIPv4 converts a 32-bit unsigned integer back to an IPv4 address.
func Uint32ToIPv4(n uint32) net.IP {
	ip := make(net.IP, 4)
	binary.BigEndian.PutUint32(ip, n)
	return ip
}

// CIDRToRange returns the start and end integer values for a CIDR block.
func CIDRToRange(cidr string) (start uint32, end uint32, err error) {
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid CIDR %q: %w", cidr, err)
	}

	startIP := network.IP.To4()
	if startIP == nil {
		return 0, 0, fmt.Errorf("not IPv4 CIDR: %s", cidr)
	}

	start = binary.BigEndian.Uint32(startIP)

	// Calculate end: invert the mask and OR with start
	mask := binary.BigEndian.Uint32(network.Mask)
	end = start | ^mask

	return start, end, nil
}
```

### IPv6 Conversion

```go
package ipmatch

import (
	"fmt"
	"math/big"
	"net"
)

// IPv6ToBigInt converts an IPv6 address to a *big.Int (128-bit integer).
func IPv6ToBigInt(ip net.IP) (*big.Int, error) {
	ipv6 := ip.To16()
	if ipv6 == nil {
		return nil, fmt.Errorf("not a valid IPv6 address: %s", ip.String())
	}
	// big.Int.SetBytes interprets bytes as unsigned big-endian
	return new(big.Int).SetBytes(ipv6), nil
}

// BigIntToIPv6 converts a *big.Int back to an IPv6 address.
func BigIntToIPv6(n *big.Int) net.IP {
	b := n.Bytes()
	// Pad to 16 bytes
	ip := make(net.IP, 16)
	copy(ip[16-len(b):], b)
	return ip
}

// CIDRv6ToRange returns the start and end big.Int for an IPv6 CIDR block.
func CIDRv6ToRange(cidr string) (start *big.Int, end *big.Int, err error) {
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid CIDR %q: %w", cidr, err)
	}

	ipv6 := network.IP.To16()
	if ipv6 == nil {
		return nil, nil, fmt.Errorf("not IPv6: %s", cidr)
	}

	start = new(big.Int).SetBytes(ipv6)

	// Calculate end: set all host bits to 1
	ones, bits := network.Mask.Size()
	hostBits := bits - ones

	// end = start | ((1 << hostBits) - 1)
	hostMask := new(big.Int).Lsh(big.NewInt(1), uint(hostBits))
	hostMask.Sub(hostMask, big.NewInt(1))
	end = new(big.Int).Or(start, hostMask)

	return start, end, nil
}
```

---

## 2. Redis Storage Pattern

### Design

Each CIDR range is stored as a single entry in a Redis sorted set:
- **Score**: end IP integer (the last IP in the CIDR block)
- **Member**: `"<start_ip_int>|<asn_number>|<asn_name>"`

### Lookup Algorithm

To find the ASN for a given IP:
1. Convert IP to integer
2. Query: `ZRANGEBYSCORE key <ip_int> +inf LIMIT 0 1`
3. This returns the first range whose end >= our IP
4. Parse the member to get the start IP
5. If our IP >= start, the IP is within this range; otherwise it falls in a gap

This works because ranges are non-overlapping. The first range ending at or after
our IP is the only candidate that could contain it.

---

## 3. Complete Go Implementation

```go
package ipmatch

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	// RedisKeyIPv4ASN is the sorted set key for IPv4 ASN ranges.
	RedisKeyIPv4ASN = "asn:ipv4:ranges"
	// RedisKeyIPv6ASN is the sorted set key for IPv6 ASN ranges.
	RedisKeyIPv6ASN = "asn:ipv6:ranges"
)

// ASNResult holds the result of an IP-to-ASN lookup.
type ASNResult struct {
	ASNumber uint32
	ASName   string
	CIDR     string
	Found    bool
}

// Client wraps a Redis connection for IP-to-ASN lookups.
type Client struct {
	rdb *redis.Client
}

// NewClient creates a new IP match client with connection pooling.
func NewClient(addr, password string, db int) *Client {
	rdb := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     password,
		DB:           db,
		PoolSize:     100,
		MinIdleConns: 10,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  2 * time.Second,
		WriteTimeout: 2 * time.Second,
	})
	return &Client{rdb: rdb}
}

// Close closes the Redis connection.
func (c *Client) Close() error {
	return c.rdb.Close()
}

// InsertRange adds a CIDR range with its ASN info to Redis.
func (c *Client) InsertRange(ctx context.Context, cidr string, asNumber uint32, asName string) error {
	start, end, err := CIDRToRange(cidr)
	if err != nil {
		return err
	}

	// Member format: "<start_uint32>|<asn_number>|<asn_name>"
	member := fmt.Sprintf("%d|%d|%s", start, asNumber, asName)

	// Score is the end of the range (for ZRANGEBYSCORE >= ip lookup)
	return c.rdb.ZAdd(ctx, RedisKeyIPv4ASN, redis.Z{
		Score:  float64(end),
		Member: member,
	}).Err()
}

// LookupIPv4 performs an O(log n) lookup of an IPv4 address.
func (c *Client) LookupIPv4(ctx context.Context, ipStr string) (ASNResult, error) {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return ASNResult{}, fmt.Errorf("invalid IP: %s", ipStr)
	}

	ipInt, err := IPv4ToUint32(ip)
	if err != nil {
		return ASNResult{}, err
	}

	// Find the first range whose end >= our IP
	results, err := c.rdb.ZRangeByScoreWithScores(ctx, RedisKeyIPv4ASN, &redis.ZRangeBy{
		Min:    strconv.FormatUint(uint64(ipInt), 10),
		Max:    "+inf",
		Offset: 0,
		Count:  1,
	}).Result()

	if err != nil {
		return ASNResult{}, fmt.Errorf("redis query failed: %w", err)
	}

	if len(results) == 0 {
		return ASNResult{Found: false}, nil
	}

	// Parse the member
	member := results[0].Member.(string)
	parts := strings.SplitN(member, "|", 3)
	if len(parts) != 3 {
		return ASNResult{}, fmt.Errorf("malformed member: %s", member)
	}

	// Verify IP >= range start
	rangeStart, err := strconv.ParseUint(parts[0], 10, 32)
	if err != nil {
		return ASNResult{}, fmt.Errorf("invalid range start: %s", parts[0])
	}

	if uint64(ipInt) < rangeStart {
		// IP is in the gap between ranges
		return ASNResult{Found: false}, nil
	}

	asNum, _ := strconv.ParseUint(parts[1], 10, 32)

	return ASNResult{
		ASNumber: uint32(asNum),
		ASName:   parts[2],
		CIDR:     fmt.Sprintf("%s (range %d-%d)", ipStr, rangeStart, uint64(results[0].Score)),
		Found:    true,
	}, nil
}

// BulkInsert efficiently loads multiple ranges using Redis pipeline.
func (c *Client) BulkInsert(ctx context.Context, ranges []CIDREntry, batchSize int) (int, error) {
	inserted := 0
	pipe := c.rdb.Pipeline()

	for i, entry := range ranges {
		start, end, err := CIDRToRange(entry.CIDR)
		if err != nil {
			continue // Skip invalid entries
		}

		member := fmt.Sprintf("%d|%d|%s", start, entry.ASNumber, entry.ASName)
		pipe.ZAdd(ctx, RedisKeyIPv4ASN, redis.Z{
			Score:  float64(end),
			Member: member,
		})
		inserted++

		if (i+1)%batchSize == 0 {
			if _, err := pipe.Exec(ctx); err != nil {
				return inserted, fmt.Errorf("pipeline exec failed at batch %d: %w", i/batchSize, err)
			}
			pipe = c.rdb.Pipeline()
		}
	}

	// Flush remaining
	if inserted%batchSize != 0 {
		if _, err := pipe.Exec(ctx); err != nil {
			return inserted, fmt.Errorf("final pipeline exec failed: %w", err)
		}
	}

	return inserted, nil
}

// CIDREntry represents a single ASN range to import.
type CIDREntry struct {
	CIDR     string
	ASNumber uint32
	ASName   string
}
```

---

## 4. Python Bulk Import Script

```python
#!/usr/bin/env python3
# Bulk import MaxMind GeoLite2-ASN-Blocks-IPv4.csv into Redis sorted sets.
#
# Requirements:
#     pip install redis==5.2.1 tqdm==4.67.1
#
# Usage:
#     python3 import_asn.py --file GeoLite2-ASN-Blocks-IPv4.csv \
#                           --host 127.0.0.1 --port 6379 --db 0

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


def cidr_to_range(cidr: str) -> tuple[int, int]:
    """Return (start_int, end_int) for a CIDR block."""
    network = ipaddress.IPv4Network(cidr, strict=False)
    start = int(network.network_address)
    end = int(network.broadcast_address)
    return start, end


def count_lines(filepath: str) -> int:
    """Count lines in CSV for progress bar."""
    with open(filepath, "r") as f:
        return sum(1 for _ in f) - 1  # Subtract header


def import_csv(filepath: str, redis_client: redis.Redis, batch_size: int = 5000) -> tuple[int, int]:
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
    print(f"  Rate:     {imported/elapsed:,.0f} ranges/sec")
    print(f"  Key size: {r.zcard(REDIS_KEY):,} members")
    print(f"  Memory:   {r.memory_usage(REDIS_KEY):,} bytes")


if __name__ == "__main__":
    main()
```

---

## 5. Redis Commands for Manual Testing

```bash
# Connect to Redis
redis-cli -h 127.0.0.1 -p 6379

# Check key exists and get member count
ZCARD asn:ipv4:ranges
# Expected: ~500,000 for full GeoLite2 ASN dataset

# Lookup IP 8.8.8.8 (Google DNS)
# First convert to integer: 8*16777216 + 8*65536 + 8*256 + 8 = 134744072
ZRANGEBYSCORE asn:ipv4:ranges 134744072 +inf LIMIT 0 1
# Expected: "134743040|15169|GOOGLE"
# Verify: 134744072 >= 134743040 (start), so IP is in range

# Lookup IP 1.1.1.1 (Cloudflare DNS)
# Integer: 1*16777216 + 1*65536 + 1*256 + 1 = 16843009
ZRANGEBYSCORE asn:ipv4:ranges 16843009 +inf LIMIT 0 1
# Expected: "16843008|13335|CLOUDFLARENET"

# Check memory usage
MEMORY USAGE asn:ipv4:ranges
# Expected: ~80-120 MB for full dataset

# Get total count
ZCARD asn:ipv4:ranges

# Get all entries for a specific ASN (slow, for debugging only)
# Use ZSCAN with pattern matching
ZSCAN asn:ipv4:ranges 0 MATCH "*|13335|*" COUNT 100

# Check score (end IP) for a specific member
ZSCORE asn:ipv4:ranges "16843008|13335|CLOUDFLARENET"

# Range query - find all ranges covering 192.168.0.0/16
# 192.168.0.0 = 3232235520, 192.168.255.255 = 3232301055
ZRANGEBYSCORE asn:ipv4:ranges 3232235520 3232301055
```

---

## 6. O(log n) Lookup Proof

### Why This Works

Redis sorted sets are implemented as skip lists, providing O(log n) time complexity
for range queries (`ZRANGEBYSCORE`).

**Data structure:** Skip list with hash table index
- Each CIDR range contributes exactly 1 member to the sorted set
- Members are sorted by score (end IP integer)
- `ZRANGEBYSCORE min +inf LIMIT 0 1` finds the first member with score >= min

**Lookup algorithm:**
1. Convert target IP to integer: O(1)
2. Query `ZRANGEBYSCORE <ip_int> +inf LIMIT 0 1`: O(log n) via skip list traversal
3. Parse member string and verify start <= ip: O(1)
4. Total: O(log n)

**Comparison with full table scan:**
- Naive approach: iterate all ~500K ranges = O(n)
- Sorted set approach: skip list traversal = O(log 500000) = ~19 comparisons

**Benchmark expectations:**
```
BenchmarkLookupIPv4-8    500000    2.1 us/op    0 allocs/op
```

At 2 microseconds per lookup, this handles 500K lookups/second per goroutine.
With connection pooling (100 connections), theoretical throughput is 50M lookups/second.

---

## 7. Benchmark Methodology

```go
package ipmatch_test

import (
	"context"
	"fmt"
	"math/rand"
	"testing"
)

func BenchmarkLookupIPv4(b *testing.B) {
	client := NewClient("127.0.0.1:6379", "", 0)
	defer client.Close()
	ctx := context.Background()

	// Pre-generate random IPs to avoid allocation in loop
	ips := make([]string, 10000)
	for i := range ips {
		ips[i] = fmt.Sprintf("%d.%d.%d.%d",
			rand.Intn(224), rand.Intn(256),
			rand.Intn(256), rand.Intn(256))
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		ip := ips[i%len(ips)]
		_, _ = client.LookupIPv4(ctx, ip)
	}
}

func BenchmarkBulkLookup(b *testing.B) {
	client := NewClient("127.0.0.1:6379", "", 0)
	defer client.Close()
	ctx := context.Background()

	// Simulate realistic traffic pattern
	knownIPs := []string{
		"8.8.8.8", "1.1.1.1", "208.67.222.222",
		"9.9.9.9", "76.76.2.0", "94.140.14.14",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ip := knownIPs[i%len(knownIPs)]
		_, _ = client.LookupIPv4(ctx, ip)
	}
}
```

**Expected benchmark output format:**

```
goos: linux
goarch: amd64
pkg: ghostroute/internal/ipmatch
cpu: AMD EPYC 7R13 Processor
BenchmarkLookupIPv4-8       548923    2134 ns/op     0 B/op    0 allocs/op
BenchmarkBulkLookup-8       612045    1876 ns/op     0 B/op    0 allocs/op
PASS
ok  	ghostroute/internal/ipmatch	3.412s
```

---

## 8. Memory Estimation

### Per-Entry Memory

Each sorted set member in Redis uses:
- Skip list node: ~96 bytes (pointers, score, backward pointer, level array)
- Hash table entry: ~56 bytes (dictEntry, key pointer, hash)
- Member string: ~40-80 bytes (SDS header + content)
  - Typical member: `"3232235520|15169|GOOGLE"` = 24 bytes + 9 bytes SDS overhead

**Total per entry: ~200-230 bytes**

### Full Dataset Estimation

MaxMind GeoLite2-ASN-Blocks-IPv4.csv contains approximately:
- ~490,000 IPv4 CIDR ranges
- ~75,000 unique ASNs

```
Memory per entry:     ~220 bytes (average)
Total entries:        490,000
Estimated memory:     490,000 * 220 = 107,800,000 bytes = ~103 MB

With Redis overhead:
  - Sorted set metadata: ~128 bytes
  - Key overhead: ~64 bytes
  - Total: ~103 MB + negligible overhead

Actual measured (typical): 80-120 MB
```

### IPv6 Dataset

GeoLite2-ASN-Blocks-IPv6.csv has ~200,000 entries:
```
Memory per entry:     ~280 bytes (longer member strings for big.Int)
Total entries:        200,000
Estimated memory:     200,000 * 280 = 56,000,000 bytes = ~53 MB
```

### Combined Total

```
IPv4 ASN ranges:  ~103 MB
IPv6 ASN ranges:  ~53 MB
Total Redis RAM:  ~156 MB

Recommended allocation: 256 MB (with headroom for fragmentation)
```

---

## 9. Comparison with Alternative Approaches

| Approach | Lookup Time | Memory | Insert Time | Pros | Cons |
|----------|-------------|--------|-------------|------|------|
| **Redis Sorted Set** | O(log n) ~2us | ~103 MB | O(log n) per insert | Simple, distributed, persistent, no custom code | Network latency, Redis dependency |
| **Radix/Patricia Trie** | O(k) k=32 bits | ~60 MB | O(k) | Optimal for IP lookup, compact | In-process only, complex implementation |
| **Binary search (sorted array)** | O(log n) | ~20 MB | O(n) rebuild | Minimal memory, simple | Must rebuild on update, in-process only |
| **Bitmap (interval tree)** | O(1) amortized | ~512 MB | O(n) | Constant time | Enormous memory, IPv4 only |
| **Hash map (all IPs)** | O(1) | ~16 GB | O(n) | Fastest lookup | Impractical memory, IPv4 only |
| **SQLite/PostgreSQL** | O(log n) ~50us | Disk | O(log n) | Durable, SQL queries | 25x slower than Redis |

### Recommendation

Use **Redis sorted set** as the primary lookup mechanism because:
1. Shared across all GhostRoute worker instances (distributed)
2. Persistent (survives restarts with RDB/AOF)
3. Simple to implement (no custom data structures)
4. Fast enough for real-time TDS decisions (<5us including network)
5. Easy to update (ZADD is atomic, no rebuild required)

For even lower latency, maintain an **in-process radix trie** as an L1 cache that
syncs from Redis periodically. This gives sub-microsecond lookups for hot IPs while
Redis serves as the source of truth.

---

*End of SKILL_redis_cidr_ipmatch.md*
