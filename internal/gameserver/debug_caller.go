package gameserver

import (
	"fmt"
	"path/filepath"
	"runtime"
)

// callerShort 返回调用栈上第 skip 层的 "文件:行" 简写，用于定位同一事件的多条推送路径。
func callerShort(skip int) string {
	_, file, line, ok := runtime.Caller(skip)
	if !ok {
		return "?"
	}
	return fmt.Sprintf("%s:%d", filepath.Base(file), line)
}
