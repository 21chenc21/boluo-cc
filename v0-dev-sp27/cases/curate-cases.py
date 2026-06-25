#!/usr/bin/env python3
# 精挑 case 14-63 的 expecteds. user 手动 curated 1-13, 14+ 我做。
# 策略: 1-2 代表性 layout (low filler 不枚举全部 split), 用户明确给的 case 完全照办。

import json

EXP_PATH = '/home/chguang/boluo-cc/v0-dev/cases/all-tests-expanded.json'

# 精挑 expecteds: { num: [(top_inc, mid_inc, bot_inc), ...] }
CURATED = {
    14: [  # JsQs 同色高散应底道, A 顶, Qs Js 底
        (['As'], ['9c', '7h'], ['Qs', 'Js']),
    ],
    15: [  # TsKh 至少一张应底道
        (['Ts'], ['3c', '4h'], ['Kh', 'Qh']),  # Ts 顶 + Kh Qh 底
        (['Kh'], ['Ts', '3c'], ['4h', 'Qh']),  # Kh 顶 + Ts 中
    ],
    16: [  # 9cQh 至少一张应底道, ♣ 同色集中底
        (['2d'], ['Qh'], ['8c', '9c', 'Kc']),
    ],
    17: [  # TT 应底, 3不上底
        (['3h', 'Ks'], ['9s'], ['Td', 'Th']),
    ],
    18: [  # Qd+TT 同底
        (['4s', '6d'], [], ['Qd', 'Tc', 'Td']),
    ],
    19: [  # 4 ♠ 全底 + Ac 顶
        (['Ac'], [], ['2s', '5s', '3s', 'Js']),
    ],
    20: [  # 7h Ts 8s 集中底
        (['3c', '4d'], [], ['Td', '8s', '7h']),
    ],
    21: [  # 9h 底, Ac 顶, 4d/6c/7d 三种合理安排 (per user)
        (['Ac'], ['4d', '7d'], ['6c', '9h']),
        (['Ac'], ['4d', '6c'], ['7d', '9h']),
        (['Ac'], ['6c', '7d'], ['4d', '9h']),
    ],
    22: [  # TT 应底, 3不上底 (同 17 复制)
        (['3h', 'Ks'], ['9s'], ['Td', 'Th']),
    ],
    23: [  # R2: 33 不破中. 弃 4d, Qc 顶 + 6s 底
        (['Qc'], [], ['6s']),
    ],
    24: [  # 固定: X+A 顶, 23 中, K 底
        (['X', 'As'], ['3h', '2d'], ['Ks']),
    ],
    25: [  # 33 在中保对, 顶无 3, 一代表
        (['Js', '8s'], ['3d', '3h'], ['Td']),
    ],
    26: [  # 233 分中底: 33 mid + Th 底
        (['2c'], ['3c', '3h'], ['Th', '5c']),
    ],
    27: [  # Ac 顶 + Jh 底 + 2/3/4 mid
        (['Ac'], ['2s', '3c', '4d'], ['Jh']),
    ],
    28: [  # R2: 9h 上底凑 4♥ flush. 弃 Kc, 6d 中
        ([], ['6d'], ['9h']),
    ],
    29: [  # R2: 不破 33, Ac 顶
        (['Ac'], [], ['2s']),  # 弃 7d
    ],
    30: [  # R3 不弃 joker. 弃 8h, Ah+X 安排
        (['Ah'], ['X'], []),
    ],
    31: [  # R2 9c 上底凑 OE. 弃 Kh, 2s 中
        ([], ['2s'], ['9c']),
    ],
    32: [  # 双鬼 ≥1 上顶 (user 确认: X顶+X中 OR X顶+X底)
        (['X'], ['X'], []),
        (['X'], [], ['X']),
    ],
    33: [  # KK 必上底
        ([], [], ['Kh', 'Ks']),
    ],
    34: [  # 2♥ 集中底凑 flush
        ([], [], ['7h', '8h']),
    ],
    35: [  # 5 不上中 (防 trips foul). 弃 5d
        ([], ['3h'], ['9c']),
    ],
    36: [  # A 必顶 (user 确认: 留 4 中 / 8 底)
        (['As'], ['4d'], []),    # 弃 8h, 4d 中
        (['As'], [], ['8h']),    # 弃 4d, 8h 底
    ],
    37: [  # R3 弃 9d 不凑 mid straight
        (['Kc'], [], ['2h']),
    ],
    38: [  # R3 9 不上中. 弃 9c, 4d/7s 安排
        ([], ['4d', '7s'], []),
    ],
    39: [  # R3 6 不上中. 弃 6c
        ([], ['4s', '5d'], []),
    ],
    40: [  # R3 弃 8d. Kc 2h 不上中
        (['Kc'], [], ['2h']),
    ],
    41: [  # 2c 必上底. Q/K 上顶或底 (user 确认 4 种)
        ([], [], ['Qh', '2c']),    # 弃 Kh, Q 底
        (['Qh'], [], ['2c']),       # 弃 Kh, Q 顶
        ([], [], ['Kh', '2c']),    # 弃 Qh, K 底
        (['Kh'], [], ['2c']),       # 弃 Qh, K 顶
    ],
    42: [  # 弃 Qd, 4s 上中
        (['Ah'], ['4s'], []),
    ],
    43: [  # K 不上顶 (user 确认: Kh 底, 2d 中, 弃 9s)
        ([], ['2d'], ['Kh']),
    ],
    44: [  # 9h 完成底 straight, 弃 2s 或 2d
        (['2d'], [], ['9h']),
        (['2s'], [], ['9h']),
    ],
    45: [  # 2c 上顶
        (['2c'], ['Kh'], []),   # Kh 中, 弃 9d
    ],
    46: [  # A 不弃 (user 确认 3 种)
        (['As'], ['3d'], []),       # As 顶, 3d 中, 弃 2d
        (['3d'], ['As'], []),       # As 中, 3d 顶, 弃 2d
        ([], ['As'], ['2d']),       # As 中, 2d 底, 弃 3d
    ],
    47: [  # 不破底 KK, 5h 上中, 2h 不上中 (user 补充)
        (['9h'], ['5h'], []),       # 5h 中 + 9h 顶, 弃 2h
        ([], ['5h'], ['9h']),       # 5h 中 + 9h 底, 弃 2h
        (['2h'], ['5h'], []),       # 5h 中 + 2h 顶, 弃 9h
        ([], ['5h'], ['2h']),       # 5h 中 + 2h 底, 弃 9h
    ],
    48: [  # Qh 完成底 flush
        (['2s'], [], ['Qh']),       # 弃 4s
        (['4s'], [], ['Qh']),       # 弃 2s
    ],
    49: [  # 8h 上顶 (joker 降为 8), Kc 中
        (['8h'], ['Kc'], []),
    ],
    50: [  # 7h 顶 + 8s 底, 弃 As
        (['7h'], [], ['8s']),
    ],
    51: [  # UR3 0A used: X+Ac 顶
        (['X', 'Ac'], ['2c', '5h'], ['9s']),
    ],
    52: [  # 2A used (同 51)
        (['X', 'Ac'], ['2c', '5h'], ['9s']),
    ],
    53: [  # 3A used (同 51)
        (['X', 'Ac'], ['2c', '5h'], ['9s']),
    ],
    54: [  # 4A used + no A: joker 顶等高牌
        (['X'], ['2c', '5h'], ['9s', '8c']),
    ],
    55: [  # 4A+3K used, dealt K: X+Kh 锁 KK
        (['X', 'Kh'], ['2c', '5h'], ['9s']),
    ],
    56: [  # 0A used, K+joker: X 顶 + Kh 底
        (['X'], ['2c', '5h'], ['9s', 'Kh']),
    ],
    57: [  # R3 3A used + A: X+Ah 顶, 弃 8h 或 2h
        (['Ah'], ['2h'], []),
        (['Ah'], ['8h'], []),
    ],
    58: [  # R3 0A used + A (同 57)
        (['Ah'], ['2h'], []),
        (['Ah'], ['8h'], []),
    ],
    59: [  # R2 KK 0A used: KK 必上底
        ([], [], ['Kh', 'Ks']),
    ],
    60: [  # KK 1A used
        ([], [], ['Kh', 'Ks']),
    ],
    61: [  # KK 2A used
        ([], [], ['Kh', 'Ks']),
    ],
    62: [  # KK 3A used: 顶或底
        (['Kh', 'Ks'], [], []),
        ([], [], ['Kh', 'Ks']),
    ],
    63: [  # KK 4A used: 必上顶
        (['Kh', 'Ks'], [], []),
    ],
}

