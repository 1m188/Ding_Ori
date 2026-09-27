using System;
using System.Reflection;

namespace OriTrainerDLL.Features
{
    // 可在不安全区域 / 不平稳地面建立灵魂链接。
    //
    // ---- 原理：hook 安全判定本身，而不是绕过它 ----
    // 游戏把所有"能否在此处建立灵魂链接"的判定汇聚在属性 getter
    // SeinSoulFlame.IsSafeToCastSoulFlame（返回 SoulFlamePlacementSafety 枚举）。
    // hook 版把 getter 换成恒返回 Safe，让游戏自己走正常逻辑：
    // HandleCharging() 看到 Safe → 正常涨蓄力到 1 → CastSoulFlame() 施放。
    //
    // ---- 停止 ----
    // Stop() 只 Unhook，还原 getter 原始字节，安全判定恢复真实结果。
    public static class SoulFlameAnywhere
    {
        private static MethodInfo _target;      // SeinSoulFlame.get_IsSafeToCastSoulFlame
        private static MethodInfo _replacement; // 我们的 replacement

        public static void Start()
        {
            if (Hooks.IsHooked(_target)) return; // 幂等：重复 Start 不重复 hook

            // C# 属性 getter 在 IL 里是 specialname 方法 get_IsSafeToCastSoulFlame。
            // 用 GetMethod("get_...") 在部分反射实现里可能因 specialname 过滤而失败
            // （找 null 会让功能静默无效），所以走正规取法：GetProperty 拿属性，
            // 再 GetGetMethod(true) 拿 getter。它是 public 属性的 getter。
            PropertyInfo prop = typeof(SeinSoulFlame).GetProperty("IsSafeToCastSoulFlame",
                BindingFlags.Public | BindingFlags.Instance);
            // 属性名对不上就直接失败（Loader 会记进错误日志），而不是静默空转
            _target = (prop?.GetGetMethod(true)) ?? throw new Exception("SeinSoulFlame.IsSafeToCastSoulFlame 与预期不符，功能无法工作");

            _replacement = typeof(SoulFlameAnywhere).GetMethod("OnIsSafeToCastSoulFlame",
                BindingFlags.NonPublic | BindingFlags.Static);

            Hooks.Replace(_target, _replacement);
        }

        public static void Stop()
        {
            if (!Hooks.IsHooked(_target)) return; // 幂等

            Hooks.Unhook(_target);
            _target = null;
            _replacement = null;
        }

        // 由游戏在需要判断"能否建立灵魂链接"时调用（代替 get_IsSafeToCastSoulFlame）。
        // 恒返回 Safe：告诉游戏任何位置都能建链接。
        private static SeinSoulFlame.SoulFlamePlacementSafety OnIsSafeToCastSoulFlame(SeinSoulFlame soulFlame)
        {
            return SeinSoulFlame.SoulFlamePlacementSafety.Safe;
        }
    }
}