#!/usr/bin/env bash
# v7_fan 部署 — 全 Go 单进程 (compute + API + DB + 静态)
#
# 本机:
#   ./deploy.sh start      启动 ofc-go
#   ./deploy.sh stop       停
#   ./deploy.sh restart    重启
#   ./deploy.sh status     状态
#   ./deploy.sh smoke      测一个 R1
#   ./deploy.sh logs       看日志
#   ./deploy.sh build      编译 Go
#
# 打包:
#   ./deploy.sh package    生成 v7_fan-deploy.tar.gz (binary + 前端 + install.sh)
#
# systemd:
#   sudo ./deploy.sh systemd-install
#   sudo systemctl enable --now v7_fan

set -e

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

# 自动检测布局:
#   - 开发: deploy.sh 在 server-go/, 二进制 server-go-bin/ofc-go, 前端 server-go/.. (v0-dev/)
#   - 部署: deploy.sh 在 v0-dev/, 二进制 server-go-bin/ofc-go, 前端在 v0-dev/
if [ -f "$SCRIPT_DIR/3player.html" ]; then
    V7_DIR="$SCRIPT_DIR"
    BIN_DIR="$SCRIPT_DIR/server-go-bin"
else
    V7_DIR="$( dirname "$SCRIPT_DIR" )"
    BIN_DIR="$V7_DIR/server-go-bin"
fi
mkdir -p "$BIN_DIR"
GO_BIN="$BIN_DIR/ofc-go"

# 配置
PORT="${PORT:-8001}"
DEFAULT_LEVEL="${DEFAULT_LEVEL:-medium}"
SOLVE_CACHE_SIZE="${SOLVE_CACHE_SIZE:-2000}"
DB_PATH="${DB_PATH:-$SCRIPT_DIR/games.db}"
GO_PID="/tmp/ofc-go.pid"
GO_LOG="/tmp/ofc-go.log"

G='\033[32m'; Y='\033[33m'; R='\033[31m'; N='\033[0m'
err()  { echo -e "${R}❌ $*${N}" >&2; }
warn() { echo -e "${Y}⚠ $*${N}"; }
ok()   { echo -e "${G}✓ $*${N}"; }
info() { echo -e "${Y}↻ $*${N}"; }

build_go() {
    command -v go >/dev/null || { err "Go 未安装 (需要 1.21+)"; exit 1; }
    info "编译 ofc-go → $GO_BIN..."
    (cd "$SCRIPT_DIR" && go build -o "$GO_BIN" ./cmd/server)
    ok "编译完成: $GO_BIN ($(du -h $GO_BIN | cut -f1))"
}

cmd_start() {
    if [ -f "$GO_PID" ] && kill -0 "$(cat $GO_PID)" 2>/dev/null; then
        warn "ofc-go 已在跑 (pid $(cat $GO_PID))"
        return
    fi
    [ -x "$GO_BIN" ] || build_go
    info "启动 ofc-go on :$PORT (cache=$SOLVE_CACHE_SIZE, level=$DEFAULT_LEVEL, db=$DB_PATH)..."
    SOLVE_CACHE_SIZE="$SOLVE_CACHE_SIZE" \
        DEFAULT_LEVEL="$DEFAULT_LEVEL" \
        nohup "$GO_BIN" -addr=":$PORT" -static="$V7_DIR" -db="$DB_PATH" > "$GO_LOG" 2>&1 &
    echo $! > "$GO_PID"
    sleep 1
    if curl -s "http://localhost:$PORT/api/health" >/dev/null 2>&1; then
        ok "ofc-go up (pid $(cat $GO_PID), :$PORT)"
        echo
        echo "   - 浏览器:    http://$(hostname -I | awk '{print $1}'):$PORT/3player.html"
        echo "                http://localhost:$PORT/3player.html"
        echo "   - 健康检查:  curl http://localhost:$PORT/api/health | jq"
        echo "   - 关闭:      $0 stop"
    else
        err "启动失败, 看日志: tail $GO_LOG"
        return 1
    fi
}

cmd_stop() {
    if [ -f "$GO_PID" ]; then
        local pid=$(cat "$GO_PID")
        if kill -0 "$pid" 2>/dev/null; then
            kill "$pid" 2>/dev/null && ok "ofc-go stopped (pid $pid)"
            sleep 1
            kill -0 "$pid" 2>/dev/null && kill -9 "$pid" 2>/dev/null
        fi
        rm -f "$GO_PID"
    else
        warn "no pid file"
    fi
}

cmd_status() {
    if [ -f "$GO_PID" ] && kill -0 "$(cat $GO_PID)" 2>/dev/null; then
        local pid=$(cat "$GO_PID")
        local resp=$(curl -s "http://localhost:$PORT/api/health" 2>/dev/null || echo '{}')
        ok "ofc-go running  pid=$pid  port=$PORT"
        echo "$resp" | head -c 400
        echo
    else
        err "ofc-go down"
    fi
}

cmd_smoke() {
    info "测一个 R1 (medium)..."
    curl -s -X POST "http://localhost:$PORT/api/solve" \
        -H 'Content-Type: application/json' \
        -d '{"round":1,"state":{"top":[],"middle":[],"bottom":[],"usedCards":[]},"dealt":["Ks","Kh","5d","9c","2s"],"discardCount":0,"level":"medium"}'
    echo
    ok "smoke 完成"
}

cmd_logs() { tail -f "$GO_LOG"; }

