#include "win32_guard.h"

#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include <tlhelp32.h>

#include <cstdio>

#include "borderless.h"

namespace rf4::borderless {
namespace {

DWORD FindGameProcess(const wchar_t* exe_name) {
    HANDLE snapshot = CreateToolhelp32Snapshot(TH32CS_SNAPPROCESS, 0);
    if (snapshot == INVALID_HANDLE_VALUE) return 0;

    PROCESSENTRY32W entry{};
    entry.dwSize = sizeof(entry);
    DWORD pid = 0;
    if (Process32FirstW(snapshot, &entry)) {
        do {
            if (_wcsicmp(entry.szExeFile, exe_name) == 0) {
                pid = entry.th32ProcessID;
                break;
            }
        } while (Process32NextW(snapshot, &entry));
    }
    CloseHandle(snapshot);
    return pid;
}

struct EnumContext {
    DWORD pid = 0;
    HWND window = nullptr;
};

BOOL CALLBACK EnumWindowsProc(HWND window, LPARAM param) {
    auto* ctx = reinterpret_cast<EnumContext*>(param);
    DWORD window_pid = 0;
    GetWindowThreadProcessId(window, &window_pid);
    if (window_pid != ctx->pid) return TRUE;
    if (!IsWindowVisible(window)) return TRUE;
    if (GetWindow(window, GW_OWNER) != nullptr) return TRUE;
    ctx->window = window;
    return FALSE;
}

HWND FindGameWindow(DWORD pid) {
    EnumContext ctx;
    ctx.pid = pid;
    EnumWindows(EnumWindowsProc, reinterpret_cast<LPARAM>(&ctx));
    return ctx.window;
}

}  // namespace

void RunGuard(int interval_ms, long long startup_grace_ms) {
    HWND seen_window = nullptr;
    bool forced_by_us = false;
    unsigned long long saved_style = 0;
    unsigned long long saved_ex_style = 0;
    ULONGLONG first_seen_at = 0;
    ULONGLONG last_fix_at = 0;

    std::printf("[rf4-borderless] watching rf4_x64.exe (interval=%dms grace=%lldms)\n",
                interval_ms, startup_grace_ms);
    std::fflush(stdout);

    for (;;) {
        const DWORD pid = FindGameProcess(L"rf4_x64.exe");
        HWND window = pid ? FindGameWindow(pid) : nullptr;

        if (window != nullptr) {
            const ULONGLONG now = GetTickCount64();
            if (window != seen_window) {
                seen_window = window;
                first_seen_at = now;
                forced_by_us = false;
                std::printf("[rf4-borderless] game window found: pid=%lu hwnd=%p\n", pid, window);
                std::fflush(stdout);
            }

            MONITORINFO monitor_info{};
            monitor_info.cbSize = sizeof(monitor_info);
            if (!GetMonitorInfoW(MonitorFromWindow(window, MONITOR_DEFAULTTONEAREST), &monitor_info)) {
                Sleep(interval_ms);
                continue;
            }

            WindowSnapshot snapshot;
            snapshot.style = static_cast<unsigned long long>(GetWindowLongPtrW(window, GWL_STYLE));
            snapshot.visible = IsWindowVisible(window) != FALSE;
            snapshot.minimized = IsIconic(window) != FALSE;
            RECT rect{};
            GetWindowRect(window, &rect);
            snapshot.rect = {rect.left, rect.top, rect.right, rect.bottom};

            const Size monitor_size{monitor_info.rcMonitor.right - monitor_info.rcMonitor.left,
                                    monitor_info.rcMonitor.bottom - monitor_info.rcMonitor.top};

            const Action action =
                Decide(snapshot, monitor_size, forced_by_us,
                       static_cast<long long>(now - first_seen_at), startup_grace_ms);

            if (action == Action::MakeBorderless && now - last_fix_at >= kFixCooldownMs) {
                if (!forced_by_us) {
                    saved_style = snapshot.style;
                    saved_ex_style =
                        static_cast<unsigned long long>(GetWindowLongPtrW(window, GWL_EXSTYLE));
                }
                SetWindowLongPtrW(window, GWL_STYLE,
                                  static_cast<LONG_PTR>(StripBorderStyle(snapshot.style)));
                SetWindowLongPtrW(window, GWL_EXSTYLE,
                                  static_cast<LONG_PTR>(StripBorderExStyle(
                                      static_cast<unsigned long long>(
                                          GetWindowLongPtrW(window, GWL_EXSTYLE)))));
                SetWindowPos(window, HWND_TOP, monitor_info.rcMonitor.left,
                             monitor_info.rcMonitor.top, monitor_size.width, monitor_size.height,
                             SWP_FRAMECHANGED | SWP_NOACTIVATE | SWP_SHOWWINDOW);
                forced_by_us = true;
                last_fix_at = now;
                std::printf("[rf4-borderless] force borderless: win=%dx%d mon=%dx%d\n",
                            snapshot.rect.width(), snapshot.rect.height(), monitor_size.width,
                            monitor_size.height);
                std::fflush(stdout);
            } else if (action == Action::RestoreBordered && forced_by_us &&
                       now - last_fix_at >= kFixCooldownMs) {
                SetWindowLongPtrW(window, GWL_STYLE, static_cast<LONG_PTR>(saved_style));
                SetWindowLongPtrW(window, GWL_EXSTYLE, static_cast<LONG_PTR>(saved_ex_style));
                SetWindowPos(window, nullptr, 0, 0, 0, 0,
                             SWP_NOMOVE | SWP_NOSIZE | SWP_NOZORDER | SWP_NOACTIVATE |
                                 SWP_FRAMECHANGED);
                forced_by_us = false;
                last_fix_at = now;
                std::printf("[rf4-borderless] restored bordered window\n");
                std::fflush(stdout);
            }
        } else {
            seen_window = nullptr;
        }

        Sleep(interval_ms);
    }
}

}  // namespace rf4::borderless
