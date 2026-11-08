// 无边框判定逻辑的单元测试，不依赖 Windows API：
//   g++ -std=c++17 tests/borderless_test.cpp src/borderless.cpp -o borderless_test
#include <cstdio>

#include "borderless.h"

namespace {

int g_failed = 0;
int g_passed = 0;

void Expect(bool condition, const char* name) {
    if (condition) {
        ++g_passed;
    } else {
        ++g_failed;
        std::printf("FAIL: %s\n", name);
    }
}

using rf4::borderless::Action;
using rf4::borderless::WindowSnapshot;

constexpr unsigned long long kCaptionStyle = 0x14CA0000ULL;  // 可见 + 标题栏 + 可调边框
constexpr unsigned long long kPopupStyle = 0x94000000ULL;    // 无边框全屏

WindowSnapshot BorderedWindow(int width, int height, bool visible = true) {
    WindowSnapshot window;
    window.style = kCaptionStyle;
    window.visible = visible;
    window.minimized = false;
    window.rect = {0, 0, width, height};
    return window;
}

void TestBorderedFullscreenIsFixed() {
    const auto window = BorderedWindow(1938, 1127);
    Expect(rf4::borderless::Decide(window, {1920, 1080}, false, 60000) == Action::MakeBorderless,
           "带标题栏且外框铺满屏幕 -> 强制无边框");
}

void TestStartupGraceSkipsFix() {
    const auto window = BorderedWindow(1938, 1127);
    Expect(rf4::borderless::Decide(window, {1920, 1080}, false, 1000) == Action::None,
           "启动宽限期内不动窗口");
    Expect(rf4::borderless::Decide(window, {1920, 1080}, false, 25000) == Action::MakeBorderless,
           "宽限期结束即可修正");
}

void TestSmallWindowIsUntouched() {
    const auto window = BorderedWindow(1280, 720);
    Expect(rf4::borderless::Decide(window, {1920, 1080}, false, 60000) == Action::None,
           "普通小窗口不处理");
}

void TestSmallWindowRestoresOwnBorder() {
    const auto window = BorderedWindow(1280, 720);
    Expect(rf4::borderless::Decide(window, {1920, 1080}, true, 60000) == Action::RestoreBordered,
           "自己改过的窗口切回小窗口时还原边框");
}

void TestBorderlessWindowIsUntouched() {
    WindowSnapshot window;
    window.style = kPopupStyle;
    window.visible = true;
    window.rect = {0, 0, 1920, 1080};
    Expect(rf4::borderless::Decide(window, {1920, 1080}, false, 60000) == Action::None,
           "已经是无边框则不动");
}

void TestHiddenOrMinimizedIsUntouched() {
    auto hidden = BorderedWindow(1938, 1127, false);
    Expect(rf4::borderless::Decide(hidden, {1920, 1080}, false, 60000) == Action::None,
           "不可见窗口不处理");

    auto minimized = BorderedWindow(1938, 1127);
    minimized.minimized = true;
    Expect(rf4::borderless::Decide(minimized, {1920, 1080}, false, 60000) == Action::None,
           "最小化窗口不处理");
}

void TestToleranceBoundary() {
    const auto inside = BorderedWindow(1920 - 64, 1080 - 64);
    Expect(rf4::borderless::Decide(inside, {1920, 1080}, false, 60000) == Action::MakeBorderless,
           "容差边界内算全屏");

    const auto outside = BorderedWindow(1920 - 65, 1080);
    Expect(rf4::borderless::Decide(outside, {1920, 1080}, false, 60000) == Action::None,
           "小于容差边界不处理");
}

void TestStripStyles() {
    const auto style = rf4::borderless::StripBorderStyle(kCaptionStyle);
    Expect((style & rf4::borderless::kWsCaption) == 0, "去边框后不再带 WS_CAPTION");
    Expect((style & rf4::borderless::kWsThickFrame) == 0, "去边框后不再带 WS_THICKFRAME");
    Expect((style & rf4::borderless::kWsPopup) != 0, "去边框后是 WS_POPUP");
    Expect((style & rf4::borderless::kWsVisible) != 0, "去边框后保持可见");

    const auto ex_style = rf4::borderless::StripBorderExStyle(
        rf4::borderless::kWsExWindowEdge | 0x00000100ULL);
    Expect((ex_style & rf4::borderless::kWsExWindowEdge) == 0, "去边框后不再带 WS_EX_WINDOWEDGE");
}

}  // namespace

int main() {
    TestBorderedFullscreenIsFixed();
    TestStartupGraceSkipsFix();
    TestSmallWindowIsUntouched();
    TestSmallWindowRestoresOwnBorder();
    TestBorderlessWindowIsUntouched();
    TestHiddenOrMinimizedIsUntouched();
    TestToleranceBoundary();
    TestStripStyles();

    std::printf("passed=%d failed=%d\n", g_passed, g_failed);
    return g_failed == 0 ? 0 : 1;
}
