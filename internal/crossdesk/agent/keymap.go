package agent

import "strings"

// 浏览器 KeyboardEvent.keyCode 使用 Windows 虚拟键码（VK）。
// 这里把它映射到 X11 keysym / macOS 虚拟键码，Linux 与 macOS 输入层共用。

// vkToKeysym 返回 X11 keysym（0 表示不支持）。
func vkToKeysym(vk int) uint32 {
	switch {
	case vk >= 'A' && vk <= 'Z':
		return uint32(vk) + 0x20 // XK_a..XK_z
	case vk >= '0' && vk <= '9':
		return uint32(vk)
	case vk >= 96 && vk <= 105: // 小键盘 0..9
		return uint32(0xffb0 + vk - 96)
	case vk >= 112 && vk <= 123: // F1..F12
		return uint32(0xffbe + vk - 112)
	case vk >= 124 && vk <= 135: // F13..F24
		return uint32(0xffca + vk - 124)
	}
	switch vk {
	case 8:
		return 0xff08 // BackSpace
	case 9:
		return 0xff09 // Tab
	case 13:
		return 0xff0d // Return
	case 16:
		return 0xffe1 // Shift_L
	case 17:
		return 0xffe3 // Control_L
	case 18:
		return 0xffe9 // Alt_L
	case 19:
		return 0xff13 // Pause
	case 20:
		return 0xffe5 // Caps_Lock
	case 27:
		return 0xff1b // Escape
	case 32:
		return 0x20 // space
	case 33:
		return 0xff55 // Prior
	case 34:
		return 0xff56 // Next
	case 35:
		return 0xff57 // End
	case 36:
		return 0xff50 // Home
	case 37:
		return 0xff51 // Left
	case 38:
		return 0xff52 // Up
	case 39:
		return 0xff53 // Right
	case 40:
		return 0xff54 // Down
	case 44:
		return 0xff61 // Print
	case 45:
		return 0xff63 // Insert
	case 46:
		return 0xffff // Delete
	case 91, 92:
		return 0xffeb // Super_L
	case 93:
		return 0xff67 // Menu
	case 144:
		return 0xff7f // Num_Lock
	case 145:
		return 0xff14 // Scroll_Lock
	case 186:
		return 0x3b // semicolon
	case 187:
		return 0x3d // equal
	case 188:
		return 0x2c // comma
	case 189:
		return 0x2d // minus
	case 190:
		return 0x2e // period
	case 191:
		return 0x2f // slash
	case 192:
		return 0x60 // grave
	case 219:
		return 0x5b // bracketleft
	case 220:
		return 0x5c // backslash
	case 221:
		return 0x5d // bracketright
	case 222:
		return 0x27 // apostrophe
	}
	return 0
}

// vkToMacKeycode 返回 macOS 虚拟键码（-1 表示不支持）。
func vkToMacKeycode(vk int) int {
	if code, ok := macKeycodes[vk]; ok {
		return code
	}
	return -1
}

var macKeycodes = map[int]int{
	'A': 0, 'S': 1, 'D': 2, 'F': 3, 'H': 4, 'G': 5, 'Z': 6, 'X': 7, 'C': 8, 'V': 9,
	'B': 11, 'Q': 12, 'W': 13, 'E': 14, 'R': 15, 'Y': 16, 'T': 17,
	'1': 18, '2': 19, '3': 20, '4': 21, '6': 22, '5': 23, '9': 25, '7': 26, '8': 28, '0': 29,
	'O': 31, 'U': 32, 'I': 34, 'P': 35, 'L': 37, 'J': 38, 'K': 40, 'N': 45, 'M': 46,
	// 符号
	187: 24, // '='
	189: 27, // '-'
	219: 33, // '['
	221: 30, // ']'
	222: 39, // '\''
	186: 41, // ';'
	220: 42, // '\\'
	188: 43, // ','
	191: 44, // '/'
	190: 47, // '.'
	192: 50, // '`'
	// 控制键
	13: 36, 8: 51, 27: 53, 9: 48, 32: 49,
	16: 56, 17: 59, 18: 58, 20: 57,
	37: 123, 39: 124, 40: 125, 38: 126,
	36: 115, 35: 119, 33: 116, 34: 121, 46: 117,
	// 功能键
	122: 120, 120: 122, 99: 99, 118: 118, 96: 96, 97: 97, 98: 98,
	100: 100, 101: 101, 109: 109, 103: 103, 111: 111,
}

// normalizeSignalURL 把 http(s)/ws(s) 地址规范成完整信令地址。
func normalizeSignalURL(raw, path string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errMissingServer
	}
	base := strings.TrimRight(raw, "/")
	if strings.HasPrefix(raw, "ws://") || strings.HasPrefix(raw, "wss://") {
		base = strings.TrimRight(raw, "/")
		rest := strings.TrimPrefix(strings.TrimPrefix(base, "ws://"), "wss://")
		if strings.Contains(rest, "/") {
			return base, nil
		}
		return base + path, nil
	}
	if strings.HasPrefix(raw, "https://") {
		return "wss://" + strings.TrimPrefix(base, "https://") + path, nil
	}
	if strings.HasPrefix(raw, "http://") {
		return "ws://" + strings.TrimPrefix(base, "http://") + path, nil
	}
	return "wss://" + strings.TrimRight(raw, "/") + path, nil
}
