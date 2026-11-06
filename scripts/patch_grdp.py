#!/usr/bin/env python3
"""对 grdp 子模块应用 iflygo 的本地补丁。

grdp 是 RDP 客户端库（github.com/tomatome/grdp），上游 2018 年已停更。
iflygo 需要以下本地修改（详见 scripts/ 与 README）：

  1. 移除 plugin/channel.go 的 `import "C"` —— grdp 的 plugin 包声明了 CGO（虚拟通道），
     导致无法 CGO_ENABLED=0 静态编译；
  2. 删除 client/rfb.go（grdp 自带的 VNC 客户端，代码 broken，iflygo 已自研 VNC）；
  3. 删除 grdp.go（main 包）与 example/（GUI/socket.io 依赖）与 protocol/rfb/（死代码）；
  4. client/client.go 移除 VNC 分支（TC_VNC）；
  5. client/rdp.go 修复 MouseWheel 事件类型（SCANCODE→MOUSE + WheelRotation 编码）。

用法（在仓库根目录）：
    python3 scripts/patch_grdp.py [third_party/grdp]

幂等：可重复运行（已 patch 的目录再次运行无副作用）。
"""

import os
import shutil
import sys

GRDP = sys.argv[1] if len(sys.argv) > 1 else os.path.join("third_party", "grdp")


def patch_channel_go():
    """移除 plugin/channel.go 的 import "C" + 修复 32 位 int 溢出。"""
    p = os.path.join(GRDP, "plugin", "channel.go")
    if not os.path.exists(p):
        return
    s = open(p, encoding="utf-8").read()
    orig = s
    if 'import "C"' in s:
        s = s.replace('import "C"\n', "", 1)
    # StaticVirtualChannels 的 map[string]int 在 32 位架构（GOARCH=arm）下，
    # CHANNEL_OPTION_* 组合值（0xC0000000 等）超出 int32 范围导致溢出。改为 uint32。
    s = s.replace("var StaticVirtualChannels = map[string]int{", "var StaticVirtualChannels = map[string]uint32{")
    if s != orig:
        open(p, "w", encoding="utf-8").write(s)
        print("  ✓ 移除 channel.go 的 import \"C\" + 修复 32 位溢出")


def patch_client_go():
    """client/client.go 移除 VNC 分支，仅保留 RDP。"""
    p = os.path.join(GRDP, "client", "client.go")
    if not os.path.exists(p):
        return
    s = open(p, encoding="utf-8").read()
    orig = s
    s = s.replace('\t"github.com/tomatome/grdp/protocol/rfb"\n', "")
    s = s.replace("const (\n\tTC_RDP = 0\n\tTC_VNC = 1\n)", "const (\n\tTC_RDP = 0\n)")
    s = s.replace("\tctl     Control\n\ttc      int\n\tsetting *Setting", "\tctl     Control\n\tsetting *Setting")
    # NewClient 简化：移除 tc 赋值与 VNC switch
    s = s.replace(
        "\t\tpasswd:  passwd,\n\t\ttc:      t,\n\t\tsetting: s,\n\t}\n\n\tswitch t {\n\tcase TC_VNC:\n\t\tc.ctl = newVncClient(s)\n\tdefault:\n\t\tc.ctl = newRdpClient(s)\n\t}",
        "\t\tpasswd:  passwd,\n\t\tsetting: s,\n\t\tctl:     newRdpClient(s),\n\t}",
    )
    # OnBitmap 移除 VNC 分支
    s = s.replace(
        "\t\tbs := make([]Bitmap, 0, 50)\n\t\tif c.tc == TC_VNC {\n\t\t\tbr := data.(*rfb.BitRect)\n\t\t\tfor _, v := range br.Rects {\n\t\t\t\tb := Bitmap{int(v.Rect.X), int(v.Rect.Y), int(v.Rect.X + v.Rect.Width), int(v.Rect.Y + v.Rect.Height),\n\t\t\t\t\tint(v.Rect.Width), int(v.Rect.Height),\n\t\t\t\t\tBpp(uint16(br.Pf.BitsPerPixel)), false, v.Data}\n\t\t\t\tbs = append(bs, b)\n\t\t\t}\n\t\t} else {\n\t\t\tfor _, v := range data.([]pdu.BitmapData) {",
        "\t\tbs := make([]Bitmap, 0, 50)\n\t\tfor _, v := range data.([]pdu.BitmapData) {",
    )
    # 修复 OnBitmap 因删除 VNC 分支后多出的一层缩进
    s = s.replace(
        "\t\t\t\tb := Bitmap{int(v.DestLeft), int(v.DestTop), int(v.DestRight), int(v.DestBottom),\n\t\t\t\t\tint(v.Width), int(v.Height), Bpp(v.BitsPerPixel), IsCompress, stream}\n\t\t\t\tbs = append(bs, b)\n\t\t\t}\n\t\t}",
        "\t\t\tb := Bitmap{int(v.DestLeft), int(v.DestTop), int(v.DestRight), int(v.DestBottom),\n\t\t\t\tint(v.Width), int(v.Height), Bpp(v.BitsPerPixel), IsCompress, stream}\n\t\t\tbs = append(bs, b)\n\t\t}",
    )
    if s != orig:
        open(p, "w", encoding="utf-8").write(s)
        print("  ✓ client/client.go 移除 VNC 分支")


def patch_rdp_go():
    """client/rdp.go 修复 MouseWheel。"""
    p = os.path.join(GRDP, "client", "rdp.go")
    if not os.path.exists(p):
        return
    s = open(p, encoding="utf-8").read()
    orig = s
    s = s.replace(
        "\tp.PointerFlags |= pdu.PTRFLAGS_WHEEL\n\tp.XPos = uint16(x)\n\tp.YPos = uint16(y)\n\tc.pdu.SendInputEvents(pdu.INPUT_EVENT_SCANCODE, []pdu.InputEventsInterface{p})",
        "\tp.PointerFlags |= pdu.PTRFLAGS_WHEEL\n\tif scroll < 0 {\n\t\tp.PointerFlags |= pdu.PTRFLAGS_WHEEL_NEGATIVE\n\t\tscroll = -scroll\n\t}\n\tp.XPos = uint16(scroll & 0x01FF) // WheelRotation 编码在 XPos 低位\n\tp.YPos = uint16(y)\n\tc.pdu.SendInputEvents(pdu.INPUT_EVENT_MOUSE, []pdu.InputEventsInterface{p})",
    )
    if s != orig:
        open(p, "w", encoding="utf-8").write(s)
        print("  ✓ client/rdp.go 修复 MouseWheel")


def remove_paths():
    """删除不需要的文件/目录（broken VNC、main 包、example、rfb 协议）。"""
    removes = [
        os.path.join(GRDP, "client", "rfb.go"),
        os.path.join(GRDP, "grdp.go"),
        os.path.join(GRDP, "protocol", "rfb"),
        os.path.join(GRDP, "example"),
    ]
    for p in removes:
        if os.path.isfile(p):
            os.remove(p)
            print(f"  ✓ 删除 {os.path.relpath(p, GRDP)}")
        elif os.path.isdir(p):
            shutil.rmtree(p)
            print(f"  ✓ 删除目录 {os.path.relpath(p, GRDP)}")


def main():
    if not os.path.isdir(GRDP):
        print(f"❌ 目录不存在: {GRDP}")
        sys.exit(1)
    print(f"对 {GRDP} 应用 grdp 补丁：")
    patch_channel_go()
    patch_client_go()
    patch_rdp_go()
    remove_paths()
    print("✅ grdp 补丁完成")


if __name__ == "__main__":
    main()
