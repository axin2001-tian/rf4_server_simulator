// 游戏窗口无边框判定逻辑（纯逻辑，不依赖 Windows API，便于单元测试）。
//
// 背景：游戏设置里的「无边框窗口」实测只是把窗口改成屏幕大小，标题栏仍然存在，
// 窗口外框因此比显示器大一圈、底部超出屏幕；重启游戏或 Alt+Tab 切回后，
// Unity 还会把标题栏重新写回来。
#pragma once

namespace rf4::borderless {

// Win32 窗口样式常量，数值与 WinUser.h 一致；这里不引入 windows.h，
// 是为了让判定逻辑能在任意平台编译和单测。
inline constexpr unsigned long long kWsBorder = 0x00800000ULL;
inline constexpr unsigned long long kWsDlgFrame = 0x00400000ULL;
inline constexpr unsigned long long kWsCaption = 0x00C00000ULL;  // WS_BORDER | WS_DLGFRAME
inline constexpr unsigned long long kWsThickFrame = 0x00040000ULL;
inline constexpr unsigned long long kWsSysMenu = 0x00080000ULL;
inline constexpr unsigned long long kWsMinimizeBox = 0x00020000ULL;
inline constexpr unsigned long long kWsMaximizeBox = 0x00010000ULL;
inline constexpr unsigned long long kWsPopup = 0x80000000ULL;
inline constexpr unsigned long long kWsVisible = 0x10000000ULL;
inline constexpr unsigned long long kWsClipSiblings = 0x04000000ULL;

inline constexpr unsigned long long kWsExWindowEdge = 0x00000100ULL;

// 显示器尺寸容差：窗口外框比屏幕大（带边框）或略小都算「玩家想要全屏」。
inline constexpr int kMonitorTolerancePx = 64;
// 启动宽限：Unity 初始化 D3D 交换链期间修改窗口样式会导致游戏退出。
inline constexpr long long kStartupGraceMs = 25000;
// 同一窗口两次修正之间的最短间隔，避免和游戏互相抢样式。
inline constexpr long long kFixCooldownMs = 2000;

struct Rect {
    int left = 0;
    int top = 0;
    int right = 0;
    int bottom = 0;

    int width() const { return right - left; }
    int height() const { return bottom - top; }
};

struct Size {
    int width = 0;
    int height = 0;
};

struct WindowSnapshot {
    unsigned long long style = 0;
    bool visible = false;
    bool minimized = false;
    Rect rect;
};

enum class Action {
    None,             // 保持原样
    MakeBorderless,   // 去掉标题栏并贴到显示器左上角
    RestoreBordered,  // 还原被我们改掉的边框（玩家切回了小窗口）
};

bool IsBordered(unsigned long long style);

// forced_by_us：当前无边框样式是否由本工具修改。
// ms_since_first_seen：从第一次看到该窗口到现在经过的毫秒数。
Action Decide(const WindowSnapshot& window, Size monitor, bool forced_by_us,
              long long ms_since_first_seen, long long startup_grace_ms = kStartupGraceMs);

unsigned long long StripBorderStyle(unsigned long long style);
unsigned long long StripBorderExStyle(unsigned long long ex_style);

}  // namespace rf4::borderless