# Case 36 错误摆法 (per user: 弃A + 8 中 + 4 底)
EXTRA_WRONGS = {
    36: [
        ([], ['8h'], ['4d']),  # incremental: 8h 中, 4d 底, 弃 As
    ],
}

def main():
    with open(EXP_PATH) as f:
        cases = json.load(f)

    curated_count = 0
    for c in cases:
        first_tok = c['name'].split(' ')[0]
        try:
            num = int(first_tok)
        except ValueError:
            continue
        if num < 14:
            continue  # 1-13 user 手动 curated
        if num not in CURATED:
            print(f"WARN: case {num} not in CURATED, keeping as-is")
            continue
        # 替换 expecteds
        layouts = CURATED[num]
        c['expecteds'] = [
            {'top': list(t), 'middle': list(m), 'bottom': list(b)}
            for (t, m, b) in layouts
        ]
        # 加 extra wrongs
        if num in EXTRA_WRONGS:
            existing = c.get('wrongs', [])
            for (t, m, b) in EXTRA_WRONGS[num]:
                new_wrong = {'top': list(t), 'middle': list(m), 'bottom': list(b)}
                if new_wrong not in existing:
                    existing.append(new_wrong)
            c['wrongs'] = existing
        # 删 check
        if 'check' in c:
            del c['check']
        curated_count += 1

    with open(EXP_PATH, 'w') as f:
        json.dump(cases, f, indent=2, ensure_ascii=False)
    print(f"✓ Curated {curated_count} cases (14-63)")
    print(f"  Total: {len(cases)}")
    print(f"  Avg expecteds: {sum(len(c.get('expecteds', [])) for c in cases)/len(cases):.1f}")
    print(f"  Cases with wrongs: {sum(1 for c in cases if c.get('wrongs'))}")

if __name__ == '__main__':
    main()
