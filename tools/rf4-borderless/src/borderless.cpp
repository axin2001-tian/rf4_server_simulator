#include "borderless.h"

namespace rf4::borderless {

bool IsBordered(unsigned long long style) {
    return (style & (kWsCaption | kWsThickFrame | kWsBorder | kWsDlgFrame)) != 0;
}

Action Decide(const WindowSnapshot& window, Size monitor, bool forced_by_us,
              long long ms_since_first_seen, long long startup_grace_ms) {
    if (!window.visible || window.minimized) return Action::None;
    if (!IsBordered(window.style)) return Action::None;

    const int win_width = window.rect.width();
    const int win_height = window.rect.height();
    const bool fills_monitor = win_width >= monitor.width - kMonitorTolerancePx &&
                               win_height >= monitor.height - kMonitorTolerancePx;

    if (!fills_monitor) {
        // 真正的小窗口不动，除非当前无边框是本工具强加的，切回来后要还原边框。
        return forced_by_us ? Action::RestoreBordered : Action::None;
    }

    if (ms_since_first_seen < startup_grace_ms) return Action::None;
    return Action::MakeBorderless;
}

unsigned long long StripBorderStyle(unsigned long long style) {
    const unsigned long long drop = kWsCaption | kWsThickFrame | kWsBorder | kWsDlgFrame |
                                    kWsSysMenu | kWsMinimizeBox | kWsMaximizeBox;
    return (style & ~drop) | kWsPopup | kWsVisible | kWsClipSiblings;
}

unsigned long long StripBorderExStyle(unsigned long long ex_style) {
    return ex_style & ~kWsExWindowEdge;
}

}  // namespace rf4::borderless
