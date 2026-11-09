#!/usr/bin/env python3
"""给 Nebula 子模块打一个最小补丁：修复 disabledTun.Close 与 Read 的数据竞争。

背景：tun.disabled=true（测试/无 TUN 环境）时，upstream 的 disabledTun.Close
会写 t.read = nil，而 Read 并发读取 t.read，`go test -race` 会报 data race。
生产使用真实 TUN 时不会触发；这里用 sync.Once 保证 Close 幂等且不再写 read 字段。

用法：
    python3 scripts/patch_nebula.py [third_party/nebula]

幂等；若上游已修复/代码结构变化，打印提示并跳过，不影响构建。
"""

import pathlib
import sys

root = pathlib.Path(sys.argv[1] if len(sys.argv) > 1 else "third_party/nebula")
path = root / "overlay" / "tun_disabled.go"
if not path.exists():
    print(f"⚠️ 未找到 {path}，跳过 Nebula 补丁")
    sys.exit(0)

src = path.read_text()
if "closeOnce sync.Once" in src:
    print("✅ Nebula disabledTun 补丁已应用，跳过")
    sys.exit(0)

old_struct = "\ttx metrics.Counter\n\trx metrics.Counter\n"
old_close = """func (t *disabledTun) Close() error {
\tif t.read != nil {
\t\tclose(t.read)
\t\tt.read = nil
\t}
\treturn nil
}"""
new_close = """func (t *disabledTun) Close() error {
\t// 用 Once 保证幂等；不写 read 字段，避免与 Read 并发读写触发 data race。
\tt.closeOnce.Do(func() { close(t.read) })
\treturn nil
}"""

if old_struct not in src or old_close not in src:
    if "t.read = nil" in src:
        print("❌ 上游代码结构已变化且仍存在 t.read=nil 竞态，请更新 patch_nebula.py")
        sys.exit(1)
    print("✅ 上游已无该竞态写法，跳过 disabledTun 补丁")
    sys.exit(0)

if '\t"sync"\n' not in src:
    src = src.replace('\t"strings"\n', '\t"strings"\n\t"sync"\n', 1)
src = src.replace(old_struct, old_struct + "\tcloseOnce sync.Once\n", 1)
src = src.replace(old_close, new_close, 1)
path.write_text(src)
print("✅ 已应用 Nebula disabledTun 并发补丁")
