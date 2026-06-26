// Package utils 提供通用工具函数。
package utils

import (
	"fmt"
	"runtime/debug"

	"github.com/brianliu-sysu/arbitrage/internal/logx"
)

// SafeGo 在新 goroutine 中执行 fn。如果 fn 发生 panic，会被自动 recover 并通过
// logger 记录完整的堆栈信息，防止整个进程崩溃。
//
// 用法：
//
//	utils.SafeGo(logger, func() {
//	    // 可能 panic 的代码
//	})
func SafeGo(logger logx.Logger, fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("goroutine panic recovered",
					"panic", fmt.Sprintf("%v", r),
					"stack", string(debug.Stack()),
				)
			}
		}()
		fn()
	}()
}
