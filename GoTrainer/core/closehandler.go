package core

import (
	"sync"
	"syscall"
)

// 控制台控制事件（CTRL_*）。点窗口关闭按钮 / 注销 / 关机时系统会同步回调。
const (
	ctrlCloseEvent    = 2
	ctrlLogoffEvent   = 5
	ctrlShutdownEvent = 6
)

var (
	procSetConsoleCtrlHandler = modkernel32.NewProc("SetConsoleCtrlHandler")
	closeHandlerMu            sync.Mutex
	closeHandlerFn            func()
)

// SetCloseHandler 注册"窗口被关闭 / 注销 / 关机"时的清理回调（best-effort）。
//
// 用途: 修改器是外部写内存的工具，"关掉程序"本身不会撤销它对游戏做的修改。
// 我们要求**任何离开运行状态的路径**都跑一次统一的清理（能复原的复原，
// 一次性修改型不动）——返回版本选择、全关功能都由主循环处理；点窗口关闭
// 按钮则靠这里。Windows 在 CTRL_CLOSE_EVENT 时会给进程约 5 秒做清理，
// 本回调同步执行后返回 TRUE（表示已处理，阻止默认的立即终止）。
//
// 不可捕获的路径: 任务管理器"结束任务"/强杀收不到该事件，无法清理。
func SetCloseHandler(fn func()) {
	closeHandlerMu.Lock()
	closeHandlerFn = fn
	closeHandlerMu.Unlock()
	procSetConsoleCtrlHandler.Call(syscall.NewCallback(consoleCtrlHandler), 1)
}

func consoleCtrlHandler(ctrlType uint32) uintptr {
	switch ctrlType {
	case ctrlCloseEvent, ctrlLogoffEvent, ctrlShutdownEvent:
		closeHandlerMu.Lock()
		fn := closeHandlerFn
		closeHandlerMu.Unlock()
		if fn != nil {
			// 回调运行在系统的控制台线程上，绝不能 panic 出去。
			func() {
				defer func() { _ = recover() }()
				fn()
			}()
		}
	}
	return 1 // TRUE: 已处理
}
