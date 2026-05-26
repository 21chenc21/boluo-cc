#!/bin/bash
# 扫并发: 找 4 核 linux 在 r1Mult=1.0 时能扛多少用户
# 用法: ./sweep-concurrency.sh [url=http://localhost:9000]

URL=${1:-http://localhost:9000}
echo "扫描 r1Mult=1.0 不同并发, 服务=$URL"
echo

for c in 1 2 3 4 5 6 8 12 16; do
    echo "━━━ $c 并发 ━━━"
    node load-go.js $c 1.0 $URL 2>&1 | grep -E "Go 计算:|端到端|总墙钟|吞吐|失败|p99"
    echo
done
