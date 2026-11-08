#include <cstdio>
#include <cstdlib>
#include <cstring>

#include "borderless.h"
#include "win32_guard.h"

namespace {

void PrintUsage() {
    std::printf(
        "rf4-borderless - 修正 RF4 无边框窗口（去掉残留标题栏并铺满屏幕）\n"
        "\n"
        "用法: rf4-borderless [--interval 300] [--grace 25000]\n"
        "  --interval <ms>  轮询间隔，默认 300\n"
        "  --grace <ms>     窗口出现后等待多久再修正，默认 25000\n"
        "                   （Unity 初始化 D3D 期间改窗口样式会导致游戏退出）\n");
}

}  // namespace

int main(int argc, char** argv) {
    int interval_ms = 300;
    long long grace_ms = rf4::borderless::kStartupGraceMs;

    for (int i = 1; i < argc; ++i) {
        if (std::strcmp(argv[i], "--help") == 0 || std::strcmp(argv[i], "-h") == 0) {
            PrintUsage();
            return 0;
        }
        if (std::strcmp(argv[i], "--interval") == 0 && i + 1 < argc) {
            interval_ms = std::atoi(argv[++i]);
            continue;
        }
        if (std::strcmp(argv[i], "--grace") == 0 && i + 1 < argc) {
            grace_ms = std::atoll(argv[++i]);
            continue;
        }
        PrintUsage();
        return 1;
    }

    if (interval_ms <= 0 || grace_ms < 0) {
        PrintUsage();
        return 1;
    }

    rf4::borderless::RunGuard(interval_ms, grace_ms);
    return 0;
}
