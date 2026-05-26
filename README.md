# boluo-cc

Pineapple OFC (大菠萝) + Texas NLHE AI 项目集合.

## 子项目

### 📦 [v0-dev/](v0-dev/) — Pineapple OFC V3 NN
- V3 NN inference (147-d features, sp19 太子)
- gen-rollout-dataset / alphazero-train / bench-cases / bench-3metric
- mac-scripts/ self-play training pipeline
- 详情见 [v0-dev/server-go/API.md](v0-dev/server-go/API.md)

### 🚀 [ofc-dev-v3/](ofc-dev-v3/) — Pineapple OFC 生产部署
- sp19 iter-3 r1 太子部署 (port 8002)
- 3player.html 前端 (AI 类型 + AI 难度 dropdown)
- 详情见 [ofc-dev-v3/DEPLOY.md](ofc-dev-v3/DEPLOY.md)

### ♠ [texas/](texas/) — Texas NLHE CFR
- MCCFR engine (HU + 6-max)
- Multi-street + abstraction (OCHS)
- NN distillation + ONNX inference
- 详情见 [texas/README.md](texas/README.md) / [texas/PROGRESS.md](texas/PROGRESS.md)

## 排除内容

`.gitignore` 排除大文件:
- 训练产物 (`v3-train-i147-sp1*/`, `v3-dataset*/`)
- Python venv (`.venv/`, ~1.3G torch)
- 编译 binary (`server-go-bin/`)
- 运行时 (`*.log`, `*.db`, `node_modules`)

要训练/部署 binary, 需要本地 `go build` + 训练 pipeline 重跑生成 ckpt.