# ============================================================
# 打包
# ============================================================
cmd_package() {
    command -v go >/dev/null || { err "Go 未安装"; exit 1; }

    info "编译 ofc-go for $(go env GOOS)/$(go env GOARCH) → $GO_BIN..."
    (cd "$SCRIPT_DIR" && go build -o "$GO_BIN" ./cmd/server)
    ok "binary: $(du -h "$GO_BIN" | cut -f1)"

    local PKG_DIR="/tmp/v7_fan-pkg"
    rm -rf "$PKG_DIR"
    mkdir -p "$PKG_DIR/v7_fan"

    info "复制文件..."
    # 二进制 + 部署脚本
    cp "$GO_BIN" "$PKG_DIR/v7_fan/ofc-go"
    cp "$SCRIPT_DIR/deploy.sh" "$PKG_DIR/v7_fan/deploy.sh"
    chmod +x "$PKG_DIR/v7_fan/deploy.sh"
    # 前端
    for f in 3player.html fantasy.html index.html app.js game.js solver.js worker.js style.css trained-main-weights.json; do
        [ -f "$V7_DIR/$f" ] && cp "$V7_DIR/$f" "$PKG_DIR/v7_fan/"
    done

    # install.sh
    cat > "$PKG_DIR/v7_fan/install.sh" <<'INSTALL_EOF'
#!/usr/bin/env bash
# 一键安装 — 单 Go binary, 无任何运行时依赖
set -e
DIR="$( cd "$( dirname "$0" )" && pwd )"
echo "v7_fan 安装到: $DIR"
[ -x "$DIR/ofc-go" ] || { echo "❌ ofc-go 不存在或不可执行"; exit 1; }
chmod +x "$DIR/deploy.sh"
"$DIR/deploy.sh" start
INSTALL_EOF
    chmod +x "$PKG_DIR/v7_fan/install.sh"

    # README
    cat > "$PKG_DIR/v7_fan/README-DEPLOY.txt" <<'README_EOF'
v7_fan 一键部署 (全 Go, 零依赖)
================================

启动:
  ./install.sh

启动后:
  - http://<host>:8001/3player.html   浏览器 3 人对战
  - http://<host>:8001/api/health     监控

管理:
  ./deploy.sh status     查状态
  ./deploy.sh logs       看日志
  ./deploy.sh stop       停
  ./deploy.sh restart    重启

环境变量:
  PORT=8001              监听端口
  DEFAULT_LEVEL=medium   high / medium / low
  SOLVE_CACHE_SIZE=2000  LRU 容量 (0=关)
  DB_PATH=games.db       sqlite 路径
README_EOF

    info "打包 tar.gz..."
    local OUT="$V7_DIR/v7_fan-deploy.tar.gz"
    cd "$PKG_DIR" && tar czf "$OUT" v7_fan/
    rm -rf "$PKG_DIR"
    ok "📦 打包完成: $OUT ($(du -h $OUT | cut -f1))"
    echo
    echo "目标机部署:"
    echo "  scp $OUT user@target:~/"
    echo "  ssh user@target"
    echo "  tar xzf v7_fan-deploy.tar.gz && cd v7_fan && ./install.sh"
}

# ============================================================
# systemd
# ============================================================
cmd_systemd_install() {
    [ "$EUID" -eq 0 ] || { err "需要 sudo"; exit 1; }
    local user_run="${SUDO_USER:-$(whoami)}"
    local svc=/etc/systemd/system/v7_fan.service
    info "生成 $svc (user=$user_run)..."
    cat > "$svc" <<UNIT_EOF
[Unit]
Description=v7_fan OFC solver (single Go binary)
After=network.target

[Service]
Type=forking
User=$user_run
WorkingDirectory=$SCRIPT_DIR
ExecStart=$SCRIPT_DIR/deploy.sh start
ExecStop=$SCRIPT_DIR/deploy.sh stop
ExecReload=$SCRIPT_DIR/deploy.sh restart
PIDFile=$GO_PID
Restart=on-failure
RestartSec=5
Environment="PATH=/usr/local/bin:/usr/bin:/bin"
Environment="HOME=/home/$user_run"

[Install]
WantedBy=multi-user.target
UNIT_EOF
    systemctl daemon-reload
    ok "systemd unit 已装 → $svc"
    echo
    echo "启用:"
    echo "  sudo systemctl enable --now v7_fan"
    echo "查看:"
    echo "  sudo systemctl status v7_fan"
    echo "  sudo journalctl -u v7_fan -f"
}

case "${1:-help}" in
    start)            cmd_start ;;
    stop)             cmd_stop ;;
    restart)          cmd_stop; sleep 1; cmd_start ;;
    status)           cmd_status ;;
    smoke)            cmd_smoke ;;
    logs)             cmd_logs ;;
    build)            build_go ;;
    package)          cmd_package ;;
    systemd-install)  cmd_systemd_install ;;
    *)
        cat <<EOF
v7_fan 部署 — 全 Go 单进程

本机管理:
  $0 start            启动
  $0 stop             停
  $0 restart          重启
  $0 status           看状态
  $0 smoke            R1 验证
  $0 logs             看日志 (tail -f)
  $0 build            只编译

打包发布:
  $0 package          → v7_fan-deploy.tar.gz (单 binary + 前端 + install.sh)
                        scp 到目标机 → tar xzf → ./install.sh

systemd 自启:
  sudo $0 systemd-install
  sudo systemctl enable --now v7_fan

环境变量:
  PORT=8001              监听端口
  DEFAULT_LEVEL=medium   high / medium / low
  SOLVE_CACHE_SIZE=2000  LRU 容量
  DB_PATH=games.db       sqlite 路径
EOF
        ;;
esac
