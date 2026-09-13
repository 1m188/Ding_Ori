package ori

import "testing"

// TestApplyProfileByVersion 锁住"版本 → 地址空间参数"的绑定关系。
//
// 背景（2026-09 实机踩坑）: 主程序曾经用 `&ori.Runtime{}`（Prof 为 nil）建会话，
// SetProcess 里 `if r.Prof != nil` 直接跳过 ApplyProfile，于是原版仍用终极版的
// 对象下界 0x40000000 去扫，而原版托管堆在 0x01xxxxxx —— 界面永远卡在
// "Sein 扫描中…"。本测试确保: 只要通过 NewRuntime 建 Runtime，两个版本的
// 下界/vtable 带就必须正确切换。
func TestApplyProfileByVersion(t *testing.T) {
	const (
		vanillaSein      = 0x01A8E6C0 // 原版实测玩家对象（低地址堆）
		vanillaVtable    = 0x35B73338 // 原版实测 Keys MonoVTable
		definitiveSein   = 0x50A1B2C4 // 终极版堆内地址（示例）
		definitiveVtable = 0x2A000000 // 终极版 vtable 带内地址
	)

	NewRuntime(Profiles[Vanilla]).SetProcess(nil)
	if !isHeapPtr(vanillaSein) {
		t.Fatalf("原版下界错误: 0x%08X 应被认作堆指针", vanillaSein)
	}
	if !vtableBand(vanillaVtable) {
		t.Fatalf("原版 vtable 带错误: 0x%08X 应在扫描带内", vanillaVtable)
	}
	// 注: 原版扫描带是 {0x20000000-0x40000000, 0x50000000-0x54000000}，前者
	// 较宽、会覆盖到终极版的 0x2A 段（多扫一点无害）；关键是**反过来**不能让
	// 终极版漏掉原版的 0x35B7 段，见下方 Definitive 断言。

	NewRuntime(Profiles[Definitive]).SetProcess(nil)
	if isHeapPtr(vanillaSein) {
		t.Fatalf("终极版下界错误: 0x%08X 是原版低地址堆，不应被认作对象", vanillaSein)
	}
	if !isHeapPtr(definitiveSein) {
		t.Fatalf("终极版下界错误: 0x%08X 应被认作堆指针", definitiveSein)
	}
	if !vtableBand(definitiveVtable) {
		t.Fatalf("终极版 vtable 带错误: 0x%08X 应在扫描带内", definitiveVtable)
	}
	if vtableBand(vanillaVtable) {
		t.Fatalf("终极版不应匹配原版 vtable 带 0x%08X", vanillaVtable)
	}
}
