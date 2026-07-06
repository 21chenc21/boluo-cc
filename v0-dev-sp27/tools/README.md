# tools/ — 常用工具箱 (2026-07-07 立)

| 工具 | 用途 |
|---|---|
| `hotfix-gen.sh <case号> <family> [games]` | 单靶粮 gen (Mac跑快). 产出 fam-<case>-<family>/ |
| `hotfix-finetune.sh <fam粮dir> <case号> [epochs]` | 太子3ep微调 (~2分钟). 产出 v3-train-hf-<case>/ |
| `bench-verify.sh <ckpt>` | 裸 + 全栈×2 + 名单稳定性 一键验收 |
| `../deploy-prod.sh [weights.json]` | 一键推 prod (binary; 带参=权重原子部署) |

热修全流程 (playbook 二·七): 听证→可达性→hotfix-gen→hotfix-finetune→bench-verify→攒批 deploy.
太子指针: 改 tools/CHAMPION 文件一行.
