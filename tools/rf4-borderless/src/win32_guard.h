#pragma once

namespace rf4::borderless {

// 常驻监视 rf4_x64.exe 的主窗口，按 borderless.h 的判定结果修正窗口样式。
// interval_ms：轮询间隔；startup_grace_ms：窗口出现后多久才开始动手。
void RunGuard(int interval_ms, long long startup_grace_ms);

}  // namespace rf4::borderless
