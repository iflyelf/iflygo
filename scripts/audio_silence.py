#!/usr/bin/env python3
"""测试用音频源：持续输出 48kHz 双声道 s16le 静音 PCM。"""

import sys
import time

CHUNK = 48000 // 50 * 2 * 2  # 20ms
SILENCE = b"\0" * CHUNK

try:
    while True:
        sys.stdout.buffer.write(SILENCE)
        sys.stdout.buffer.flush()
        time.sleep(0.02)
except (BrokenPipeError, KeyboardInterrupt):
    pass
